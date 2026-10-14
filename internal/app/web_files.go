package app

// Files for web pages on other PCs: what a native file dialog does in the
// window becomes an upload or a download in the browser (browserPicker).
//
// Upload: a "stage" is a temporary folder. The page uploads each file with
// PUT /web/stage/<id>/file?p=<relative path> and finishes with POST
// /web/stage/<id>/done {"tops": [...]} (the top-level names it picked or
// dropped), which returns their paths here. A picker that asked for files
// (the web:pick event) waits for that; a drop on the file window makes its
// own stage and passes the paths to FileUploadPaths.
//
// Download: a finished file is offered with the web:download event; the
// page fetches /web/dl/<token>, after which the file is deleted.

import (
	"archive/zip"
	_ "embed"
	"encoding/json"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"choboterm/internal/dbg"
	"choboterm/internal/transfer"
)

//go:embed web_shim.js
var webShim []byte

func (w *webServer) handleShim(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-cache")
	_, _ = rw.Write(webShim)
}

// webStage is a folder receiving one upload.
type webStage struct {
	id     string
	client *webClient
	dir    string
}

// webDownload is a file waiting for the page to fetch it.
type webDownload struct {
	path    string
	name    string
	dir     string // removed with the file
	client  *webClient
	expires time.Time
}

// webDownloadTTL is how long an offered file waits to be fetched.
const webDownloadTTL = 30 * time.Minute

// newStage makes an upload folder for client c.
func (w *webServer) newStage(c *webClient) (*webStage, error) {
	dir, err := c.tempDir()
	if err != nil {
		return nil, err
	}
	st := &webStage{id: randomHex(16), client: c, dir: dir}
	w.mu.Lock()
	w.stages[st.id] = st
	w.mu.Unlock()
	return st, nil
}

// dropStage deletes an upload folder.
func (w *webServer) dropStage(st *webStage) {
	w.mu.Lock()
	delete(w.stages, st.id)
	w.mu.Unlock()
	_ = os.RemoveAll(st.dir)
}

// stageFor finds the stage of a request, which must come from its page.
func (w *webServer) stageFor(r *http.Request, id string) *webStage {
	w.mu.Lock()
	st := w.stages[id]
	w.mu.Unlock()
	// The key header also stops other sites: they can't send custom headers
	// without a CORS preflight, which is never allowed.
	if st == nil || r.Header.Get("X-Cbt-Key") != st.client.key {
		return nil
	}
	return st
}

// releaseStaged deletes the upload folders holding paths, once a call is
// done with files dropped on the page.
func (c *webClient) releaseStaged(paths []string) {
	w := c.srv
	var drop []*webStage
	w.mu.Lock()
	for _, st := range w.stages {
		if st.client != c {
			continue
		}
		for _, p := range paths {
			if strings.HasPrefix(p, st.dir+string(filepath.Separator)) {
				drop = append(drop, st)
				break
			}
		}
	}
	w.mu.Unlock()
	for _, st := range drop {
		w.dropStage(st)
	}
}

// handleStageNew starts an upload the page began itself (a drop).
func (w *webServer) handleStageNew(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method", http.StatusMethodNotAllowed)
		return
	}
	var c *webClient
	w.mu.Lock()
	for _, cl := range w.clients {
		if cl.key == r.Header.Get("X-Cbt-Key") {
			c = cl
		}
	}
	w.mu.Unlock()
	if c == nil {
		http.Error(rw, "unknown page", http.StatusForbidden)
		return
	}
	st, err := w.newStage(c)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(rw, map[string]string{"id": st.id})
}

// handleStage receives a file (PUT .../file?p=) or finishes (POST .../done).
func (w *webServer) handleStage(rw http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/web/stage/")
	id, op, _ := strings.Cut(rest, "/")
	st := w.stageFor(r, id)
	if st == nil {
		http.Error(rw, "unknown upload", http.StatusNotFound)
		return
	}
	switch {
	case op == "file" && r.Method == http.MethodPut:
		p, ok := stagePath(st.dir, r.URL.Query().Get("p"))
		if !ok {
			http.Error(rw, "bad path", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(f, r.Body)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	case op == "dir" && r.Method == http.MethodPut:
		// An empty folder that was dropped.
		p, ok := stagePath(st.dir, r.URL.Query().Get("p"))
		if !ok {
			http.Error(rw, "bad path", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		rw.WriteHeader(http.StatusNoContent)
	case op == "done" && r.Method == http.MethodPost:
		var req struct {
			Tops []string `json:"tops"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		var paths []string
		for _, t := range req.Tops {
			p, ok := stagePath(st.dir, t)
			if !ok || strings.ContainsAny(filepath.ToSlash(strings.Trim(t, "/")), "/") {
				http.Error(rw, "bad name", http.StatusBadRequest)
				return
			}
			if _, err := os.Stat(p); err == nil {
				paths = append(paths, p)
			}
		}
		st.client.answerPick(st.id, paths)
		writeJSON(rw, map[string][]string{"paths": paths})
	default:
		http.Error(rw, "bad request", http.StatusBadRequest)
	}
}

// stagePath joins a relative path from the page to dir, refusing anything
// that would leave dir.
func stagePath(dir, rel string) (string, bool) {
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	if rel == "" || strings.ContainsRune(rel, 0) {
		return "", false
	}
	local := filepath.FromSlash(path.Clean(rel))
	if !filepath.IsLocal(local) {
		return "", false
	}
	return filepath.Join(dir, local), true
}

func writeJSON(rw http.ResponseWriter, v interface{}) {
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(v)
}

// answerPick hands the uploaded paths (nil: cancelled) to the picker
// waiting for stage id.
func (c *webClient) answerPick(id string, paths []string) {
	c.mu.Lock()
	ch := c.picks[id]
	delete(c.picks, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- paths // buffered
		close(ch)
	}
}

// offer hands a finished file to the page as a download. dir (if not "")
// is deleted with it.
func (c *webClient) offer(p, name, dir string) {
	w := c.srv
	tok := randomHex(16)
	now := time.Now()
	var expired []*webDownload
	w.mu.Lock()
	for t, d := range w.downloads {
		if now.After(d.expires) {
			expired = append(expired, d)
			delete(w.downloads, t)
		}
	}
	w.downloads[tok] = &webDownload{path: p, name: name, dir: dir, client: c, expires: now.Add(webDownloadTTL)}
	w.mu.Unlock()
	for _, d := range expired {
		d.remove()
	}
	c.event("web:download", []interface{}{map[string]string{"url": "/web/dl/" + tok, "name": name}})
}

func (d *webDownload) remove() {
	_ = os.Remove(d.path)
	if d.dir != "" {
		_ = os.RemoveAll(d.dir)
	}
}

// handleDownload sends an offered file once (ranges are allowed while it lasts).
func (w *webServer) handleDownload(rw http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.URL.Path, "/web/dl/")
	w.mu.Lock()
	d := w.downloads[tok]
	w.mu.Unlock()
	if d == nil {
		http.Error(rw, "gone", http.StatusNotFound)
		return
	}
	f, err := os.Open(d.path)
	if err != nil {
		http.Error(rw, err.Error(), http.StatusNotFound)
		return
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.name}))
	rw.Header().Set("Cache-Control", "no-store")
	http.ServeContent(rw, r, d.name, st.ModTime(), f)
	f.Close()
	if r.Header.Get("Range") == "" && r.Method == http.MethodGet {
		// Fetched whole: done with it.
		w.mu.Lock()
		delete(w.downloads, tok)
		w.mu.Unlock()
		d.remove()
	}
}

// dropFiles deletes a closed client's uploads and downloads.
func (w *webServer) dropFiles(c *webClient) {
	var stages []*webStage
	var downs []*webDownload
	w.mu.Lock()
	for id, st := range w.stages {
		if st.client == c {
			stages = append(stages, st)
			delete(w.stages, id)
		}
	}
	for t, d := range w.downloads {
		if d.client == c {
			downs = append(downs, d)
			delete(w.downloads, t)
		}
	}
	w.mu.Unlock()
	for _, d := range downs {
		d.remove()
	}
	_ = os.RemoveAll(filepath.Join(w.stageRoot, strconv.Itoa(c.id)))
}

// ---- browserPicker ----

func (c *webClient) browserPicker() picker { return browserPicker{c} }

// browserPicker asks a browser on another PC for files by upload and
// download (see the top of this file).
type browserPicker struct{ c *webClient }

// webPick is the web:pick event: show the browser's file chooser.
type webPick struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Multiple bool   `json:"multiple"`
	Accept   string `json:"accept"` // for <input type=file accept=>, e.g. ".lua"
}

// pickTimeout is how long a file chooser may stay open in the page.
var pickTimeout = 10 * time.Minute

func (p browserPicker) pick(o pickOpts, multiple bool) ([]string, func(), error) {
	c := p.c
	st, err := c.srv.newStage(c)
	if err != nil {
		return nil, noRelease, err
	}
	release := func() { c.srv.dropStage(st) }
	ch := make(chan []string, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		release()
		return nil, noRelease, nil
	}
	c.picks[st.id] = ch
	c.mu.Unlock()

	var accept []string
	for _, f := range o.Filters {
		for _, pat := range strings.Split(f.Pattern, ";") {
			if ext := strings.TrimPrefix(strings.TrimSpace(pat), "*"); strings.HasPrefix(ext, ".") {
				accept = append(accept, ext)
			}
		}
	}
	c.event("web:pick", []interface{}{webPick{ID: st.id, Title: o.Title, Multiple: multiple, Accept: strings.Join(accept, ",")}})
	select {
	case paths := <-ch:
		if len(paths) == 0 {
			release()
			return nil, noRelease, nil
		}
		return paths, release, nil
	case <-time.After(pickTimeout):
		c.answerPick(st.id, nil)
		c.event("web:pickDone", []interface{}{st.id})
		release()
		return nil, noRelease, nil
	}
}

func (p browserPicker) OpenFile(o pickOpts) (string, func(), error) {
	paths, release, err := p.pick(o, false)
	if len(paths) == 0 {
		return "", release, err
	}
	return paths[0], release, err
}

func (p browserPicker) OpenFiles(o pickOpts) ([]string, func(), error) {
	return p.pick(o, true)
}

func (p browserPicker) SaveFile(o pickOpts) (string, func(bool) string, error) {
	dir, err := p.c.tempDir()
	if err != nil {
		return "", keepPath(""), err
	}
	name := safeFileName(o.Name, "download")
	file := filepath.Join(dir, name)
	return file, func(ok bool) string {
		if !ok {
			_ = os.RemoveAll(dir)
			return ""
		}
		p.c.offer(file, name, dir)
		return name
	}, nil
}

func (p browserPicker) SaveDir(o pickOpts) (string, func(bool) string, error) {
	dir, err := p.c.tempDir()
	if err != nil {
		return "", keepPath(""), err
	}
	return dir, p.finishDir(dir), nil
}

func (p browserPicker) ReceiveDir() (string, func(bool) string) {
	dir, err := p.c.tempDir()
	if err != nil {
		// Nowhere to stage: fall back to this PC's downloads folder.
		d := transfer.DownloadsDir()
		return d, keepPath(d)
	}
	return dir, p.finishDir(dir)
}

func (browserPicker) Folder(pickOpts) (string, error) { return "", errNotInBrowser }

// finishDir offers what was saved into dir: a single file as it is, more
// (or folders) as one .zip.
func (p browserPicker) finishDir(dir string) func(bool) string {
	return func(ok bool) string {
		ents, _ := os.ReadDir(dir)
		if !ok || len(ents) == 0 {
			_ = os.RemoveAll(dir)
			return ""
		}
		if len(ents) == 1 && ents[0].Type().IsRegular() {
			name := ents[0].Name()
			p.c.offer(filepath.Join(dir, name), name, dir)
			return name
		}
		name := "choboterm-" + time.Now().Format("20060102-150405") + ".zip"
		if len(ents) == 1 {
			name = ents[0].Name() + ".zip"
		}
		zipDir, err := p.c.tempDir()
		if err != nil {
			_ = os.RemoveAll(dir)
			return ""
		}
		zp := filepath.Join(zipDir, name)
		err = zipFolder(dir, zp)
		_ = os.RemoveAll(dir)
		if err != nil {
			dbg.Printf("web: zip: %v", err)
			_ = os.RemoveAll(zipDir)
			return ""
		}
		p.c.offer(zp, name, zipDir)
		return name
	}
}

// zipFolder writes the contents of dir into the zip file dst.
func zipFolder(dir, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			_, err = zw.Create(name + "/")
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name, h.Method = name, zip.Deflate
		h.Flags |= 0x800 // UTF-8 names
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, src)
		src.Close()
		return err
	})
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// safeFileName makes a name from the page or a server usable as one file name.
func safeFileName(name, fallback string) string {
	// path.Base, not filepath.Base: on Windows "a:b.txt" would lose "a:" as
	// if it were a drive. Both separators count, as names come from anywhere.
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return fallback
	}
	return name
}
