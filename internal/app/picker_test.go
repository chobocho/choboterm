package app

// Every file dialog goes through App.picker (picker.go). These tests run
// each dialog's method with a fake picker: cancelling gives the same empty
// result as before, a chosen path is used, and finish/release are called so
// a browser picker can complete (download) or clean up (upload).

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"choboterm/internal/codec"
	"choboterm/internal/config"
	"choboterm/internal/dialog"
	"choboterm/internal/store"
	"choboterm/internal/transfer"
	"choboterm/internal/web"
)

// fakePicker answers dialogs from its fields and records what happened.
type fakePicker struct {
	mu       sync.Mutex
	file     string   // openFile / saveFile answer ("" = cancel)
	files    []string // openFiles answer
	dir      string   // saveDir / folder / receiveDir answer
	err      error
	calls    []string // "openFile:title" ...
	finished []bool   // finish(ok) calls
	released int
	opts     []dialog.Opts
}

func (p *fakePicker) rec(op string, o dialog.Opts) {
	p.mu.Lock()
	p.calls = append(p.calls, op+":"+o.Title)
	p.opts = append(p.opts, o)
	p.mu.Unlock()
}

func (p *fakePicker) release() { p.mu.Lock(); p.released++; p.mu.Unlock() }

func (p *fakePicker) finish(ret string) func(bool) string {
	return func(ok bool) string {
		p.mu.Lock()
		p.finished = append(p.finished, ok)
		p.mu.Unlock()
		if !ok {
			return ""
		}
		return ret
	}
}

func (p *fakePicker) OpenFile(o dialog.Opts) (string, func(), error) {
	p.rec("openFile", o)
	return p.file, p.release, p.err
}
func (p *fakePicker) OpenFiles(o dialog.Opts) ([]string, func(), error) {
	p.rec("openFiles", o)
	return p.files, p.release, p.err
}
func (p *fakePicker) SaveFile(o dialog.Opts) (string, func(bool) string, error) {
	p.rec("saveFile", o)
	return p.file, p.finish("saved:" + filepath.Base(p.file)), p.err
}
func (p *fakePicker) SaveDir(o dialog.Opts) (string, func(bool) string, error) {
	p.rec("saveDir", o)
	return p.dir, p.finish("dir:" + p.dir), p.err
}
func (p *fakePicker) ReceiveDir() (string, func(bool) string) {
	p.rec("receiveDir", dialog.Opts{})
	return p.dir, p.finish(p.dir)
}
func (p *fakePicker) Folder(o dialog.Opts) (string, error) {
	p.rec("folder", o)
	return p.dir, p.err
}

// memFS is an in-memory RemoteFS.
type memFS struct {
	mu    sync.Mutex
	files map[string][]byte
	fail  error // Download fails with this
}

func newMemFS() *memFS { return &memFS{files: map[string][]byte{}} }

func (m *memFS) Getwd() (string, error) { return "/", nil }
func (m *memFS) List(dir string) ([]transfer.FileEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var out []transfer.FileEntry
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for p, b := range m.files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		name, sub, isDir := strings.Cut(rest, "/")
		_ = sub
		if seen[name] {
			continue
		}
		seen[name] = true
		e := transfer.FileEntry{Name: name, IsDir: isDir}
		if !isDir {
			e.Size = int64(len(b))
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (m *memFS) Download(p string, w io.Writer) error {
	m.mu.Lock()
	b, ok := m.files[p]
	fail := m.fail
	m.mu.Unlock()
	if fail != nil {
		return fail
	}
	if !ok {
		return os.ErrNotExist
	}
	_, err := w.Write(b)
	return err
}
func (m *memFS) Upload(p string, r io.Reader, size int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.files[p] = b
	m.mu.Unlock()
	return nil
}
func (m *memFS) Remove(p string, isDir bool) error { return nil }
func (m *memFS) Rename(from, to string) error      { return nil }
func (m *memFS) Mkdir(p string) error              { return nil }
func (m *memFS) Close() error                      { return nil }

// pickerApp is an App with a fake picker and a tab 1 on a memFS.
func pickerApp(t *testing.T, p *fakePicker) (*App, *memFS) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	isolateSettings(t)
	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.hooks.picker = p
	fs := newMemFS()
	tb := a.getTab(1)
	tb.files.fs, tb.files.proto = fs, "TEST"
	return a, fs
}

// isolateSettings points the settings (and the web token next to them) at a
// temporary folder.
func isolateSettings(t *testing.T) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "settings.json")
	orig := config.File
	config.File = func() (string, error) { return file, nil }
	t.Cleanup(func() { config.File = orig })
}

func TestFileDownloadThroughPicker(t *testing.T) {
	local := filepath.Join(t.TempDir(), "got.txt")
	p := &fakePicker{file: local}
	a, fs := pickerApp(t, p)
	fs.files["/srv/a.txt"] = []byte("hello")

	got, err := a.FileDownload(1, "/srv/a.txt", 5)
	if err != nil || got != "saved:got.txt" {
		t.Fatalf("download: %q %v", got, err)
	}
	if b, _ := os.ReadFile(local); string(b) != "hello" {
		t.Fatalf("file %q", b)
	}
	if len(p.finished) != 1 || !p.finished[0] {
		t.Fatalf("finish %v", p.finished)
	}
	if o := p.opts[0]; o.Name != "a.txt" || o.Title == "" {
		t.Fatalf("opts %+v", o)
	}

	// A failed download finishes with false (a browser drops the file).
	fs.fail = errors.New("boom")
	if _, err := a.FileDownload(1, "/srv/a.txt", 5); err == nil {
		t.Fatal("no error")
	}
	if len(p.finished) != 2 || p.finished[1] {
		t.Fatalf("finish %v", p.finished)
	}

	// Cancelled: "" and no error, nothing finished.
	p.file = ""
	if got, err := a.FileDownload(1, "/srv/a.txt", 5); got != "" || err != nil {
		t.Fatalf("cancel: %q %v", got, err)
	}
	if len(p.finished) != 2 {
		t.Fatalf("finish after cancel %v", p.finished)
	}
}

func TestFileUploadThroughPicker(t *testing.T) {
	src := t.TempDir()
	writeFiles(t, src, map[string]string{"x.txt": "xx", "y.txt": "yyy"})
	p := &fakePicker{files: []string{filepath.Join(src, "x.txt"), filepath.Join(src, "y.txt")}}
	a, fs := pickerApp(t, p)

	n, err := a.FileUpload(1, "/up")
	if err != nil || n != 2 {
		t.Fatalf("upload: %d %v", n, err)
	}
	if string(fs.files["/up/x.txt"]) != "xx" || string(fs.files["/up/y.txt"]) != "yyy" {
		t.Fatalf("remote %v", fs.files)
	}
	if p.released != 1 {
		t.Fatalf("released %d", p.released)
	}
	p.files = nil
	if n, err := a.FileUpload(1, "/up"); n != 0 || err != nil {
		t.Fatalf("cancel: %d %v", n, err)
	}
	if p.released != 2 {
		t.Fatalf("release after cancel %d", p.released)
	}
}

func TestFileDownloadManyThroughPicker(t *testing.T) {
	dir := t.TempDir()
	p := &fakePicker{dir: dir}
	a, fs := pickerApp(t, p)
	fs.files["/d/a.txt"] = []byte("a")
	fs.files["/d/sub/b.txt"] = []byte("bb")

	res, err := a.FileDownloadMany(1, "/", []transfer.FileEntry{{Name: "d", IsDir: true}})
	if err != nil || res.Count != 2 || res.Dir != "dir:"+dir {
		t.Fatalf("download many: %+v %v", res, err)
	}
	if got := listTree(t, dir); !equalList(got, []string{"d/", "d/a.txt", "d/sub/", "d/sub/b.txt"}) {
		t.Fatalf("tree %v", got)
	}
	if len(p.finished) != 1 || !p.finished[0] {
		t.Fatalf("finish %v", p.finished)
	}
	// The test hook still skips the dialog.
	a.hooks.downloadDir = t.TempDir()
	before := len(p.calls)
	if _, err := a.FileDownloadMany(1, "/d", []transfer.FileEntry{{Name: "a.txt"}}); err != nil || len(p.calls) != before {
		t.Fatalf("hook: %v, calls %v", err, p.calls)
	}
	a.hooks.downloadDir = ""
	p.dir = ""
	if res, err := a.FileDownloadMany(1, "/", []transfer.FileEntry{{Name: "d", IsDir: true}}); res.Dir != "" || err != nil {
		t.Fatalf("cancel: %+v %v", res, err)
	}
}

func TestConsoleOpenSaveThroughPicker(t *testing.T) {
	src := filepath.Join(t.TempDir(), "s.lua")
	os.WriteFile(src, []byte("\xEF\xBB\xBFprint(1)\r\nprint(2)"), 0o644)
	p := &fakePicker{file: src}
	a, _ := pickerApp(t, p)
	f, err := a.ConsoleOpen()
	if err != nil || f.Name != "s.lua" || f.Text != "print(1)\nprint(2)" {
		t.Fatalf("open: %+v %v", f, err)
	}
	if p.released != 1 || p.opts[0].Filters[0].Pattern != "*.lua" {
		t.Fatalf("released %d opts %+v", p.released, p.opts[0])
	}

	out := filepath.Join(t.TempDir(), "c.txt")
	p.file = out
	got, err := a.ConsoleSave("abc")
	if err != nil || got != "saved:c.txt" {
		t.Fatalf("save: %q %v", got, err)
	}
	if b, _ := os.ReadFile(out); string(b) != "abc" {
		t.Fatalf("saved %q", b)
	}

	memo := filepath.Join(t.TempDir(), "memo.txt")
	p.file = memo
	if got, err := a.MemoSave("memo text"); err != nil || got != "saved:memo.txt" {
		t.Fatalf("memo save: %q %v", got, err)
	}
	if b, _ := os.ReadFile(memo); string(b) != "memo text" {
		t.Fatalf("saved memo %q", b)
	}
	p.file = ""
	if f, err := a.ConsoleOpen(); f.Name != "" || err != nil {
		t.Fatalf("cancel open: %+v %v", f, err)
	}
	if got, err := a.ConsoleSave("x"); got != "" || err != nil {
		t.Fatalf("cancel save: %q %v", got, err)
	}
}

func TestRunScriptFileCancel(t *testing.T) {
	p := &fakePicker{}
	a, _ := pickerApp(t, p)
	if name, err := a.RunScriptFile(1); name != "" || err != nil {
		t.Fatalf("cancel: %q %v", name, err)
	}
	if p.released != 1 || !strings.HasPrefix(p.calls[0], "openFile:") {
		t.Fatalf("calls %v released %d", p.calls, p.released)
	}
}

func TestSessionsExportImportThroughPicker(t *testing.T) {
	sess := filepath.Join(t.TempDir(), "sessions.json")
	orig := store.SessionsFile
	store.SessionsFile = func() (string, error) { return sess, nil }
	t.Cleanup(func() { store.SessionsFile = orig })
	p := &fakePicker{}
	a, _ := pickerApp(t, p)
	if _, err := a.SaveSession(store.SavedSession{Name: "web1", Host: "h", Port: 22}, false); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "export.json")
	p.file = out
	got, err := a.ExportSessions()
	if err != nil || got != "saved:export.json" {
		t.Fatalf("export: %q %v", got, err)
	}
	// Import into an empty store.
	os.Remove(sess)
	r, err := a.ImportSessions()
	if err != nil || r.Added != 1 {
		t.Fatalf("import: %+v %v", r, err)
	}
	p.file = ""
	if got, err := a.ExportSessions(); got != "" || err != nil {
		t.Fatalf("cancel export: %q %v", got, err)
	}
	if r, err := a.ImportSessions(); r.Added != 0 || err != nil {
		t.Fatalf("cancel import: %+v %v", r, err)
	}
}

func TestChooseLogDirThroughPicker(t *testing.T) {
	p := &fakePicker{dir: "/some/where"}
	a, _ := pickerApp(t, p)
	if d, err := a.ChooseLogDir(""); d != "/some/where" || err != nil {
		t.Fatalf("choose: %q %v", d, err)
	}
	p.err = errors.New("no")
	if _, err := a.ChooseLogDir(""); err == nil {
		t.Fatal("error lost")
	}
}

func TestTextSaveAsThroughPicker(t *testing.T) {
	out := filepath.Join(t.TempDir(), "k.txt")
	p := &fakePicker{file: out}
	a, _ := pickerApp(t, p)
	r, err := a.TextSaveAs("k.txt", "한글", codec.EUCKR)
	if err != nil || r.Path != "saved:k.txt" || r.Bad != 0 {
		t.Fatalf("save: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(out); !bytes.Equal(b, []byte{0xC7, 0xD1, 0xB1, 0xDB}) {
		t.Fatalf("bytes % X", b)
	}
	p.file = ""
	if r, err := a.TextSaveAs("k.txt", "x", "UTF-8"); r.Path != "" || err != nil {
		t.Fatalf("cancel: %+v %v", r, err)
	}
}

// Zmodem's received files land in the picker's receive folder, and the
// message names where finish says they are.
func TestZmodemReceiveDirFromPicker(t *testing.T) {
	p := &fakePicker{dir: t.TempDir()}
	a, _ := pickerApp(t, p)
	pick := a.picker()
	dir, finish := pick.ReceiveDir()
	if dir != p.dir || finish(true) != p.dir {
		t.Fatal("receiveDir")
	}
}

// Which picker a caller gets.
func TestPickerSelection(t *testing.T) {
	f := startTestWeb(t, web.Options{})
	a := f.a
	if _, ok := a.picker().(dialog.None); !ok {
		t.Fatalf("no window, no caller: %T", a.picker())
	}
	page := func(local bool) *App {
		c, err := f.w.NewClient(local)
		if err != nil {
			t.Fatal(err)
		}
		return &App{appState: a.appState, caller: c}
	}
	remote, local := page(false), page(true)
	browser := reflect.TypeOf(remote.caller.BrowserPicker())
	if reflect.TypeOf(remote.picker()) != browser {
		t.Fatalf("remote browser: %T", remote.picker())
	}
	if reflect.TypeOf(local.picker()) != browser {
		t.Fatalf("local browser without a window: %T", local.picker())
	}
	a.ctx = context.Background() // stands in for the Wails window
	defer func() { a.ctx = nil }()
	if _, ok := a.picker().(wailsPicker); !ok {
		t.Fatalf("window: %T", a.picker())
	}
	if _, ok := local.picker().(wailsPicker); !ok {
		t.Fatalf("local browser with a window: %T", local.picker())
	}
	if reflect.TypeOf(remote.picker()) != browser {
		t.Fatalf("remote browser with a window: %T", remote.picker())
	}
}

// Opening Explorer would happen on the wrong PC for a remote browser.
func TestExplorerOnlyForLocalCallers(t *testing.T) {
	a := NewApp()
	remote := &App{appState: a.appState, caller: &web.Client{}}
	if err := remote.ShowScripts(); !errors.Is(err, dialog.ErrNotInBrowser) {
		t.Fatalf("ShowScripts: %v", err)
	}
	if err := remote.ShowLogs(1); !errors.Is(err, dialog.ErrNotInBrowser) {
		t.Fatalf("ShowLogs: %v", err)
	}
	// Quitting is the window's alone.
	remote.Quit()
	if a.quit.Load() {
		t.Fatal("a web page quit the app")
	}
}

// Native dialogs are used only in picker.go (and the window's host key
// question in app.go): new code must ask App.picker, or web pages would
// open dialogs on the wrong screen.
func TestNativeDialogsOnlyInPicker(t *testing.T) {
	allowed := map[string]bool{"picker.go": true, "app.go": true}
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "runtime" && strings.HasSuffix(sel.Sel.Name, "Dialog") {
				t.Errorf("%s: runtime.%s: use App.picker()", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}
