package main

import (
	"encoding/base64"
	"errors"
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
	}, nil
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
