package main

// The Lua script host: choboterm.exe started again as "choboterm.exe --lua-host".
// A script runs here, in its own process, so nothing it does (an endless
// loop, running out of memory, a crash in the VM) can take the app down.
// It talks to the app over stdin/stdout, one JSON message per line.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// luaMsg is one message between the app and the script host.
type luaMsg struct {
	T     string `json:"t"`               // message type, see below
	ID    int    `json:"id,omitempty"`    // call / reply pairing
	D     string `json:"d,omitempty"`     // text: output, input to send, printed text, script source
	Name  string `json:"name,omitempty"`  // script name (for error messages)
	Err   string `json:"err,omitempty"`   // error of a reply or of the script
	Limit uint64 `json:"limit,omitempty"` // memory limit in bytes (run)
}

// App → host: run (D = source), out (terminal output), ret (reply to a call), stop.
// Host → app: send / screen (calls, answered with ret), print, done (Err = why it failed).

const (
	luaMaxLine      = 1 << 20   // longest message line either side accepts
	luaMaxSend      = 64 << 10  // most bytes one send() may write
	luaOutputKeep   = 256 << 10 // terminal text kept for expect()
	luaDefaultWait  = 30        // expect() timeout in seconds when none is given
	luaPrintBatch   = 100 * time.Millisecond
	luaCallTimeout  = 10 * time.Second
	luaCallStackMax = 200
)

var errScriptStopped = errors.New("스크립트를 멈췄습니다")

type luaHost struct {
	ctx    context.Context
	cancel context.CancelFunc

	wmu sync.Mutex
	w   *bufio.Writer

	mu     sync.Mutex
	text   strings.Builder // complete lines received and not yet consumed by expect()
	plain  plainText       // turns output into text; holds the unfinished line
	skip   string          // start of the unfinished line that expect() already matched
	notify chan struct{}   // closed and replaced when text grows
	calls  map[int]chan luaMsg
	nextID int

	pmu    sync.Mutex
	prints []string
}

// runLuaHost serves one script and returns the process exit code.
func runLuaHost(in io.Reader, out io.Writer) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &luaHost{
		ctx: ctx, cancel: cancel, w: bufio.NewWriter(out),
		notify: make(chan struct{}), calls: map[int]chan luaMsg{},
	}
	r := bufio.NewReaderSize(in, 64<<10)

	first, err := readLuaMsg(r)
	if err != nil || first.T != "run" {
		return 2
	}
	if first.Limit > 0 {
		// Collect garbage harder near the limit, before the OS ends the process.
		debug.SetMemoryLimit(int64(first.Limit / 10 * 8))
	}
	go h.readLoop(r)
	go h.printLoop()

	err = h.run(first.Name, first.D)
	h.flushPrints()
	done := luaMsg{T: "done"}
	if err != nil {
		done.Err = err.Error()
	}
	h.write(done)
	return 0
}

func readLuaMsg(r *bufio.Reader) (luaMsg, error) {
	var m luaMsg
	line, err := readLimitedLine(r, luaMaxLine)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return m, fmt.Errorf("bad message: %w", err)
	}
	return m, nil
}

// readLimitedLine reads one line, failing if it is longer than max bytes.
func readLimitedLine(r *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > max {
			return nil, errors.New("message too long")
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		return line, nil
	}
}

func (h *luaHost) write(m luaMsg) {
	data, _ := json.Marshal(m)
	h.wmu.Lock()
	defer h.wmu.Unlock()
	_, _ = h.w.Write(append(data, '\n'))
	_ = h.w.Flush()
}

// readLoop takes the app's messages. If the app goes away (stdin closes),
// the script is stopped.
func (h *luaHost) readLoop(r *bufio.Reader) {
	defer h.cancel()
	for {
		m, err := readLuaMsg(r)
		if err != nil {
			return
		}
		switch m.T {
		case "out":
			h.addOutput(m.D)
		case "ret":
			h.mu.Lock()
			ch := h.calls[m.ID]
			delete(h.calls, m.ID)
			h.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case "stop":
			return
		}
	}
}

// addOutput keeps the terminal output as plain text (no escape sequences)
// for expect(); only the last luaOutputKeep bytes are kept.
func (h *luaHost) addOutput(d string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.text.Write(h.plain.feed([]byte(d)))
	if h.skip != "" && h.text.Len() > 0 {
		// The line expect() matched part of is complete now: drop that part.
		s := h.text.String()
		h.text.Reset()
		h.text.WriteString(strings.TrimPrefix(s, h.skip))
		h.skip = ""
	}
	if h.text.Len() > luaOutputKeep {
		s := h.text.String()
		h.text.Reset()
		h.text.WriteString(s[len(s)-luaOutputKeep/2:])
	}
	close(h.notify)
	h.notify = make(chan struct{})
}

// pending returns the unconsumed text, including the unfinished last line
// (a prompt usually has no line break yet), and a channel closed when more arrives.
func (h *luaHost) pending() (string, chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	line := string(h.plain.line) // trailing spaces kept: prompts end with "login: "
	if h.text.Len() == 0 {
		line = strings.TrimPrefix(line, h.skip)
	}
	return h.text.String() + line, h.notify
}

// consume drops the first n bytes of what pending() returned.
func (h *luaHost) consume(text string, n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	done := h.text.Len()
	if n <= done {
		s := h.text.String()
		h.text.Reset()
		h.text.WriteString(s[n:])
		return
	}
	// The match reached into the unfinished line: remember the matched start
	// of that line, which pending() leaves out from now on.
	h.text.Reset()
	h.skip += text[done:n]
}

// call asks the app for something and waits for the answer.
func (h *luaHost) call(m luaMsg) (luaMsg, error) {
	ch := make(chan luaMsg, 1)
	h.mu.Lock()
	h.nextID++
	m.ID = h.nextID
	h.calls[m.ID] = ch
	h.mu.Unlock()
	h.write(m)
	select {
	case r := <-ch:
		if r.Err != "" {
			return r, errors.New(r.Err)
		}
		return r, nil
	case <-h.ctx.Done():
		return luaMsg{}, errScriptStopped
	case <-time.After(luaCallTimeout):
		return luaMsg{}, errors.New("choboterm이 응답하지 않습니다")
	}
}

// print output is sent in batches, so a script printing in a tight loop
// sends a few messages a second instead of one per call.
func (h *luaHost) printLoop() {
	t := time.NewTicker(luaPrintBatch)
	defer t.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
			h.flushPrints()
		}
	}
}

func (h *luaHost) flushPrints() {
	h.pmu.Lock()
	lines := h.prints
	h.prints = nil
	h.pmu.Unlock()
	if len(lines) == 0 {
		return
	}
	const keep = 50 // a flood is cut down to its last lines
	if len(lines) > keep {
		skipped := len(lines) - keep
		lines = append([]string{fmt.Sprintf("... (%d줄 생략)", skipped)}, lines[len(lines)-keep:]...)
	}
	h.write(luaMsg{T: "print", D: strings.Join(lines, "\n")})
}

// run executes the script and returns its error, if any.
func (h *luaHost) run(name, src string) (err error) {
	L := lua.NewState(lua.Options{SkipOpenLibs: true, CallStackSize: luaCallStackMax, IncludeGoStackTrace: false})
	defer L.Close()
	defer func() {
		// gopher-lua reports some internal failures as Go panics.
		if r := recover(); r != nil {
			err = fmt.Errorf("Lua 실행 오류: %v", r)
		}
	}()
	openSafeLibs(L)
	h.register(L)
	L.SetContext(h.ctx)

	fn, err := L.Load(strings.NewReader(src), name)
	if err != nil {
		return cleanLuaError(err)
	}
	L.Push(fn)
	if err := L.PCall(0, 0, nil); err != nil {
		if h.ctx.Err() != nil {
			return errScriptStopped
		}
		return cleanLuaError(err)
	}
	return nil
}

// cleanLuaError keeps "name:line: message" and drops the stack traceback.
func cleanLuaError(err error) error {
	var ae *lua.ApiError
	if errors.As(err, &ae) && ae.Object != nil {
		return errors.New(ae.Object.String())
	}
	msg := err.Error()
	if i := strings.Index(msg, "\nstack traceback:"); i >= 0 {
		msg = msg[:i]
	}
	return errors.New(msg)
}

// openSafeLibs opens the libraries a script may use: no io, no package
// (require), no file loading, and only the clock functions of os.
func openSafeLibs(L *lua.LState) {
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.CoroutineLibName, lua.OpenCoroutine},
		{lua.OsLibName, lua.OpenOs},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	for _, name := range []string{"dofile", "loadfile", "require", "module"} {
		L.SetGlobal(name, lua.LNil)
	}
	if os, ok := L.GetGlobal("os").(*lua.LTable); ok {
		safe := L.NewTable()
		for _, name := range []string{"time", "clock", "date", "difftime"} {
			safe.RawSetString(name, os.RawGetString(name))
		}
		L.SetGlobal("os", safe)
	}
}

// register adds the choboterm functions: send, expect, sleep, screen, print.
func (h *luaHost) register(L *lua.LState) {
	L.SetGlobal("send", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		if len(s) > luaMaxSend {
			L.RaiseError("send: 한 번에 %dKB까지 보낼 수 있습니다", luaMaxSend>>10)
		}
		if _, err := h.call(luaMsg{T: "send", D: s}); err != nil {
			L.RaiseError("send: %s", err.Error())
		}
		return 0
	}))
	L.SetGlobal("expect", L.NewFunction(func(L *lua.LState) int {
		pattern := L.CheckString(1)
		secs := float64(L.OptNumber(2, luaDefaultWait))
		find := L.GetField(L.GetGlobal("string"), "find")
		deadline := time.After(time.Duration(secs * float64(time.Second)))
		for {
			text, more := h.pending()
			L.Push(find)
			L.Push(lua.LString(text))
			L.Push(lua.LString(pattern))
			L.Call(2, 2)
			start, end := L.Get(-2), L.Get(-1)
			L.Pop(2)
			if _, ok := start.(lua.LNumber); ok {
				e := int(end.(lua.LNumber))
				h.consume(text, e)
				L.Push(lua.LString(text[:e])) // everything up to the end of the match
				return 1
			}
			select {
			case <-more:
			case <-deadline:
				L.Push(lua.LNil)
				return 1
			case <-h.ctx.Done():
				L.RaiseError("%s", errScriptStopped.Error())
			}
		}
	}))
	L.SetGlobal("sleep", L.NewFunction(func(L *lua.LState) int {
		ms := L.CheckInt(1)
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-h.ctx.Done():
			L.RaiseError("%s", errScriptStopped.Error())
		}
		return 0
	}))
	L.SetGlobal("screen", L.NewFunction(func(L *lua.LState) int {
		r, err := h.call(luaMsg{T: "screen"})
		if err != nil {
			L.RaiseError("screen: %s", err.Error())
		}
		L.Push(lua.LString(r.D))
		return 1
	}))
	L.SetGlobal("print", L.NewFunction(func(L *lua.LState) int {
		parts := make([]string, L.GetTop())
		for i := range parts {
			parts[i] = L.ToStringMeta(L.Get(i + 1)).String()
		}
		h.pmu.Lock()
		h.prints = append(h.prints, strings.Join(parts, "\t"))
		if len(h.prints) > 1000 {
			h.prints = h.prints[len(h.prints)-1000:]
		}
		h.pmu.Unlock()
		return 0
	}))
}
