package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// opened records what App.open would have opened on this PC, and clears
// the limit on how often for the length of the test.
type opened struct {
	mu  sync.Mutex
	got []string // "kind target"
}

func recordOpens(t *testing.T, a *App) *opened {
	t.Helper()
	o := &opened{}
	a.hooks.open = func(kind, target string) error {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.got = append(o.got, kind+" "+target)
		return nil
	}
	reset := func() {
		opens.Lock()
		opens.at = nil
		opens.Unlock()
	}
	reset()
	t.Cleanup(reset)
	return o
}

func (o *opened) list() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.Join(o.got, "|")
}

func TestCheckURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/chobocho": "https://github.com/chobocho",
		"  http://localhost:8080/x ":  "http://localhost:8080/x",
		"HTTPS://example.com":         "https://example.com",
		"github.com/chobocho":         "https://github.com/chobocho", // no scheme: https
		"192.168.0.1:8080":            "https://192.168.0.1:8080",
	} {
		if got, err := checkURL(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "file:///etc/passwd", "javascript:alert(1)", "ftp://example.com", "https://",
		"a b.com", "https://exa\nmple.com", strings.Repeat("a", openMaxLen) + ".com",
	} {
		if got, err := checkURL(in); err == nil {
			t.Errorf("%q: accepted as %q", in, got)
		}
	}
}

func TestCheckFolder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "run.exe")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		dir:                              dir,
		dir + string(filepath.Separator): dir,
		`"` + dir + `"`:                  dir, // Explorer's "Copy as path"
		filepath.Join(dir, "x", ".."):    dir,
	} {
		if got, err := checkFolder(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if got, err := checkFolder("~"); err != nil || got != filepath.Clean(home) {
			t.Errorf("~: got %q, %v; want %q", got, err, home)
		}
	}
	// a file is never opened: Explorer would run it
	for in, want := range map[string]string{
		file:                          "폴더가 아닙니다",
		filepath.Join(dir, "missing"): "폴더가 없습니다",
		"relative/dir":                "전체 경로",
		"":                            "경로가 없습니다",
	} {
		if _, err := checkFolder(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", in, err, want)
		}
	}
}

func TestOpenHereElsewhereAndHowOften(t *testing.T) {
	a := NewApp()
	rec := recordOpens(t, a)
	dir := t.TempDir()
	// at this PC: shown here
	if o, err := a.open(true, openURL, "example.com"); err != nil || o.InPage || o.Target != "https://example.com" {
		t.Fatalf("local url: %+v %v", o, err)
	}
	if _, err := a.open(true, openFolder, dir); err != nil {
		t.Fatal(err)
	}
	// a browser elsewhere opens the address itself; a folder can't be shown there
	if o, err := a.open(false, openURL, "https://example.com/a"); err != nil || !o.InPage {
		t.Fatalf("remote url: %+v %v", o, err)
	}
	if _, err := a.open(false, openFolder, dir); !errors.Is(err, errFolderElsewhere) {
		t.Fatalf("remote folder: %v", err)
	}
	if got := rec.list(); got != "url https://example.com|folder "+dir {
		t.Fatalf("opened %q", got)
	}
	// refused ones don't count; openBurst within openWindow, then no more
	for i := 3; i < openBurst; i++ {
		if _, err := a.open(true, openURL, "example.com"); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
	}
	if _, err := a.open(true, openURL, "example.com"); err == nil || !strings.Contains(err.Error(), "너무 자주") {
		t.Fatalf("open %d: %v", openBurst+1, err)
	}
	if !openAllowed(time.Now().Add(openWindow)) {
		t.Fatal("still refused after the window")
	}
}

func TestEvalLuaOpens(t *testing.T) {
	useLuaHelper(t, "host")
	a := NewApp()
	rec := recordOpens(t, a)
	dir := t.TempDir()
	r, err := a.EvalLua("open_url('github.com/chobocho')\nopen_folder([[" + dir + "]])\nreturn 'ok'")
	if err != nil || r.Error != "" || r.Value != "ok" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if got := rec.list(); got != "url https://github.com/chobocho|folder "+dir {
		t.Fatalf("opened %q", got)
	}
	if len(r.Open) != 0 {
		t.Fatalf("the window opens them itself, not the page: %q", r.Open)
	}
	for code, want := range map[string]string{
		"open_url('file:///etc/passwd')":                    "open_url: http · https",
		"open_folder([[" + filepath.Join(dir, "no") + "]])": "open_folder: 폴더가 없습니다",
		"open_folder('relative')":                           "open_folder: 전체 경로",
		"open_url()":                                        "bad argument",
	} {
		r, err := a.EvalLua(code)
		if err != nil || !strings.Contains(r.Error, want) {
			t.Errorf("%s: %+v %v, want an error with %q", code, r, err, want)
		}
	}
}

func TestScriptOpens(t *testing.T) {
	f := newScriptFixture(t, "host")
	rec := recordOpens(t, f.app)
	f.run(t, `
		open_url("https://example.com/dash")
		local ok, err = pcall(open_url, "javascript:alert(1)")
		assert(not ok and err:find("http"), "javascript: was opened")
	`)
	if msg := f.end(t, 10*time.Second); msg != "" {
		t.Fatalf("ended with %q", msg)
	}
	if got := rec.list(); got != "url https://example.com/dash" {
		t.Fatalf("opened %q", got)
	}
}
