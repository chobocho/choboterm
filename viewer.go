package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// viewLimit is how much of a remote file the text viewer loads.
var viewLimit = 10 << 20

var errViewLimit = errors.New("view limit reached")

// limitWriter keeps up to max bytes and then stops the download.
type limitWriter struct {
	buf  []byte
	max  int
	full bool
}

func (w *limitWriter) Write(b []byte) (int, error) {
	room := w.max - len(w.buf)
	if len(b) > room {
		w.buf = append(w.buf, b[:room]...)
		w.full = true
		return room, errViewLimit
	}
	w.buf = append(w.buf, b...)
	return len(b), nil
}

// ViewResult is the raw content of a remote file for the text viewer.
// The viewer decodes it itself, so the encoding can be switched without
// downloading again.
type ViewResult struct {
	Data      string `json:"data"` // base64
	Truncated bool   `json:"truncated"`
	// The file as listed right before reading; the editor checks it again
	// before saving, to notice changes made by someone else.
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// statRemote finds p in its folder's listing (RemoteFS has no Stat).
func statRemote(fs RemoteFS, p string) (FileEntry, error) {
	list, err := fs.List(path.Dir(p))
	if err != nil {
		return FileEntry{}, err
	}
	name := path.Base(p)
	for _, e := range list {
		if e.Name == name {
			return e, nil
		}
	}
	return FileEntry{}, fmt.Errorf("파일이 없습니다: %s", p)
}

// FileView downloads up to viewLimit bytes of remotePath for the text viewer.
func (a *App) FileView(tabID int, remotePath string, size int64) (ViewResult, error) {
	t := a.findTab(tabID)
	if t == nil {
		return ViewResult{}, errors.New("연결되어 있지 않습니다")
	}
	total := size
	if total > int64(viewLimit) {
		total = int64(viewLimit)
	}
	var st FileEntry
	if err := t.withFS(func(fs RemoteFS) (err error) {
		st, err = statRemote(fs, remotePath)
		return err
	}); err != nil {
		return ViewResult{}, err
	}
	if st.IsDir {
		return ViewResult{}, errors.New("폴더는 열 수 없습니다")
	}
	w := &limitWriter{max: viewLimit}
	err := t.transfer(path.Base(remotePath), total, false, func(fs RemoteFS, p *progress) error {
		err := fs.Download(remotePath, progressWriter{w, p})
		if w.full {
			return nil // the rest of the file isn't needed
		}
		return err
	})
	if err != nil {
		return ViewResult{}, err
	}
	return ViewResult{
		Data:      base64.StdEncoding.EncodeToString(w.buf),
		Truncated: w.full,
		Size:      st.Size,
		ModTime:   st.ModTime,
	}, nil
}

// TextSave is the editor's request to write a remote file.
type TextSave struct {
	Path     string `json:"path"`
	Text     string `json:"text"`
	Encoding string `json:"encoding"`
	// What the file looked like when it was opened or last saved.
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	// Overwrite even if the file changed on the server meanwhile.
	IgnoreConflict bool `json:"ignoreConflict"`
	// Save even if some characters don't exist in Encoding (they become '?').
	AllowLossy bool `json:"allowLossy"`
}

// TextSaveResult says whether the file was written, and if not, why.
type TextSaveResult struct {
	Saved    bool `json:"saved"`
	Conflict bool `json:"conflict"` // the file changed on the server
	Bad      int  `json:"bad"`      // characters missing from the encoding
	// The file after saving, for the next conflict check.
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	// The bytes written (base64), so the viewer can decode them again.
	Data string `json:"data"`
}

// FileSaveText writes the editor's text to a remote file in the requested encoding.
// It doesn't write if characters would be lost or the file changed on the
// server, unless the request allows it.
func (a *App) FileSaveText(tabID int, req TextSave) (TextSaveResult, error) {
	t := a.findTab(tabID)
	if t == nil {
		return TextSaveResult{}, errors.New("연결되어 있지 않습니다")
	}
	out, bad := encodeText(req.Text, req.Encoding)
	if bad > 0 && !req.AllowLossy {
		return TextSaveResult{Bad: bad}, nil
	}
	if !req.IgnoreConflict {
		var st FileEntry
		err := t.withFS(func(fs RemoteFS) (err error) {
			st, err = statRemote(fs, req.Path)
			return err
		})
		if err != nil {
			return TextSaveResult{}, err
		}
		if st.Size != req.Size || st.ModTime != req.ModTime {
			return TextSaveResult{Conflict: true, Bad: bad}, nil
		}
	}
	size := int64(len(out))
	err := t.transfer(path.Base(req.Path), size, true, func(fs RemoteFS, p *progress) error {
		return fs.Upload(req.Path, progressReader{bytes.NewReader(out), p}, size)
	})
	if err != nil {
		return TextSaveResult{}, err
	}
	res := TextSaveResult{Saved: true, Bad: bad, Size: size, Data: base64.StdEncoding.EncodeToString(out)}
	_ = t.withFS(func(fs RemoteFS) error {
		if st, err := statRemote(fs, req.Path); err == nil {
			res.Size, res.ModTime = st.Size, st.ModTime
		}
		return nil
	})
	return res, nil
}

// encodeText converts UTF-8 text to enc. Characters missing from CP949
// become '?'; bad counts them.
func encodeText(text, enc string) (out []byte, bad int) {
	if enc != EncodingEUCKR {
		return []byte(text), 0
	}
	c := newCodec(EncodingEUCKR)
	out = c.Encode(text)
	// Encode turns each missing character into '?', so compare the counts.
	bad = strings.Count(string(out), "?") - strings.Count(text, "?")
	return out, bad
}

// SaveResult tells where the converted text went.
type SaveResult struct {
	Path string `json:"path"`
	Bad  int    `json:"bad"` // characters replaced with '?'
}

// TextSaveAs asks for a local file and writes text to it in encoding enc.
// Path is "" if the user cancelled the dialog.
func (a *App) TextSaveAs(name, text, enc string) (SaveResult, error) {
	local, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "다른 인코딩으로 저장 (" + enc + ")",
		DefaultDirectory: downloadsDir(),
		DefaultFilename:  name,
	})
	if err != nil || local == "" {
		return SaveResult{}, err
	}
	out, bad := encodeText(text, enc)
	if err := os.WriteFile(local, out, 0o644); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Path: local, Bad: bad}, nil
}
