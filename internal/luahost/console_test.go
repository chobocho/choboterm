package luahost

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// run compiles code as console input and runs it, returning the values it
// gave (tab-separated) and the global x afterwards.
func run(t *testing.T, code string) (vals, x string, err error) {
	t.Helper()
	L := lua.NewState()
	defer L.Close()
	fn, err := compileConsole(L, code)
	if err != nil {
		return "", "", err
	}
	L.Push(fn)
	if err := L.PCall(0, lua.MultRet, nil); err != nil {
		return "", "", err
	}
	var parts []string
	for i := 1; i <= L.GetTop(); i++ {
		parts = append(parts, L.Get(i).String())
	}
	return strings.Join(parts, "\t"), L.GetGlobal("x").String(), nil
}

func TestConsoleShowsTheLastExpression(t *testing.T) {
	for _, c := range []struct{ code, vals, x string }{
		{"1 + 2", "3", "nil"},
		{"=1, 2", "1\t2", "nil"},
		{"x = 5\nx", "5", "5"},
		{"local t = {1, 2, 3}\n#t", "3", "nil"},
		{"x = 1\n\n  x * 10  \n", "10", "1"},
		// a call is a statement too, but its values are shown
		{"f = function() return 7 end\nf()", "7", "nil"},
		// an expression over several lines
		{"local a = 2\nmath.max(a,\n  5)", "5", "nil"},
		// statements only
		{"x = 1\nx = 2", "", "2"},
		{"for i = 1, 3 do\n  x = i\nend", "", "3"},
		{"x = 1 -- a comment\n-- another", "", "1"},
		// valid code keeps its meaning: these lines continue the one before
		{"g = function(t) return t[1] * 2 end\nx = g\n{4}", "", "8"},
		{"x = 10\n- 3", "", "7"},
		{"x = tostring\n'abc'", "", "abc"},
		// a head ending inside a long string must not swallow "return"
		{"x = [[\nhi\n1]]", "", "hi\n1"},
		{"x = [[\na --]]", "", "a --"},
	} {
		vals, x, err := run(t, c.code)
		if err != nil {
			t.Errorf("%q: %v", c.code, err)
			continue
		}
		if vals != c.vals || x != c.x {
			t.Errorf("%q: values %q x %q, want %q %q", c.code, vals, x, c.vals, c.x)
		}
	}
}

func TestConsoleErrorsKeepTheirLines(t *testing.T) {
	for _, c := range []struct{ code, want string }{
		{"x = 1\nerror('boom')", "console:2:"},
		{"print(1)\n1 +", "line:2"},
		{"x = 1\ny y\n3", "line:2"},
	} {
		_, _, err := run(t, c.code)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: error %v, want it to mention %q", c.code, err, c.want)
		}
	}
}
