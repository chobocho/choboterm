package main

import (
	"errors"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"choboterm/internal/osutil"
)

// fileFilter is one entry of a file dialog's type list, e.g. {"Lua (*.lua)", "*.lua"}.
type fileFilter struct {
	Name    string
	Pattern string // "*.lua" or "*.txt;*.log"
}

// pickOpts describes one file dialog.
type pickOpts struct {
	Title   string
	Dir     string // the folder the dialog starts in (native dialogs only)
	Name    string // the suggested file name when saving
	Filters []fileFilter
}

// picker asks the user for local files: with the window's native dialogs
// (wailsPicker) for the window and a browser on this PC, or by uploading and
// downloading through the browser for a web client elsewhere (browserPicker).
//
// Every method returns "" (or nil) when the user cancelled, like the native
// dialogs do. A picker may make temporary files; release and finish tell it
// when the caller is done with them.
type picker interface {
	// openFile asks for an existing file. release frees what the picker made
	// for it (a browser upload) once the caller has read the file.
	openFile(o pickOpts) (path string, release func(), err error)
	// openFiles asks for one or more existing files.
	openFiles(o pickOpts) (paths []string, release func(), err error)
	// saveFile asks where to write a new file. Once the caller wrote it,
	// finish(true) completes it (a browser download) and returns the path
	// or name to show the user; finish(false) drops it after a failure.
	saveFile(o pickOpts) (path string, finish func(ok bool) string, err error)
	// saveDir asks for a folder to save files into; finish as for saveFile
	// (a browser gets the files in a .zip, or the file itself if only one).
	saveDir(o pickOpts) (dir string, finish func(ok bool) string, err error)
	// receiveDir is the folder for files that arrive without asking
	// (Zmodem sz): the downloads folder, or one handed to the browser.
	receiveDir() (dir string, finish func(ok bool) string)
	// folder asks for an existing folder of this PC (e.g. the log folder).
	folder(o pickOpts) (string, error)
}

// errNotInBrowser is returned for things a remote browser can't do, such as
// choosing a folder of the PC running choboterm.
var errNotInBrowser = errors.New("원격 브라우저에서는 쓸 수 없는 기능입니다. choboterm을 실행한 PC에서 하세요")

// picker returns how to ask the caller for local files.
func (a *App) picker() picker {
	if a.hooks.picker != nil {
		return a.hooks.picker
	}
	// The window's dialogs show on this PC's screen: right for the window
	// itself and for a browser on this PC, but nobody would see them from
	// elsewhere. Without a window (headless web) there are no native dialogs.
	if a.ctx != nil && (a.caller == nil || a.caller.local) {
		return wailsPicker{a}
	}
	if a.caller != nil {
		return a.caller.browserPicker()
	}
	return noPicker{}
}

// local reports whether the caller sits at this PC: the window, or a
// browser verified to run here (see webServer.isLocal). Opening a folder in
// Explorer and similar only make sense then.
func (a *App) local() bool {
	return a.caller == nil || a.caller.local
}

// keepPath is the finish of a native dialog: the file is already in place.
func keepPath(p string) func(bool) string { return func(bool) string { return p } }

func noRelease() {}

// wailsPicker uses the native dialogs of the Wails window.
type wailsPicker struct{ a *App }

func wailsFilters(fs []fileFilter) []runtime.FileFilter {
	var out []runtime.FileFilter
	for _, f := range fs {
		out = append(out, runtime.FileFilter{DisplayName: f.Name, Pattern: f.Pattern})
	}
	return out
}

// front brings a dialog opened for a browser on this PC in front of the
// browser, which has the keyboard focus (see raiseDialogSoon).
func (p wailsPicker) front() {
	if p.a.caller != nil {
		osutil.RaiseDialogSoon()
	}
}

func (p wailsPicker) openFile(o pickOpts) (string, func(), error) {
	p.front()
	path, err := runtime.OpenFileDialog(p.a.ctx, runtime.OpenDialogOptions{
		Title: o.Title, DefaultDirectory: o.Dir, Filters: wailsFilters(o.Filters),
	})
	return path, noRelease, err
}

func (p wailsPicker) openFiles(o pickOpts) ([]string, func(), error) {
	p.front()
	paths, err := runtime.OpenMultipleFilesDialog(p.a.ctx, runtime.OpenDialogOptions{
		Title: o.Title, DefaultDirectory: o.Dir, Filters: wailsFilters(o.Filters),
	})
	return paths, noRelease, err
}

func (p wailsPicker) saveFile(o pickOpts) (string, func(bool) string, error) {
	p.front()
	path, err := runtime.SaveFileDialog(p.a.ctx, runtime.SaveDialogOptions{
		Title: o.Title, DefaultDirectory: o.Dir, DefaultFilename: o.Name, Filters: wailsFilters(o.Filters),
	})
	return path, keepPath(path), err
}

func (p wailsPicker) saveDir(o pickOpts) (string, func(bool) string, error) {
	dir, err := p.folder(o)
	return dir, keepPath(dir), err
}

func (p wailsPicker) receiveDir() (string, func(bool) string) {
	dir := p.a.downloadDir()
	return dir, keepPath(dir)
}

func (p wailsPicker) folder(o pickOpts) (string, error) {
	p.front()
	return runtime.OpenDirectoryDialog(p.a.ctx, runtime.OpenDialogOptions{Title: o.Title, DefaultDirectory: o.Dir})
}

// noPicker is used when there is nobody to ask (no window, no web client).
type noPicker struct{}

func (noPicker) openFile(pickOpts) (string, func(), error)    { return "", noRelease, errNotInBrowser }
func (noPicker) openFiles(pickOpts) ([]string, func(), error) { return nil, noRelease, errNotInBrowser }
func (noPicker) saveFile(pickOpts) (string, func(bool) string, error) {
	return "", keepPath(""), errNotInBrowser
}
func (noPicker) saveDir(pickOpts) (string, func(bool) string, error) {
	return "", keepPath(""), errNotInBrowser
}
func (noPicker) receiveDir() (string, func(bool) string) {
	d := downloadsDir()
	return d, keepPath(d)
}
func (noPicker) folder(pickOpts) (string, error) { return "", errNotInBrowser }
