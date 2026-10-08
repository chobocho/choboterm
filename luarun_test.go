package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLuaHostHelper is not a test: it is the script host process the
// other tests start (like "choboterm.exe --lua-host"), or a broken one.
func TestLuaHostHelper(t *testing.T) {
	switch os.Getenv("CHOBOTERM_LUA_HELPER") {
	case "":
		t.Skip("helper process only")
	case "host":
		os.Exit(runLuaHost(os.Stdin, os.Stdout))
	case "garbage":
		fmt.Println("this is not json")
	case "longline":
		fmt.Print(strings.Repeat("x", 2*luaMaxLine))
	case "noread":
		// Never reads its input and ignores "stop".
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}

type scriptFixture struct {
	app  *App
	tab  *tab
	sess *pipeSession

	mu     sync.Mutex
	screen strings.Builder
	prints []string
	ends   chan string
}

func newScriptFixture(t *testing.T, mode string) *scriptFixture {
	t.Helper()
	origCmd, origLimit := luaHostCommand, luaMemoryLimit
	luaHostCommand = func() (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLuaHostHelper$")
		cmd.Env = append(os.Environ(), "CHOBOTERM_LUA_HELPER="+mode)
		return cmd, nil
	}
	luaMemoryLimit = 128 << 20
	t.Cleanup(func() { luaHostCommand, luaMemoryLimit = origCmd, origLimit })

	f := &scriptFixture{app: NewApp(), sess: newPipeSession(), ends: make(chan string, 4)}
	f.app.hooks.emit = func(name string, data ...interface{}) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch name {
		case "term:data":
			b, _ := base64.StdEncoding.DecodeString(data[1].(string))
			f.screen.Write(b)
		case "script:print":
			f.prints = append(f.prints, data[1].(string))
		case "script:end":
			msg, stopped := data[2].(string), data[3].(bool)
			if stopped != (msg == "스크립트를 멈췄습니다" || strings.HasPrefix(msg, "연결")) {
				msg = fmt.Sprintf("stopped=%v for %q", stopped, msg)
			}
			f.ends <- msg
		}
	}
	f.tab = f.app.getTab(1)
	f.tab.sess = f.sess
	go f.tab.pump(f.sess)
	t.Cleanup(func() {
		f.tab.stopScript("test end")
		_ = f.sess.Close()
	})
	return f
}

func (f *scriptFixture) run(t *testing.T, src string) {
	t.Helper()
	if err := f.app.RunScript(1, "test", src); err != nil {
		t.Fatal(err)
	}
}

// end waits for the script to end and returns its message ("" = success).
func (f *scriptFixture) end(t *testing.T, within time.Duration) string {
	t.Helper()
	select {
	case msg := <-f.ends:
		if f.tab.script.Load() != nil {
			t.Fatal("script still set after it ended")
		}
		return msg
	case <-time.After(within):
		t.Fatalf("script did not end within %v", within)
		return ""
	}
}

func (f *scriptFixture) input() string {
	f.sess.mu.Lock()
	defer f.sess.mu.Unlock()
	return f.sess.in.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestScriptExpectAndSend(t *testing.T) {
	f := newScriptFixture(t, "host")
	f.run(t, `
		local got = expect("login: ")
		send("guest\r")
		expect("Password:")
		send("pw\r")
		print("logged in", got:find("Welcome") ~= nil)
		if expect("never", 0.2) ~= nil then error("expect should time out") end
	`)
	f.sess.out.Write([]byte("Welcome\r\nlogin: "))
	waitFor(t, "login sent", func() bool { return f.input() == "guest\r" })
	// Escape sequences are not part of what expect() sees.
	f.sess.out.Write([]byte("\x1b[1mPass\x1b[0mword: "))
	waitFor(t, "password sent", func() bool { return f.input() == "guest\rpw\r" })
	if msg := f.end(t, 10*time.Second); msg != "" {
		t.Fatalf("ended with %q", msg)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.prints, "|") != "logged in\ttrue" {
		t.Fatalf("prints %q", f.prints)
	}
}

func TestScriptOneAtATime(t *testing.T) {
	f := newScriptFixture(t, "host")
	f.run(t, `sleep(5000)`)
	if err := f.app.RunScript(1, "second", `print(1)`); err == nil {
		t.Fatal("second script started in the same tab")
	}
	f.app.StopScript(1)
	if msg := f.end(t, 5*time.Second); msg != "스크립트를 멈췄습니다" {
		t.Fatalf("ended with %q", msg)
	}
}

func TestScriptSandbox(t *testing.T) {
	f := newScriptFixture(t, "host")
	f.run(t, `
		assert(io == nil and require == nil and dofile == nil and loadfile == nil, "file access")
		assert(os.execute == nil and os.remove == nil and os.exit == nil and os.getenv == nil, "os")
		assert(type(os.time()) == "number" and type(os.clock()) == "number", "clock")
	`)
	if msg := f.end(t, 10*time.Second); msg != "" {
		t.Fatalf("ended with %q", msg)
	}
}

// Each of these must end the script with a message while the app goes on.
func TestScriptFailuresDontHurtTheApp(t *testing.T) {
	cases := []struct {
		name, mode, src string
		stop, kill      bool
		want            string
	}{
		{name: "error", mode: "host", src: "local x = nil\nx.y = 1", want: "test:2:"},
		{name: "syntax", mode: "host", src: "if then", want: "test"},
		{name: "endless loop", mode: "host", src: "while true do end", stop: true, want: "스크립트를 멈췄습니다"},
		{name: "deep recursion", mode: "host", src: "local function f(n) return f(n + 1) + 1 end f(1)", want: "stack overflow"},
		{name: "memory", mode: "host", src: `local t = {} for i = 1, 1e9 do t[i] = string.rep("x", 1048576) .. i end`, want: "메모리 한도"},
		{name: "host crash", mode: "host", src: "sleep(60000)", kill: true, want: "비정상"},
		{name: "garbage", mode: "garbage", want: "잘못된 메시지"},
		{name: "long line", mode: "longline", want: "너무 긴 메시지"},
		{name: "ignores stop", mode: "noread", stop: true, want: "스크립트를 멈췄습니다"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newScriptFixture(t, c.mode)
			f.run(t, c.src)
			if c.stop {
				time.Sleep(300 * time.Millisecond)
				f.app.StopScript(1)
			}
			if c.kill {
				time.Sleep(300 * time.Millisecond)
				_ = f.tab.script.Load().cmd.Process.Kill()
			}
			msg := f.end(t, 20*time.Second)
			if !strings.Contains(msg, c.want) {
				t.Fatalf("ended with %q, want %q", msg, c.want)
			}
			// The tab still works, and can run the next script.
			f.app.Send(1, "still alive")
			if !strings.HasSuffix(f.input(), "still alive") {
				t.Fatalf("input %q", f.input())
			}
		})
	}
}

// A script that doesn't read its output must not slow the terminal down.
func TestScriptNeverBlocksTheTerminal(t *testing.T) {
	f := newScriptFixture(t, "noread")
	f.run(t, "")
	const total = 32 << 20
	chunk := []byte(strings.Repeat("0123456789abcdef", 4096)) // 64KB
	start := time.Now()
	go func() {
		w := bufio.NewWriter(f.sess.out)
		for n := 0; n < total; n += len(chunk) {
			w.Write(chunk)
		}
		w.Flush()
	}()
	waitFor(t, "all output on screen", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.screen.Len() >= total
	})
	if d := time.Since(start); d > 8*time.Second {
		t.Fatalf("32MB took %v", d)
	}
	f.app.StopScript(1)
	if msg := f.end(t, 10*time.Second); msg != "스크립트를 멈췄습니다" {
		t.Fatalf("ended with %q", msg)
	}
}

func TestScriptPrintFlood(t *testing.T) {
	f := newScriptFixture(t, "host")
	f.run(t, `for i = 1, 300000 do print(i) end`)
	if msg := f.end(t, 30*time.Second); msg != "" {
		t.Fatalf("ended with %q", msg)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.prints) == 0 || len(f.prints) > 200 || !strings.HasSuffix(f.prints[len(f.prints)-1], "300000") {
		t.Fatalf("%d print messages, last %q", len(f.prints), f.prints[len(f.prints)-1])
	}
}

func TestScriptStopsWithTheConnection(t *testing.T) {
	f := newScriptFixture(t, "host")
	f.run(t, `expect("never")`)
	time.Sleep(300 * time.Millisecond)
	f.app.Disconnect(1)
	if msg := f.end(t, 10*time.Second); msg != "연결을 끊어 스크립트를 멈췄습니다" {
		t.Fatalf("ended with %q", msg)
	}
}

func TestScriptScreen(t *testing.T) {
	f := newScriptFixture(t, "host")
	f.app.hooks.emit = func(orig func(string, ...interface{})) func(string, ...interface{}) {
		return func(name string, data ...interface{}) {
			if name == "script:screen" {
				go f.app.ScriptScreen(1, data[1].(int), "user@host:~$ ")
			}
			orig(name, data...)
		}
	}(f.app.hooks.emit)
	f.run(t, `assert(screen():find("~%$"), "screen text")`)
	if msg := f.end(t, 10*time.Second); msg != "" {
		t.Fatalf("ended with %q", msg)
	}
}
