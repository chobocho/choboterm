package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RemoteFS is a remote file system reachable over SFTP or FTP.
// Paths are slash-separated and absolute.
type RemoteFS interface {
	Getwd() (string, error)
	List(dir string) ([]FileEntry, error)
	Download(remotePath string, w io.Writer) error
	Upload(remotePath string, r io.Reader, size int64) error
	// Remove deletes a file, or a folder with everything in it.
	Remove(p string, isDir bool) error
	Rename(from, to string) error
	Mkdir(p string) error
	Close() error
}

// FileEntry is one row of the remote file list.
type FileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	ModTime string `json:"modTime"`
}

// XferProgress is emitted as "xfer:progress" while a file is transferred.
type XferProgress struct {
	Name   string `json:"name"`
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	Upload bool   `json:"upload"`
}

// XferEnd is emitted as "xfer:end" when a transfer finishes, fails or is cancelled.
type XferEnd struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

var errCancelled = errors.New("전송이 취소되었습니다")

// fileState holds the remote file system and the running transfer.
type fileState struct {
	mu     sync.Mutex // serializes remote operations (FTP allows only one at a time)
	fs     RemoteFS
	proto  string     // "SFTP", "SCP" or "FTP", shown in the file window title
	cmu    sync.Mutex // guards cancel, which must be reachable while mu is held
	cancel context.CancelFunc
}

func (s *fileState) setCancel(c context.CancelFunc) {
	s.cmu.Lock()
	s.cancel = c
	s.cmu.Unlock()
}

func sortEntries(list []FileEntry) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDir != list[j].IsDir {
			return list[i].IsDir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
}

// progress reports transfer progress (throttled) and aborts on cancel.
type progress struct {
	ctx  context.Context
	emit func(XferProgress)
	info XferProgress
	last time.Time
}

func (p *progress) add(n int) error {
	p.info.Done += int64(n)
	if now := time.Now(); now.Sub(p.last) >= 100*time.Millisecond || p.info.Done == p.info.Total {
		p.last = now
		p.emit(p.info)
	}
	if p.ctx.Err() != nil {
		return errCancelled
	}
	return nil
}

type progressWriter struct {
	w io.Writer
	p *progress
}

func (pw progressWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	if perr := pw.p.add(n); err == nil {
		err = perr
	}
	return n, err
}

type progressReader struct {
	r io.Reader
	p *progress
}

func (pr progressReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	if perr := pr.p.add(n); err == nil {
		err = perr
	}
	return n, err
}

func downloadsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, "Downloads")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
		return home
	}
	return "."
}

// ---- App bindings (per tab) ----

// FileOpenResult tells the file window where to start and which protocol is used.
type FileOpenResult struct {
	Home     string `json:"home"`
	Protocol string `json:"protocol"`
}

// FileOpen prepares remote file access for a tab's connection.
// SSH connections use SFTP, or SCP when the server has no SFTP subsystem.
func (a *App) FileOpen(tabID int) (FileOpenResult, error) {
	t := a.findTab(tabID)
	if t == nil {
		return FileOpenResult{}, errors.New("연결되어 있지 않습니다")
	}
	t.files.mu.Lock()
	defer t.files.mu.Unlock()

	if t.files.fs == nil {
		sess, _ := t.session().(*sshSession)
		if sess == nil {
			return FileOpenResult{}, errors.New("파일 전송은 SSH(SFTP/SCP) 또는 FTP 접속에서 사용할 수 있습니다.\nTelnet에서는 sz / rz (Zmodem)를 사용하세요.")
		}
		if fs, err := newSFTP(sess.client); err == nil {
			t.files.fs, t.files.proto = fs, "SFTP"
		} else {
			scp, serr := newSCP(sess.client, t.codec)
			if serr != nil {
				return FileOpenResult{}, fmt.Errorf("SFTP를 열 수 없고 (%v), SCP도 사용할 수 없습니다 (%v)", err, serr)
			}
			t.files.fs, t.files.proto = scp, "SCP"
		}
	}
	home, err := t.files.fs.Getwd()
	return FileOpenResult{Home: home, Protocol: t.files.proto}, err
}

// FileList lists a remote directory, folders first.
func (a *App) FileList(tabID int, dir string) ([]FileEntry, error) {
	t := a.findTab(tabID)
	if t == nil {
		return nil, errors.New("연결되어 있지 않습니다")
	}
	t.files.mu.Lock()
	defer t.files.mu.Unlock()
	if t.files.fs == nil {
		return nil, errors.New("연결되어 있지 않습니다")
	}
	list, err := t.files.fs.List(dir)
	if err != nil {
		return nil, err
	}
	sortEntries(list)
	return list, nil
}

// FileDownload asks where to save remotePath and downloads it.
// Returns the local path, or "" if the user cancelled the dialog.
func (a *App) FileDownload(tabID int, remotePath string, size int64) (string, error) {
	t := a.findTab(tabID)
	if t == nil {
		return "", errors.New("연결되어 있지 않습니다")
	}
	local, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "다운로드",
		DefaultDirectory: downloadsDir(),
		DefaultFilename:  path.Base(remotePath),
	})
	if err != nil || local == "" {
		return "", err
	}

	f, err := os.Create(local)
	if err != nil {
		return "", err
	}
	err = t.transfer(path.Base(remotePath), size, false, func(fs RemoteFS, p *progress) error {
		return fs.Download(remotePath, progressWriter{f, p})
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(local)
		return "", err
	}
	return local, nil
}

// FileUpload asks for local files and uploads them into remoteDir.
// Returns the number of uploaded files.
func (a *App) FileUpload(tabID int, remoteDir string) (int, error) {
	t := a.findTab(tabID)
	if t == nil {
		return 0, errors.New("연결되어 있지 않습니다")
	}
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "업로드할 파일 선택",
	})
	if err != nil || len(files) == 0 {
		return 0, err
	}
	return t.uploadPaths(remoteDir, files)
}

// FileUploadPaths uploads local files and folders (with their contents) into
// remoteDir, e.g. ones dropped on the file window. Returns the number of files.
func (a *App) FileUploadPaths(tabID int, remoteDir string, paths []string) (int, error) {
	t := a.findTab(tabID)
	if t == nil {
		return 0, errors.New("연결되어 있지 않습니다")
	}
	return t.uploadPaths(remoteDir, paths)
}

// upItem is one folder to create or file to send.
type upItem struct {
	local, remote string
	size          int64
	dir           bool
}

func (t *tab) uploadPaths(remoteDir string, paths []string) (int, error) {
	var items []upItem
	files := 0
	for _, local := range paths {
		st, err := os.Stat(local)
		if err != nil {
			return 0, err
		}
		top := path.Join(remoteDir, filepath.Base(local))
		if !st.IsDir() {
			items = append(items, upItem{local: local, remote: top, size: st.Size()})
			files++
			continue
		}
		err = filepath.WalkDir(local, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(local, p)
			remote := path.Join(top, filepath.ToSlash(rel))
			if d.IsDir() {
				items = append(items, upItem{remote: remote, dir: true})
				return nil
			}
			if !d.Type().IsRegular() {
				return nil // skip links, devices...
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			items = append(items, upItem{local: p, remote: remote, size: info.Size()})
			files++
			return nil
		})
		if err != nil {
			return 0, err
		}
	}

	done := 0
	for _, it := range items {
		if it.dir {
			// It may already exist; a real problem shows up when its files are sent.
			_ = t.withFS(func(fs RemoteFS) error { return fs.Mkdir(it.remote) })
			continue
		}
		f, err := os.Open(it.local)
		if err != nil {
			return done, err
		}
		name := path.Base(it.remote)
		if files > 1 {
			name = fmt.Sprintf("(%d/%d) %s", done+1, files, name)
		}
		err = t.transfer(name, it.size, true, func(fs RemoteFS, p *progress) error {
			return fs.Upload(it.remote, progressReader{f, p}, it.size)
		})
		f.Close()
		if err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// DownloadResult tells where several downloaded files went.
type DownloadResult struct {
	Dir   string `json:"dir"`
	Count int    `json:"count"`
}

// FileDownloadMany asks for a local folder and downloads entries of dir into
// it, folders with all their contents. Existing names get " (1)" etc.
// Dir is "" if the user cancelled the dialog.
func (a *App) FileDownloadMany(tabID int, dir string, entries []FileEntry) (DownloadResult, error) {
	t := a.findTab(tabID)
	if t == nil {
		return DownloadResult{}, errors.New("연결되어 있지 않습니다")
	}
	localDir := a.hooks.downloadDir
	if localDir == "" {
		d, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
			Title:            "저장할 폴더 선택",
			DefaultDirectory: downloadsDir(),
		})
		if err != nil || d == "" {
			return DownloadResult{}, err
		}
		localDir = d
	}

	// Collect the files first, so progress can say "(3/10)".
	type downItem struct {
		remote, local string
		size          int64
	}
	var items []downItem
	var collect func(fs RemoteFS, remote, local string, e FileEntry) error
	collect = func(fs RemoteFS, remote, local string, e FileEntry) error {
		if !e.IsDir {
			items = append(items, downItem{remote, local, e.Size})
			return nil
		}
		if err := os.MkdirAll(local, 0o755); err != nil {
			return err
		}
		list, err := fs.List(remote)
		if err != nil {
			return err
		}
		for _, c := range list {
			if err := collect(fs, path.Join(remote, c.Name), filepath.Join(local, c.Name), c); err != nil {
				return err
			}
		}
		return nil
	}
	err := t.withFS(func(fs RemoteFS) error {
		for _, e := range entries {
			if err := collect(fs, path.Join(dir, e.Name), uniquePath(localDir, e.Name), e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return DownloadResult{}, err
	}

	res := DownloadResult{Dir: localDir}
	for _, it := range items {
		name := path.Base(it.remote)
		if len(items) > 1 {
			name = fmt.Sprintf("(%d/%d) %s", res.Count+1, len(items), name)
		}
		f, err := os.Create(it.local)
		if err != nil {
			return res, err
		}
		err = t.transfer(name, it.size, false, func(fs RemoteFS, p *progress) error {
			return fs.Download(it.remote, progressWriter{f, p})
		})
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(it.local)
			return res, err
		}
		res.Count++
	}
	return res, nil
}

// withFS runs fn with the tab's remote file system, one operation at a time.
func (t *tab) withFS(fn func(RemoteFS) error) error {
	t.files.mu.Lock()
	defer t.files.mu.Unlock()
	if t.files.fs == nil {
		return errors.New("연결되어 있지 않습니다")
	}
	return fn(t.files.fs)
}

// checkName rejects names that aren't a single path element.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("이름이 올바르지 않습니다: %q", name)
	}
	return nil
}

// FileDelete deletes entries of dir; folders are deleted with their contents.
// Returns how many were deleted before an error.
func (a *App) FileDelete(tabID int, dir string, entries []FileEntry) (int, error) {
	t := a.findTab(tabID)
	if t == nil {
		return 0, errors.New("연결되어 있지 않습니다")
	}
	count := 0
	err := t.withFS(func(fs RemoteFS) error {
		for _, e := range entries {
			if err := checkName(e.Name); err != nil {
				return err
			}
			if err := fs.Remove(path.Join(dir, e.Name), e.IsDir); err != nil {
				return fmt.Errorf("%s: %w", e.Name, err)
			}
			count++
		}
		return nil
	})
	return count, err
}

// FileRename renames from to to inside dir.
func (a *App) FileRename(tabID int, dir, from, to string) error {
	t := a.findTab(tabID)
	if t == nil {
		return errors.New("연결되어 있지 않습니다")
	}
	if err := checkName(to); err != nil {
		return err
	}
	return t.withFS(func(fs RemoteFS) error {
		return fs.Rename(path.Join(dir, from), path.Join(dir, to))
	})
}

// FileMkdir creates the folder name inside dir.
func (a *App) FileMkdir(tabID int, dir, name string) error {
	t := a.findTab(tabID)
	if t == nil {
		return errors.New("연결되어 있지 않습니다")
	}
	if err := checkName(name); err != nil {
		return err
	}
	return t.withFS(func(fs RemoteFS) error { return fs.Mkdir(path.Join(dir, name)) })
}

// FileCreate creates the empty file name inside dir. It never replaces
// a file or folder that is already there.
func (a *App) FileCreate(tabID int, dir, name string) error {
	t := a.findTab(tabID)
	if t == nil {
		return errors.New("연결되어 있지 않습니다")
	}
	if err := checkName(name); err != nil {
		return err
	}
	return t.withFS(func(fs RemoteFS) error {
		list, err := fs.List(dir)
		if err != nil {
			return err
		}
		for _, e := range list {
			if e.Name == name {
				return fmt.Errorf("같은 이름이 이미 있습니다: %s", name)
			}
		}
		return fs.Upload(path.Join(dir, name), strings.NewReader(""), 0)
	})
}

// transfer runs one cancellable file transfer with progress events.
func (t *tab) transfer(name string, size int64, upload bool, run func(RemoteFS, *progress) error) error {
	t.files.mu.Lock()
	defer t.files.mu.Unlock()
	if t.files.fs == nil {
		return errors.New("연결되어 있지 않습니다")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.files.setCancel(cancel)
	defer func() {
		cancel()
		t.files.setCancel(nil)
	}()

	p := &progress{
		ctx:  ctx,
		emit: func(x XferProgress) { t.emit("xfer:progress", x) },
		info: XferProgress{Name: name, Total: size, Upload: upload},
	}
	p.emit(p.info)
	err := run(t.files.fs, p)
	if err != nil && ctx.Err() != nil {
		err = errCancelled
	}
	end := XferEnd{OK: err == nil, Message: name}
	if err != nil {
		end.Message = err.Error()
	}
	t.emit("xfer:end", end)
	return err
}

// FileCancel aborts a tab's running transfer (SFTP/SCP/FTP or Zmodem).
func (a *App) FileCancel(tabID int) {
	if t := a.findTab(tabID); t != nil {
		t.fileCancel()
	}
}

func (t *tab) fileCancel() {
	t.cancelZmodem()
	t.files.cmu.Lock()
	c := t.files.cancel
	t.files.cmu.Unlock()
	if c != nil {
		c()
	}
}

// FileClose releases a tab's remote file system (SFTP channel or FTP connection).
func (a *App) FileClose(tabID int) {
	if t := a.findTab(tabID); t != nil {
		t.fileClose()
	}
}

func (t *tab) fileClose() {
	t.fileCancel()
	t.files.mu.Lock()
	fs := t.files.fs
	t.files.fs, t.files.proto = nil, ""
	t.files.mu.Unlock()
	if fs != nil {
		_ = fs.Close()
	}
}

// FileStartDir turns the shell's folder seen in the terminal (from the prompt,
// the window title or OSC 7) into an absolute remote path. Returns "" if it
// can't be resolved or isn't a folder that can be listed.
func (a *App) FileStartDir(tabID int, dir string) string {
	t := a.findTab(tabID)
	if t == nil {
		return ""
	}
	if strings.HasPrefix(dir, "~") {
		rest := dir[1:]
		if rest != "" && rest[0] != '/' {
			return "" // ~user
		}
		home := t.remoteHome()
		if home == "" {
			return ""
		}
		dir = home + rest
	}
	if !path.IsAbs(dir) {
		return ""
	}
	dir = path.Clean(dir)
	if err := t.withFS(func(fs RemoteFS) error {
		_, err := fs.List(dir)
		return err
	}); err != nil {
		return ""
	}
	return dir
}

// remoteHome is the login user's home folder: $HOME from the shell, or the
// file system's start folder when commands can't be run.
func (t *tab) remoteHome() string {
	if sess, _ := t.session().(*sshSession); sess != nil {
		if s, err := sess.client.NewSession(); err == nil {
			out, err := s.Output(`printf '%s' "$HOME"`)
			s.Close()
			if home := t.codec.DecodeString(string(out)); err == nil && path.IsAbs(home) {
				return home
			}
		}
	}
	var home string
	_ = t.withFS(func(fs RemoteFS) (err error) {
		home, err = fs.Getwd()
		return err
	})
	return home
}
