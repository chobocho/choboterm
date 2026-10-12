package main

// Tests that pin down what the page and the files on disk see of the Go
// side, so that moving code between files and packages cannot change it:
// the methods a page may call, the JSON shape of every value exchanged or
// saved, the default settings, and the names of the events.
//
// The expected values are in testdata/wire.golden. After a change that is
// meant, run "UPDATE_GOLDEN=1 go test -run TestWire" and review the diff.

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// moduleRoot is the folder with go.mod, wherever this test's package is.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("go.mod not found")
		}
		dir = up
	}
}

// goSources calls fn for every Go file of the program (no tests).
func goSources(t *testing.T, root string, fn func(path string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "frontend", "node_modules", "build", "release", "docs", ".git", ".idea", "tools":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			fn(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// wireType names a type the way the page sees it: without its package, so a
// type keeps its name when it moves to another package.
func wireType(t reflect.Type) string {
	if t.PkgPath() != "" && t.Name() != "" {
		if strings.Contains(strings.SplitN(t.PkgPath(), "/", 2)[0], ".") || !strings.HasPrefix(t.String(), "main.") && isStdlib(t.PkgPath()) {
			return t.String()
		}
		return t.Name()
	}
	switch t.Kind() {
	case reflect.Ptr:
		return "*" + wireType(t.Elem())
	case reflect.Slice:
		return "[]" + wireType(t.Elem())
	case reflect.Array:
		return fmt.Sprintf("[%d]%s", t.Len(), wireType(t.Elem()))
	case reflect.Map:
		return "map[" + wireType(t.Key()) + "]" + wireType(t.Elem())
	}
	return t.String()
}

func isStdlib(pkg string) bool {
	return !strings.HasPrefix(pkg, "choboterm") && pkg != "main"
}

// boundMethods lists the methods of *App, which are what Wails binds and
// what a browser may call (webMethods), with their arguments and results.
func boundMethods() []string {
	var out []string
	typ := reflect.TypeOf(&App{})
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		var in, res []string
		for j := 1; j < m.Type.NumIn(); j++ {
			in = append(in, wireType(m.Type.In(j)))
		}
		for j := 0; j < m.Type.NumOut(); j++ {
			res = append(res, wireType(m.Type.Out(j)))
		}
		line := m.Name + "(" + strings.Join(in, ", ") + ")"
		if len(res) > 0 {
			line += " (" + strings.Join(res, ", ") + ")"
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

var qualifier = regexp.MustCompile(`\b[A-Za-z_]\w*\.`)
var identifier = regexp.MustCompile(`[A-Za-z_]\w*`)

// unexported writes a name with a small first letter, so that exporting a
// type (to use it from another package) doesn't count as a change.
func unexported(name string) string {
	r := []rune(name)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// jsonStructs lists every struct in the program that has json tags: the
// values exchanged with the page (arguments, results, events) and the ones
// saved in files. A field shows its JSON name and options, or for a field
// without a tag its Go name as written (exporting one would add it to the JSON).
func jsonStructs(t *testing.T, root string) []string {
	var out []string
	fset := token.NewFileSet()
	goSources(t, root, func(path string) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			var fields []string
			tagged := false
			for _, fl := range st.Fields.List {
				typ := string(src[fset.Position(fl.Type.Pos()).Offset:fset.Position(fl.Type.End()).Offset])
				typ = identifier.ReplaceAllStringFunc(qualifier.ReplaceAllString(typ, ""), unexported)
				typ = strings.Join(strings.Fields(typ), " ")
				tag := ""
				if fl.Tag != nil {
					tag = reflect.StructTag(strings.Trim(fl.Tag.Value, "`")).Get("json")
				}
				if tag != "" {
					tagged = true
					fields = append(fields, fmt.Sprintf("    %q %s", tag, typ))
					continue
				}
				if len(fl.Names) == 0 {
					fields = append(fields, "    (embedded) "+typ)
				}
				for _, name := range fl.Names {
					fields = append(fields, fmt.Sprintf("    %s %s", name.Name, typ))
				}
			}
			if tagged {
				out = append(out, unexported(ts.Name.Name)+" {\n"+strings.Join(fields, "\n")+"\n}")
			}
			return true
		})
	})
	sort.Strings(out)
	return out
}

func TestWireSurface(t *testing.T) {
	root := moduleRoot(t)
	defaults, err := json.MarshalIndent(defaultSettings(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := "# Methods a page may call (Wails bindings, and browsers through the web server)\n\n" +
		strings.Join(boundMethods(), "\n") +
		"\n\n# Structs with JSON fields (exchanged with the page, or saved in files)\n\n" +
		strings.Join(jsonStructs(t, root), "\n\n") +
		"\n\n# Default settings\n\n" + string(defaults) + "\n"

	golden := filepath.Join("testdata", "wire.golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if w := strings.ReplaceAll(string(want), "\r\n", "\n"); got != w {
		gl, wl := strings.Split(got, "\n"), strings.Split(w, "\n")
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var g, x string
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				x = wl[i]
			}
			if g != x {
				t.Fatalf("what the page or the saved files see has changed (%s line %d)\n got: %s\nwant: %s", golden, i+1, g, x)
			}
		}
	}
}

var (
	emitCall  = regexp.MustCompile(`(?i)emit\w*\(|\.event\(`)
	eventName = regexp.MustCompile(`["']([a-z]+:[A-Za-z]+)["']`)
	listened  = regexp.MustCompile(`EventsOn\('([a-z]+:[A-Za-z]+)'`)
)

// Every event the Go side emits has a listener in the page (or is handled by
// the browser side of the web server, web_shim.js), and every event the page
// listens for is emitted: a renamed or dropped event would otherwise fail
// silently.
func TestEventsMatchThePage(t *testing.T) {
	root := moduleRoot(t)
	emitted := map[string]bool{}
	var shim string
	goSources(t, root, func(path string) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			if emitCall.MatchString(line) {
				for _, m := range eventName.FindAllStringSubmatch(line, -1) {
					emitted[m[1]] = true
				}
			}
		}
		if s := filepath.Join(filepath.Dir(path), "web_shim.js"); shim == "" {
			if b, err := os.ReadFile(s); err == nil {
				shim = string(b)
			}
		}
	})
	if shim == "" {
		t.Fatal("web_shim.js not found next to the Go sources")
	}
	page := map[string]bool{}
	files, err := filepath.Glob(filepath.Join(root, "frontend", "src", "*.ts"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no page sources: %v", err)
	}
	for _, p := range files {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range listened.FindAllStringSubmatch(string(src), -1) {
			page[m[1]] = true
		}
	}
	if len(emitted) < 15 || len(page) < 15 {
		t.Fatalf("found only %d emitted and %d listened events: the search is broken", len(emitted), len(page))
	}
	for name := range emitted {
		if !page[name] && !strings.Contains(shim, "'"+name+"'") {
			t.Errorf("%s is emitted, but the page has no listener for it", name)
		}
	}
	for name := range page {
		if !emitted[name] {
			t.Errorf("the page listens for %s, which is never emitted", name)
		}
	}
}
