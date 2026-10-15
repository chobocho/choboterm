package app

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// useLuaHelper makes the script host the test binary's helper (see
// TestLuaHostHelper) for the length of the test.
func useLuaHelper(t *testing.T, mode string) {
	t.Helper()
	origCmd, origLimit := luaHostCommand, luaMemoryLimit
	luaHostCommand = func() (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLuaHostHelper$")
		cmd.Env = append(os.Environ(), "CHOBOTERM_LUA_HELPER="+mode)
		return cmd, nil
	}
	luaMemoryLimit = 128 << 20
	t.Cleanup(func() { luaHostCommand, luaMemoryLimit = origCmd, origLimit })
}

func TestEvalLuaValuesAndPrints(t *testing.T) {
	useLuaHelper(t, "host")
	a := NewApp()
	for _, c := range []struct{ code, output, value string }{
		{"1 + 2", "", "3"},
		{"print('a') print('b') return 1, 'x'", "a\nb", "1\tx"},
		{"local s = 0 for i = 1, 10 do s = s + i end print(s)", "55", ""},
		{"string.format('%05.1f', math.pi)", "", "003.1"},
		{"=os.time() > 0", "", "true"},
		// the last line's value is shown after the statements before it
		{"print('야옹')\n1 + 2", "야옹", "3"},
	} {
		r, err := a.EvalLua(c.code)
		if err != nil {
			t.Fatalf("%q: %v", c.code, err)
		}
		if r.Error != "" || r.Output != c.output || r.Value != c.value {
			t.Errorf("%q: got output %q value %q error %q, want %q %q", c.code, r.Output, r.Value, r.Error, c.output, c.value)
		}
	}
}

func TestEvalLuaErrors(t *testing.T) {
	useLuaHelper(t, "host")
	a := NewApp()
	for _, c := range []struct{ code, want string }{
		{"error('boom')", "boom"},
		{"if then", "console"},
		{"io.open('x')", "attempt to index"},    // no io library
		{"os.execute('ls')", "attempt to call"}, // only the clock functions of os
		{"send('ls\\r')", "탭"},                  // no tab to send to
		{"return screen()", "탭"},
	} {
		r, err := a.EvalLua(c.code)
		if err != nil {
			t.Fatalf("%q: %v", c.code, err)
		}
		if !strings.Contains(r.Error, c.want) {
			t.Errorf("%q: error %q, want it to mention %q", c.code, r.Error, c.want)
		}
	}
	// what was printed before the error is kept
	r, _ := a.EvalLua("print('before') error('after')")
	if r.Output != "before" || !strings.Contains(r.Error, "after") {
		t.Errorf("print then error: %+v", r)
	}
}

func TestEvalLuaEndlessLoopEnds(t *testing.T) {
	useLuaHelper(t, "host")
	orig := luaEvalTimeout
	luaEvalTimeout = 500 * time.Millisecond
	t.Cleanup(func() { luaEvalTimeout = orig })
	a := NewApp()
	start := time.Now()
	r, err := a.EvalLua("while true do end")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Error, "초") || time.Since(start) > 5*time.Second {
		t.Fatalf("endless loop: %+v after %v", r, time.Since(start))
	}
	// and the next one runs as usual
	if r, _ := a.EvalLua("1"); r.Value != "1" {
		t.Fatalf("after a timeout: %+v", r)
	}
}

func TestEvalLuaBrokenHost(t *testing.T) {
	for _, mode := range []string{"garbage", "longline", "noread"} {
		t.Run(mode, func(t *testing.T) {
			useLuaHelper(t, mode)
			orig := luaEvalTimeout
			luaEvalTimeout = 500 * time.Millisecond
			t.Cleanup(func() { luaEvalTimeout = orig })
			start := time.Now()
			r, err := NewApp().EvalLua("1")
			if err == nil && r.Error == "" {
				t.Fatalf("%s: no error: %+v", mode, r)
			}
			if time.Since(start) > 6*time.Second {
				t.Fatalf("%s: took %v", mode, time.Since(start))
			}
		})
	}
}

func TestEvalLuaOutputIsCapped(t *testing.T) {
	useLuaHelper(t, "host")
	a := NewApp()
	// a flood of lines is cut by the host to its last ones
	r, err := a.EvalLua("for i = 1, 20000 do print(i) end")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Output, "생략") || !strings.HasSuffix(r.Output, "20000") || len(r.Output) > 4096 {
		t.Fatalf("flood: %d bytes, %q...", len(r.Output), r.Output[:min(len(r.Output), 60)])
	}
	// one huge line is cut here
	r, err = a.EvalLua("print(string.rep('가', 100000))")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Output) > luaEvalMaxOutput+10 || !strings.HasSuffix(r.Output, "…") || !utf8.ValidString(r.Output) {
		t.Fatalf("long line not capped: %d bytes, ends %q", len(r.Output), r.Output[max(0, len(r.Output)-12):])
	}
}
