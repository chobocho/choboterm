package app

// Where the web server meets the rest of the app, as a browser sees it over
// HTTP and the WebSocket: logging in with the form, picking and saving files
// (the browser picker), calls only the window may make, and what goes away
// with a page. Written before the server moved out of this package, so that
// the move can be checked against them.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"choboterm/internal/terminal"
	"choboterm/internal/transfer"
)

// noRedirect is a client of f that shows redirects instead of following them.
func (f *webFixture) noRedirect() *http.Client {
	hc := *f.hc
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &hc
}

func TestWebLoginForm(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	hc := f.noRedirect()

	// Without the cookie the page is the login form.
	if code, body := f.get("/"); code != http.StatusUnauthorized || !strings.Contains(body, `action="/login"`) || strings.Contains(body, `class="err"`) {
		t.Fatalf("login page %d %q", code, body)
	}
	// Only a POST logs in.
	r, err := hc.Get(f.base + "/login")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" {
		t.Fatalf("GET /login: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	post := func(token string) *http.Response {
		r, err := hc.PostForm(f.base+"/login", url.Values{"token": {token}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r = post("wrong")
	var b bytes.Buffer
	b.ReadFrom(r.Body)
	r.Body.Close()
	if r.StatusCode != http.StatusUnauthorized || !strings.Contains(b.String(), `class="err"`) {
		t.Fatalf("wrong token: %d %q", r.StatusCode, b.String())
	}
	if code, _ := f.get("/"); code != http.StatusUnauthorized {
		t.Fatalf("logged in by a wrong token: %d", code)
	}
	// The right one (spaces around it are fine) sets the cookie.
	r = post("  " + f.w.token + "\n")
	r.Body.Close()
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" {
		t.Fatalf("right token: %d", r.StatusCode)
	}
	if p := f.page(); p.ID == 0 || p.Base != p.ID<<tabBits {
		t.Fatalf("page %+v", p)
	}
}

// The page learns who it is and where it runs from window.__cbt.
func TestWebPageInfo(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	_, body := f.get("/")
	m := cbtRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no info in %q", body)
	}
	var info map[string]interface{}
	if err := json.Unmarshal([]byte(m[1]), &info); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range info {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if got := strings.Join(keys, ","); got != "arch,base,id,key,lang,local,platform,window" {
		t.Fatalf("keys %s", got)
	}
	// No Wails window in a test: the page must not offer window features.
	if info["window"] != false {
		t.Fatalf("window %v", info["window"])
	}
	if !strings.Contains(body, `<script src="/web/shim.js"></script>`) {
		t.Fatal("no shim")
	}
	if code, js := f.get("/web/shim.js"); code != 200 || !bytes.Equal([]byte(js), webShim) {
		t.Fatalf("shim %d", code)
	}
}

// What only the window may do is refused to a browser, and changes nothing.
func TestWebWindowOnlyMethods(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	c := f.connect(f.page())
	for _, m := range []string{"WebStart", "WebStop", "WebOpen", "WebNewToken", "WebHideWindow"} {
		_, x := c.callID(c.callAsync(m))
		if x == nil || *x != errWindowOnly.Error() {
			t.Errorf("%s: %v", m, x)
		}
	}
	// Quit has no result to refuse with: it just does nothing.
	if v := c.callOK("Quit"); string(v) != "null" {
		t.Fatalf("Quit: %s", v)
	}
	if f.a.web.Load() != f.w || f.a.quit.Load() {
		t.Fatal("a page changed the server or quit")
	}
}

// browserTab is a page with a tab that has files (an SFTP or FTP tab).
func browserTab(t *testing.T) (*webFixture, pageInfo, *wsConn, int, *memFS) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	c := f.connect(p)
	id := p.Base + 1
	fs := newMemFS()
	tb := f.a.getTab(id)
	tb.files.fs, tb.files.proto = fs, "TEST"
	return f, p, c, id, fs
}

// upload answers a web:pick with files: PUT each, then done with tops.
func (f *webFixture) upload(p pageInfo, stage string, files map[string]string, tops ...string) []string {
	f.t.Helper()
	do := func(method, path, body string) *http.Response {
		req, _ := http.NewRequest(method, f.base+path, strings.NewReader(body))
		req.Header.Set("X-Cbt-Key", p.Key)
		r, err := f.hc.Do(req)
		if err != nil {
			f.t.Fatal(err)
		}
		return r
	}
	for name, body := range files {
		r := do("PUT", "/web/stage/"+stage+"/file?p="+url.QueryEscape(name), body)
		r.Body.Close()
		if r.StatusCode != http.StatusNoContent {
			f.t.Fatalf("put %s: %d", name, r.StatusCode)
		}
	}
	b, _ := json.Marshal(map[string][]string{"tops": tops})
	r := do("POST", "/web/stage/"+stage+"/done", string(b))
	defer r.Body.Close()
	var res struct{ Paths []string }
	json.NewDecoder(r.Body).Decode(&res)
	return res.Paths
}

func (c *wsConn) pick() webPick {
	c.t.Helper()
	m := c.readUntil(func(m msg) bool { return m.E == "web:pick" })
	var p webPick
	json.Unmarshal(m.D[0], &p)
	return p
}

type offered struct{ URL, Name string }

func (c *wsConn) download() offered {
	c.t.Helper()
	m := c.readUntil(func(m msg) bool { return m.E == "web:download" })
	var d offered
	json.Unmarshal(m.D[0], &d)
	return d
}

// FileUpload from a browser: several files picked at once, uploaded, sent
// on, and the uploaded copies deleted afterwards.
func TestWebBrowserUploadsSeveralFiles(t *testing.T) {
	f, p, c, id, fs := browserTab(t)
	call := c.callAsync("FileUpload", id, "/up")
	pick := c.pick()
	if !pick.Multiple || pick.Accept != "" {
		t.Fatalf("pick %+v", pick)
	}
	paths := f.upload(p, pick.ID, map[string]string{"a.txt": "A", "b.txt": "BB"}, "a.txt", "b.txt")
	v, x := c.callID(call)
	var n int
	json.Unmarshal(v, &n)
	if x != nil || n != 2 || string(fs.files["/up/a.txt"]) != "A" || string(fs.files["/up/b.txt"]) != "BB" {
		t.Fatalf("upload %d %v %v", n, x, fs.files)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("upload left %s", p)
		}
	}
	// Cancelled: nothing uploaded, no error.
	call = c.callAsync("FileUpload", id, "/up2")
	c.send(map[string]string{"cancel": c.pick().ID})
	if v, x := c.callID(call); x != nil || string(v) != "0" {
		t.Fatalf("cancel %s %v", v, x)
	}
}

// FileDownloadMany to a browser: one file comes as it is, more as a .zip.
func TestWebBrowserSavesAFolder(t *testing.T) {
	f, _, c, id, fs := browserTab(t)
	fs.files["/d/a.txt"] = []byte("a")
	fs.files["/d/sub/b.txt"] = []byte("bb")

	call := c.callAsync("FileDownloadMany", id, "/d", []transfer.FileEntry{{Name: "a.txt", Size: 1}})
	d := c.download()
	v, x := c.callID(call)
	var res DownloadResult
	json.Unmarshal(v, &res)
	if x != nil || res.Count != 1 || res.Dir != "a.txt" || d.Name != "a.txt" {
		t.Fatalf("one file: %+v %v %+v", res, x, d)
	}
	if code, body := f.get(d.URL); code != 200 || body != "a" {
		t.Fatalf("download %d %q", code, body)
	}

	call = c.callAsync("FileDownloadMany", id, "/", []transfer.FileEntry{{Name: "d", IsDir: true}})
	d = c.download()
	v, x = c.callID(call)
	json.Unmarshal(v, &res)
	if x != nil || res.Count != 2 || res.Dir != "d.zip" || d.Name != "d.zip" {
		t.Fatalf("folder: %+v %v %+v", res, x, d)
	}
	code, body := f.get(d.URL)
	if code != 200 {
		t.Fatalf("zip download %d", code)
	}
	zr, err := zip.NewReader(strings.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range zr.File {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	if got := strings.Join(names, ","); got != "d/,d/a.txt,d/sub/,d/sub/b.txt" {
		t.Fatalf("zip %s", got)
	}

	// Two entries at the top: a .zip named after the time.
	call = c.callAsync("FileDownloadMany", id, "/d", []transfer.FileEntry{{Name: "a.txt"}, {Name: "sub", IsDir: true}})
	d = c.download()
	c.callID(call)
	if !strings.HasPrefix(d.Name, "choboterm-") || !strings.HasSuffix(d.Name, ".zip") {
		t.Fatalf("name %q", d.Name)
	}
}

// Files arriving without a dialog (Zmodem) go to a folder of the page's and
// are offered as a download when done; nothing is offered after a failure.
func TestWebBrowserReceiveDir(t *testing.T) {
	f, p, c, _, _ := browserTab(t)
	view := &App{appState: f.a.appState, caller: f.w.client(p.ID)}
	dir, finish := view.picker().receiveDir()
	if !strings.HasPrefix(dir, f.w.stageRoot) {
		t.Fatalf("dir %s outside %s", dir, f.w.stageRoot)
	}
	os.WriteFile(filepath.Join(dir, "r.bin"), []byte("zm"), 0o600)
	if name := finish(true); name != "r.bin" {
		t.Fatalf("finish %q", name)
	}
	if d := c.download(); d.Name != "r.bin" {
		t.Fatalf("offered %+v", d)
	}
	dir, finish = view.picker().receiveDir()
	if name := finish(false); name != "" {
		t.Fatalf("failed finish %q", name)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("failed receive left its folder")
	}
}

// A page that goes away takes its prompts, downloads and uploads with it.
func TestWebClosedPageDropsItsThings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := startTestWeb(t, webOptions{Grace: 200 * time.Millisecond})
	f.login()
	p := f.page()
	c := f.connect(p)
	id := p.Base + 1

	// A download offered but not fetched yet.
	call := c.callAsync("ConsoleSave", "text")
	d := c.download()
	c.callID(call)
	// An upload started but not finished.
	r, _ := http.NewRequest("POST", f.base+"/web/stage", nil)
	r.Header.Set("X-Cbt-Key", p.Key)
	resp, err := f.hc.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	var st struct{ ID string }
	json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	// A prompt waiting in the page's tab.
	asked := make(chan bool)
	go func() {
		_, ok := f.a.ask(id, "t", "m", []terminal.PromptField{{Label: "x"}})
		asked <- ok
	}()
	c.readUntil(func(m msg) bool { return m.E == "auth:prompt" })

	c.c.Close()
	select {
	case ok := <-asked:
		if ok {
			t.Fatal("prompt answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("prompt still waiting after the page closed")
	}
	// Prompts go first; the files when the page is forgotten.
	for deadline := time.Now().Add(5 * time.Second); f.w.clientCount() != 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("page not closed")
		}
	}
	if code, _ := f.get(d.URL); code != http.StatusNotFound {
		t.Fatalf("download after close: %d", code)
	}
	if _, err := os.Stat(filepath.Join(f.w.stageRoot, strconv.Itoa(p.ID))); !os.IsNotExist(err) {
		t.Fatal("page's files left")
	}
	r, _ = http.NewRequest("PUT", f.base+"/web/stage/"+st.ID+"/file?p=x", strings.NewReader("x"))
	r.Header.Set("X-Cbt-Key", p.Key)
	if resp, err := f.hc.Do(r); err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("upload after close: %v %v", resp, err)
	}
}
