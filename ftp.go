package main

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
)

// ftpFS is a plain FTP connection. File names are converted with the
// selected encoding, so EUC-KR servers show Korean names correctly.
type ftpFS struct {
	mu    sync.Mutex // one command at a time on the control connection
	c     *ftp.ServerConn
	names *codec
	stop  chan struct{}
}

func dialFTP(req ConnectRequest) (*ftpFS, error) {
	addr := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(10*time.Second))
	if err != nil {
		return nil, fmt.Errorf("FTP 접속 실패: %w", err)
	}

	user, pass := req.Login, req.Pass
	if user == "" {
		user, pass = "anonymous", "anonymous@"
	}
	if err := c.Login(user, pass); err != nil {
		c.Quit()
		return nil, fmt.Errorf("FTP 로그인 실패: %w", err)
	}

	f := &ftpFS{c: c, names: newCodec(req.Encoding), stop: make(chan struct{})}
	go f.keepAlive()
	return f, nil
}

// keepAlive sends NOOP while idle so the server doesn't drop the session.
func (f *ftpFS) keepAlive() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			if f.mu.TryLock() { // skip while a transfer is running
				_ = f.c.NoOp()
				f.mu.Unlock()
			}
		}
	}
}

func (f *ftpFS) Getwd() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, err := f.c.CurrentDir()
	if err != nil {
		return "/", nil
	}
	return f.names.DecodeString(dir), nil
}

func (f *ftpFS) List(dir string) ([]FileEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := f.c.List(f.names.EncodeString(dir))
	if err != nil {
		return nil, err
	}
	list := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		list = append(list, FileEntry{
			Name:    f.names.DecodeString(e.Name),
			Size:    int64(e.Size),
			IsDir:   e.Type == ftp.EntryTypeFolder || (e.Type == ftp.EntryTypeLink && e.Size == 0),
			ModTime: e.Time.Local().Format("2006-01-02 15:04"),
		})
	}
	return list, nil
}

func (f *ftpFS) Download(remotePath string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.c.Retr(f.names.EncodeString(remotePath))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	return err
}

func (f *ftpFS) Upload(remotePath string, r io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.Stor(f.names.EncodeString(remotePath), r)
}

func (f *ftpFS) Close() error {
	close(f.stop)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.Quit()
}
