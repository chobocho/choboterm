package app

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"choboterm/internal/dialog"
	"choboterm/internal/osutil"
)

// picker returns how to ask the caller for local files.
func (a *App) picker() dialog.Picker {
	if a.hooks.picker != nil {
		return a.hooks.picker
	}
	// The window's dialogs show on this PC's screen: right for the window
	// itself and for a browser on this PC, but nobody would see them from
	// elsewhere. Without a window (headless web) there are no native dialogs.
	if a.ctx != nil && (a.caller == nil || a.caller.AtThisPC()) {
		return wailsPicker{a}
	}
	if a.caller != nil {
		return a.caller.BrowserPicker()
	}
	return dialog.None{}
}

// local reports whether the caller sits at this PC: the window, or a
// browser verified to run here (see web.Server). Opening a folder in
// Explorer and similar only make sense then.
func (a *App) local() bool {
	return a.caller == nil || a.caller.AtThisPC()
}

// wailsPicker uses the native dialogs of the Wails window.
type wailsPicker struct{ a *App }

func wailsFilters(fs []dialog.Filter) []runtime.FileFilter {
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

func (p wailsPicker) OpenFile(o dialog.Opts) (string, func(), error) {
	p.front()
	path, err := runtime.OpenFileDialog(p.a.ctx, runtime.OpenDialogOptions{
		Title: o.Title, DefaultDirectory: o.Dir, Filters: wailsFilters(o.Filters),
	})
	return path, dialog.NoRelease, err
}

func (p wailsPicker) OpenFiles(o dialog.Opts) ([]string, func(), error) {
	p.front()
	paths, err := runtime.OpenMultipleFilesDialog(p.a.ctx, runtime.OpenDialogOptions{
		Title: o.Title, DefaultDirectory: o.Dir, Filters: wailsFilters(o.Filters),
	})
	return paths, dialog.NoRelease, err
}

func (p wailsPicker) SaveFile(o dialog.Opts) (string, func(bool) string, error) {
	p.front()
	path, err := runtime.SaveFileDialog(p.a.ctx, runtime.SaveDialogOptions{
		Title: o.Title, DefaultDirectory: o.Dir, DefaultFilename: o.Name, Filters: wailsFilters(o.Filters),
	})
	return path, dialog.KeepPath(path), err
}

func (p wailsPicker) SaveDir(o dialog.Opts) (string, func(bool) string, error) {
	dir, err := p.Folder(o)
	return dir, dialog.KeepPath(dir), err
}

func (p wailsPicker) ReceiveDir() (string, func(bool) string) {
	dir := p.a.downloadDir()
	return dir, dialog.KeepPath(dir)
}

func (p wailsPicker) Folder(o dialog.Opts) (string, error) {
	p.front()
	return runtime.OpenDirectoryDialog(p.a.ctx, runtime.OpenDialogOptions{Title: o.Title, DefaultDirectory: o.Dir})
}
