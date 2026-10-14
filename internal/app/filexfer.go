package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"choboterm/internal/dialog"
	"choboterm/internal/i18n"
	"choboterm/internal/sshconn"
	"choboterm/internal/transfer"
)

// partSuffix marks a download in progress; it is renamed when complete, and
// kept when the transfer breaks so it can be resumed.
const partSuffix = ".part"

// brokenUploads remembers uploads that broke off, by server and remote path,
// with the local file they came from. Only those are offered to be continued:
// a smaller file on the server may just be an older version.
var brokenUploads = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func (t *tab) uploadKey(remote string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("%s:%d|%s", t.host, t.port, remote)
}

// localID identifies a local file's version: path, size and time.
func localID(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s|%d|%d", p, st.Size(), st.ModTime().UnixNano())
}

// fileState holds the remote file system and the running transfer.
type fileState struct {
	mu     sync.Mutex // serializes remote operations (FTP allows only one at a time)
	fs     transfer.RemoteFS
	proto  string     // "SFTP", "SCP" or "FTP", shown in the file window title
	cmu    sync.Mutex // guards cancel, which must be reachable while mu is held
	cancel context.CancelFunc
}

func (s *fileState) setCancel(c context.CancelFunc) {
	s.cmu.Lock()
	s.cancel = c
	s.cmu.Unlock()
}

// canResume tells whether the tab's file system can continue transfers.
func (t *tab) canResume() bool {
	ok := false
	_ = t.withFS(func(fs transfer.RemoteFS) error {
		_, ok = fs.(transfer.Resumer)
		return nil
	})
	return ok
}

// downloadFile downloads remotePath (size bytes) to local through local.part.
// A .part left by a broken download of the same file is continued if the user
// agrees; a failed download keeps its .part for next time.
func (t *tab) downloadFile(remotePath, local string, size int64, name string) error {
	part := local + partSuffix
	var offset int64
	if st, err := os.Stat(part); err == nil && st.Mode().IsRegular() && st.Size() > 0 && st.Size() < size && t.canResume() {
		msg := fmt.Sprintf("%s\n\n받다 만 파일이 있습니다: %s / %s\n이어서 받을까요? \"처음부터\"는 새로 받습니다.",
			filepath.Base(local), transfer.HumanSize(st.Size()), transfer.HumanSize(size))
		if t.app.choose(t.id, "이어받기", msg, "이어받기", "처음부터") {
			offset = st.Size()
		}
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if offset > 0 {
		flags = os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return err
	}
	err = t.transfer(name, size, false, func(fs transfer.RemoteFS, p *transfer.Progress) error {
		w := transfer.ProgressWriter{W: f, P: p}
		if offset > 0 {
			p.Info.Done = offset
			return fs.(transfer.Resumer).DownloadFrom(remotePath, offset, w)
		}
		return fs.Download(remotePath, w)
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if st, serr := os.Stat(part); serr == nil && st.Size() == 0 {
			_ = os.Remove(part) // nothing to resume
		}
		return err
	}
	_ = os.Remove(local) // the save dialog already asked about replacing it
	return os.Rename(part, local)
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
		sess, _ := t.session().(*sshconn.Session)
		if sess == nil {
			return FileOpenResult{}, errors.New("파일 전송은 SSH(SFTP/SCP) 또는 FTP 접속에서 사용할 수 있습니다.\nTelnet에서는 sz / rz (Zmodem)를 사용하세요.")
		}
		if fs, err := transfer.NewSFTP(sess.Client); err == nil {
			t.files.fs, t.files.proto = fs, "SFTP"
		} else {
			scp, serr := transfer.NewSCP(sess.Client, t.codec)
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
func (a *App) FileList(tabID int, dir string) ([]transfer.FileEntry, error) {
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
	transfer.SortEntries(list)
	return list, nil
}

// FileDownload asks where to save remotePath and downloads it.
// Returns the local path, or "" if the user cancelled the dialog.
func (a *App) FileDownload(tabID int, remotePath string, size int64) (string, error) {
	t := a.findTab(tabID)
	if t == nil {
		return "", errors.New("연결되어 있지 않습니다")
	}
	local, finish, err := a.picker().SaveFile(dialog.Opts{
		Title: i18n.Tr("다운로드"),
		Dir:   transfer.DownloadsDir(),
		Name:  path.Base(remotePath),
	})
	if err != nil || local == "" {
		return "", err
	}

	if err := t.downloadFile(remotePath, local, size, path.Base(remotePath)); err != nil {
		finish(false)
		return "", err
	}
	return finish(true), nil
}

// FileUpload asks for local files and uploads them into remoteDir.
// Returns the number of uploaded files.
func (a *App) FileUpload(tabID int, remoteDir string) (int, error) {
	t := a.findTab(tabID)
	if t == nil {
		return 0, errors.New("연결되어 있지 않습니다")
	}
	files, release, err := a.picker().OpenFiles(dialog.Opts{Title: i18n.Tr("업로드할 파일 선택")})
	defer release()
	if err != nil || len(files) == 0 {
		return 0, err
	}
	return t.uploadPaths(remoteDir, files)
}

// FileUploadPaths uploads local files and folders (with their contents) into
// remoteDir, e.g. ones dropped on the file window. Returns the number of files.
func (a *App) FileUploadPaths(tabID int, remoteDir string, paths []string) (int, error) {
	if a.caller != nil {
		defer a.caller.ReleaseStaged(paths) // files a browser dropped (see internal/web/files.go)
	}
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

	// Files already partly on the server (a broken upload) may be continued.
	resume := t.canResume()
	listed := map[string]map[string]transfer.FileEntry{} // remote folder → its files, read when needed
	remoteSize := func(p string) int64 {
		dir := path.Dir(p)
		if listed[dir] == nil {
			listed[dir] = map[string]transfer.FileEntry{}
			_ = t.withFS(func(fs transfer.RemoteFS) error {
				list, err := fs.List(dir)
				for _, e := range list {
					listed[dir][e.Name] = e
				}
				return err
			})
		}
		if e, ok := listed[dir][path.Base(p)]; ok && !e.IsDir {
			return e.Size
		}
		return -1
	}

	done := 0
	for _, it := range items {
		if it.dir {
			// It may already exist; a real problem shows up when its files are sent.
			_ = t.withFS(func(fs transfer.RemoteFS) error { return fs.Mkdir(it.remote) })
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
		var offset int64
		key := t.uploadKey(it.remote)
		brokenUploads.Lock()
		broke := brokenUploads.m[key]
		brokenUploads.Unlock()
		if resume && it.size > 0 && broke != "" && broke == localID(it.local) {
			if have := remoteSize(it.remote); have > 0 && have < it.size {
				msg := fmt.Sprintf("%s\n\n서버에 일부만 올라간 파일이 있습니다: %s / %s\n이어서 올릴까요? \"처음부터\"는 새로 올립니다.",
					path.Base(it.remote), transfer.HumanSize(have), transfer.HumanSize(it.size))
				if t.app.choose(t.id, "이어 올리기", msg, "이어 올리기", "처음부터") {
					if _, err := f.Seek(have, io.SeekStart); err != nil {
						f.Close()
						return done, err
					}
					offset = have
				}
			}
		}
		err = t.transfer(name, it.size, true, func(fs transfer.RemoteFS, p *transfer.Progress) error {
			if offset > 0 {
				p.Info.Done = offset
				return fs.(transfer.Resumer).UploadFrom(it.remote, transfer.ProgressReader{R: f, P: p}, offset)
			}
			return fs.Upload(it.remote, transfer.ProgressReader{R: f, P: p}, it.size)
		})
		f.Close()
		brokenUploads.Lock()
		if err != nil {
			brokenUploads.m[key] = localID(it.local)
		} else {
			delete(brokenUploads.m, key)
		}
		brokenUploads.Unlock()
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
func (a *App) FileDownloadMany(tabID int, dir string, entries []transfer.FileEntry) (DownloadResult, error) {
	t := a.findTab(tabID)
	if t == nil {
		return DownloadResult{}, errors.New("연결되어 있지 않습니다")
	}
	localDir := a.hooks.downloadDir
	finish := dialog.KeepPath(localDir)
	if localDir == "" {
		d, f, err := a.picker().SaveDir(dialog.Opts{
			Title: i18n.Tr("저장할 폴더 선택"),
			Dir:   transfer.DownloadsDir(),
		})
		if err != nil || d == "" {
			return DownloadResult{}, err
		}
		localDir, finish = d, f
	}

	// Collect the files first, so progress can say "(3/10)".
	type downItem struct {
		remote, local string
		size          int64
	}
	var items []downItem
	var collect func(fs transfer.RemoteFS, remote, local string, e transfer.FileEntry) error
	collect = func(fs transfer.RemoteFS, remote, local string, e transfer.FileEntry) error {
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
	err := t.withFS(func(fs transfer.RemoteFS) error {
		for _, e := range entries {
			if err := collect(fs, path.Join(dir, e.Name), transfer.UniquePath(localDir, e.Name), e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		finish(false)
		return DownloadResult{}, err
	}

	res := DownloadResult{Dir: localDir}
	for _, it := range items {
		name := path.Base(it.remote)
		if len(items) > 1 {
			name = fmt.Sprintf("(%d/%d) %s", res.Count+1, len(items), name)
		}
		if err := t.downloadFile(it.remote, it.local, it.size, name); err != nil {
			// The files that did arrive stay, as in the folder of a native dialog.
			res.Dir = finish(res.Count > 0)
			return res, err
		}
		res.Count++
	}
	res.Dir = finish(true)
	return res, nil
}

// withFS runs fn with the tab's remote file system, one operation at a time.
func (t *tab) withFS(fn func(transfer.RemoteFS) error) error {
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
func (a *App) FileDelete(tabID int, dir string, entries []transfer.FileEntry) (int, error) {
	t := a.findTab(tabID)
	if t == nil {
		return 0, errors.New("연결되어 있지 않습니다")
	}
	count := 0
	err := t.withFS(func(fs transfer.RemoteFS) error {
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
	return t.withFS(func(fs transfer.RemoteFS) error {
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
	return t.withFS(func(fs transfer.RemoteFS) error { return fs.Mkdir(path.Join(dir, name)) })
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
	return t.withFS(func(fs transfer.RemoteFS) error {
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
func (t *tab) transfer(name string, size int64, upload bool, run func(transfer.RemoteFS, *transfer.Progress) error) error {
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

	p := &transfer.Progress{
		Ctx:  ctx,
		Emit: func(x transfer.XferProgress) { t.emit("xfer:progress", x) },
		Info: transfer.XferProgress{Name: name, Total: size, Upload: upload},
	}
	p.Emit(p.Info)
	err := run(t.files.fs, p)
	if err != nil && ctx.Err() != nil {
		err = transfer.ErrCancelled
	}
	end := transfer.XferEnd{OK: err == nil, Message: name}
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
	if err := t.withFS(func(fs transfer.RemoteFS) error {
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
	if sess, _ := t.session().(*sshconn.Session); sess != nil {
		if s, err := sess.Client.NewSession(); err == nil {
			out, err := s.Output(`printf '%s' "$HOME"`)
			s.Close()
			if home := t.codec.DecodeString(string(out)); err == nil && path.IsAbs(home) {
				return home
			}
		}
	}
	var home string
	_ = t.withFS(func(fs transfer.RemoteFS) (err error) {
		home, err = fs.Getwd()
		return err
	})
	return home
}
