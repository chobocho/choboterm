package main

import (
	"context"
	"errors"
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
	Upload(remotePath string, r io.Reader) error
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

// ---- App bindings ----

// FileOpen prepares remote file access for the current connection and
// returns the initial directory. SSH connections use SFTP.
func (a *App) FileOpen() (string, error) {
	a.files.mu.Lock()
	defer a.files.mu.Unlock()

	if a.files.fs == nil {
		a.mu.Lock()
		sess, _ := a.sess.(*sshSession)
		a.mu.Unlock()
		if sess == nil {
			return "", errors.New("파일 전송은 SSH(SFTP) 또는 FTP 접속에서 사용할 수 있습니다.\nTelnet에서는 sz / rz (Zmodem)를 사용하세요.")
		}
		fs, err := newSFTP(sess.client)
		if err != nil {
			return "", err
		}
		a.files.fs = fs
	}
	return a.files.fs.Getwd()
}

// FileList lists a remote directory, folders first.
func (a *App) FileList(dir string) ([]FileEntry, error) {
	a.files.mu.Lock()
	defer a.files.mu.Unlock()
	if a.files.fs == nil {
		return nil, errors.New("연결되어 있지 않습니다")
	}
	list, err := a.files.fs.List(dir)
	if err != nil {
		return nil, err
	}
	sortEntries(list)
	return list, nil
}

// FileDownload asks where to save remotePath and downloads it.
// Returns the local path, or "" if the user cancelled the dialog.
func (a *App) FileDownload(remotePath string, size int64) (string, error) {
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
	err = a.transfer(path.Base(remotePath), size, false, func(fs RemoteFS, p *progress) error {
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
func (a *App) FileUpload(remoteDir string) (int, error) {
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "업로드할 파일 선택",
	})
	if err != nil || len(files) == 0 {
		return 0, err
	}

	count := 0
	for _, local := range files {
		name := filepath.Base(local)
		f, err := os.Open(local)
		if err != nil {
			return count, err
		}
		var size int64
		if st, err := f.Stat(); err == nil {
			size = st.Size()
		}
		err = a.transfer(name, size, true, func(fs RemoteFS, p *progress) error {
			return fs.Upload(path.Join(remoteDir, name), progressReader{f, p})
		})
		f.Close()
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// transfer runs one cancellable file transfer with progress events.
func (a *App) transfer(name string, size int64, upload bool, run func(RemoteFS, *progress) error) error {
	a.files.mu.Lock()
	defer a.files.mu.Unlock()
	if a.files.fs == nil {
		return errors.New("연결되어 있지 않습니다")
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.files.setCancel(cancel)
	defer func() {
		cancel()
		a.files.setCancel(nil)
	}()

	p := &progress{
		ctx:  ctx,
		emit: func(x XferProgress) { a.emit("xfer:progress", x) },
		info: XferProgress{Name: name, Total: size, Upload: upload},
	}
	p.emit(p.info)
	err := run(a.files.fs, p)
	if err != nil && ctx.Err() != nil {
		err = errCancelled
	}
	end := XferEnd{OK: err == nil, Message: name}
	if err != nil {
		end.Message = err.Error()
	}
	a.emit("xfer:end", end)
	return err
}

// FileCancel aborts the running transfer (SFTP/FTP or Zmodem).
func (a *App) FileCancel() {
	a.cancelZmodem()
	a.files.cmu.Lock()
	c := a.files.cancel
	a.files.cmu.Unlock()
	if c != nil {
		c()
	}
}

// FileClose releases the remote file system (SFTP channel or FTP connection).
func (a *App) FileClose() {
	a.FileCancel()
	a.files.mu.Lock()
	fs := a.files.fs
	a.files.fs = nil
	a.files.mu.Unlock()
	if fs != nil {
		_ = fs.Close()
	}
}
