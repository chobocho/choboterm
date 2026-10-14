// Package dialog is how choboterm asks the user for local files: the
// Picker interface that the window's native dialogs (app) and a remote
// browser's uploads and downloads both implement.
package dialog

import (
	"errors"

	"choboterm/internal/transfer"
)

// Filter is one entry of a file dialog's type list, e.g. {"Lua (*.lua)", "*.lua"}.
type Filter struct {
	Name    string
	Pattern string // "*.lua" or "*.txt;*.log"
}

// Opts describes one file dialog.
type Opts struct {
	Title   string
	Dir     string // the folder the dialog starts in (native dialogs only)
	Name    string // the suggested file name when saving
	Filters []Filter
}

// Picker asks the user for local files: with the window's native dialogs
// (app.wailsPicker) for the window and a browser on this PC, or by uploading and
// downloading through the browser for a web client elsewhere (the browser picker).
//
// Every method returns "" (or nil) when the user cancelled, like the native
// dialogs do. A picker may make temporary files; release and finish tell it
// when the caller is done with them.
type Picker interface {
	// OpenFile asks for an existing file. release frees what the picker made
	// for it (a browser upload) once the caller has read the file.
	OpenFile(o Opts) (path string, release func(), err error)
	// OpenFiles asks for one or more existing files.
	OpenFiles(o Opts) (paths []string, release func(), err error)
	// SaveFile asks where to write a new file. Once the caller wrote it,
	// finish(true) completes it (a browser download) and returns the path
	// or name to show the user; finish(false) drops it after a failure.
	SaveFile(o Opts) (path string, finish func(ok bool) string, err error)
	// SaveDir asks for a folder to save files into; finish as for SaveFile
	// (a browser gets the files in a .zip, or the file itself if only one).
	SaveDir(o Opts) (dir string, finish func(ok bool) string, err error)
	// ReceiveDir is the folder for files that arrive without asking
	// (Zmodem sz): the downloads folder, or one handed to the browser.
	ReceiveDir() (dir string, finish func(ok bool) string)
	// Folder asks for an existing folder of this PC (e.g. the log folder).
	Folder(o Opts) (string, error)
}

// ErrNotInBrowser is returned for things a remote browser can't do, such as
// choosing a folder of the PC running choboterm.
var ErrNotInBrowser = errors.New("원격 브라우저에서는 쓸 수 없는 기능입니다. choboterm을 실행한 PC에서 하세요")

// KeepPath is the finish of a native dialog: the file is already in place.
func KeepPath(p string) func(bool) string { return func(bool) string { return p } }

// NoRelease is the release of a picker that made nothing to free.
func NoRelease() {}

// None is used when there is nobody to ask (no window, no web client).
type None struct{}

func (None) OpenFile(Opts) (string, func(), error) { return "", NoRelease, ErrNotInBrowser }

func (None) OpenFiles(Opts) ([]string, func(), error) { return nil, NoRelease, ErrNotInBrowser }

func (None) SaveFile(Opts) (string, func(bool) string, error) {
	return "", KeepPath(""), ErrNotInBrowser
}

func (None) SaveDir(Opts) (string, func(bool) string, error) {
	return "", KeepPath(""), ErrNotInBrowser
}

func (None) ReceiveDir() (string, func(bool) string) {
	d := transfer.DownloadsDir()
	return d, KeepPath(d)
}

func (None) Folder(Opts) (string, error) { return "", ErrNotInBrowser }
