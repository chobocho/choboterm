package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// Resumer is a RemoteFS that can continue a transfer part way (SFTP, FTP;
// SCP can't).
type Resumer interface {
	// DownloadFrom sends remotePath from offset on.
	DownloadFrom(remotePath string, offset int64, w io.Writer) error
	// UploadFrom writes r at offset of the existing remotePath.
	UploadFrom(remotePath string, r io.Reader, offset int64) error
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

var ErrCancelled = errors.New("전송이 취소되었습니다")

func SortEntries(list []FileEntry) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDir != list[j].IsDir {
			return list[i].IsDir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
}

// Progress reports transfer progress (throttled) and aborts on cancel.
type Progress struct {
	Ctx  context.Context
	Emit func(XferProgress)
	Info XferProgress
	last time.Time
}

func (p *Progress) add(n int) error {
	p.Info.Done += int64(n)
	if now := time.Now(); now.Sub(p.last) >= 100*time.Millisecond || p.Info.Done == p.Info.Total {
		p.last = now
		p.Emit(p.Info)
	}
	if p.Ctx.Err() != nil {
		return ErrCancelled
	}
	return nil
}

type ProgressWriter struct {
	W io.Writer
	P *Progress
}

func (pw ProgressWriter) Write(b []byte) (int, error) {
	n, err := pw.W.Write(b)
	if perr := pw.P.add(n); err == nil {
		err = perr
	}
	return n, err
}

type ProgressReader struct {
	R io.Reader
	P *Progress
}

func (pr ProgressReader) Read(b []byte) (int, error) {
	n, err := pr.R.Read(b)
	if perr := pr.P.add(n); err == nil {
		err = perr
	}
	return n, err
}

// HumanSize is n in B, KB, MB or GB, for messages.
func HumanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	v, units := float64(n)/unit, []string{"KB", "MB", "GB", "TB"}
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func DownloadsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, "Downloads")
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
		return home
	}
	return "."
}

var errViewLimit = errors.New("view limit reached")

// LimitWriter keeps up to max bytes and then stops the download.
type LimitWriter struct {
	Buf  []byte
	Max  int
	Full bool
}

func (w *LimitWriter) Write(b []byte) (int, error) {
	room := w.Max - len(w.Buf)
	if len(b) > room {
		w.Buf = append(w.Buf, b[:room]...)
		w.Full = true
		return room, errViewLimit
	}
	w.Buf = append(w.Buf, b...)
	return len(b), nil
}
