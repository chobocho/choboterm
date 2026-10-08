package main

// Running Lua scripts for a tab. The script runs in a child process (see
// luahost.go); this side starts it, feeds it the tab's output, carries out
// what it asks (send, screen, print) and makes sure it ends. Nothing here
// waits on the child while holding up the tab: output that the script
// can't take fast enough is dropped for the script, never for the terminal.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	luaMemLimit     = 256 << 20 // memory a script process may use
	luaStopGrace    = 2 * time.Second
	luaSendPerSec   = 256 << 10 // bytes a script may send to the session per second
	luaMsgPerSec    = 2000      // messages per second before the script is considered broken
	luaOutChunk     = 64 << 10  // terminal output is passed on in pieces of at most this
	luaOutQueue     = 256       // output pieces waiting for the script; more are dropped
	luaScreenWait   = 3 * time.Second
	luaScriptMaxSrc = 512 << 10
)

// luaHostCommand starts the script host; tests replace it.
var luaHostCommand = func() (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return exec.Command(exe, "--lua-host"), nil
}

// luaMemoryLimit is luaMemLimit; tests lower it.
var luaMemoryLimit uint64 = luaMemLimit

type scriptRun struct {
	t       *tab
	name    string
	started time.Time
	cmd     *exec.Cmd
	job     *luaJob

	toHost chan luaMsg   // messages for the host, written by writeLoop
	sends  chan luaMsg   // send() calls, carried out one at a time by sendLoop
	ended  chan struct{} // closed when the process is gone

	mu      sync.Mutex
	reason  string // why the app stopped it ("" = it ended by itself)
	result  *luaMsg
	screens map[int]chan string
	killed  bool
}

// ScriptStatus is what the frontend shows about a tab's script.
type ScriptStatus struct {
	Name    string `json:"name"`
	Started int64  `json:"started"` // Unix milliseconds
}

// startScript runs src in tab t. Only one script runs in a tab at a time.
func (t *tab) startScript(name, src string) error {
	if len(src) > luaScriptMaxSrc {
		return fmt.Errorf("스크립트가 너무 큽니다 (%dKB까지)", luaScriptMaxSrc>>10)
	}
	if t.script.Load() != nil {
		return errors.New("이 탭에서 이미 스크립트가 실행 중입니다")
	}
	cmd, err := luaHostCommand()
	if err != nil {
		return err
	}
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("스크립트를 시작하지 못했습니다: %w", err)
	}
	job, err := newLuaJob(cmd.Process.Pid, luaMemoryLimit)
	if err != nil {
		// Without the job there is no memory limit: don't run it.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("스크립트를 시작하지 못했습니다: %w", err)
	}
	s := &scriptRun{
		t: t, name: name, started: time.Now(), cmd: cmd, job: job,
		toHost: make(chan luaMsg, luaOutQueue), sends: make(chan luaMsg, 4),
		ended: make(chan struct{}), screens: map[int]chan string{},
	}
	if !t.script.CompareAndSwap(nil, s) {
		job.close()
		_ = cmd.Wait()
		return errors.New("이 탭에서 이미 스크립트가 실행 중입니다")
	}
	s.toHost <- luaMsg{T: "run", Name: name, D: src, Limit: luaMemoryLimit}
	go s.writeLoop(stdin)
	go s.readLoop(stdout)
	go s.sendLoop()
	go s.wait()
	debugf("tab %d script %q started (pid %d)", t.id, name, cmd.Process.Pid)
	t.emit("script:start", ScriptStatus{Name: name, Started: s.started.UnixMilli()})
	return nil
}

// feed passes terminal output (already decoded to UTF-8) to the script.
// It never waits: when the script is behind, the output is dropped for it.
func (s *scriptRun) feed(data []byte) {
	for len(data) > 0 {
		n := min(len(data), luaOutChunk)
		select {
		case s.toHost <- luaMsg{T: "out", D: string(data[:n])}:
		default:
			return
		}
		data = data[n:]
	}
}

// stop asks the script to stop and kills it if it hasn't after luaStopGrace.
func (s *scriptRun) stop(reason string) {
	s.mu.Lock()
	if s.reason == "" && s.result == nil {
		s.reason = reason
	}
	s.mu.Unlock()
	select {
	case s.toHost <- luaMsg{T: "stop"}:
	default:
	}
	go func() {
		select {
		case <-s.ended:
		case <-time.After(luaStopGrace):
			s.kill()
		}
	}()
}

func (s *scriptRun) kill() {
	s.mu.Lock()
	s.killed = true
	s.mu.Unlock()
	_ = s.cmd.Process.Kill()
}

// fail ends a host that broke the protocol.
func (s *scriptRun) fail(why string) {
	s.mu.Lock()
	if s.reason == "" {
		s.reason = why
	}
	s.mu.Unlock()
	s.kill()
}

func (s *scriptRun) writeLoop(w io.WriteCloser) {
	defer w.Close()
	bw := bufio.NewWriter(w)
	for {
		select {
		case m := <-s.toHost:
			data, _ := json.Marshal(m)
			if _, err := bw.Write(append(data, '\n')); err != nil {
				return
			}
			if len(s.toHost) == 0 {
				if bw.Flush() != nil {
					return
				}
			}
		case <-s.ended:
			return
		}
	}
}

// reply answers a call of the host; it may be dropped if the host is gone.
func (s *scriptRun) reply(id int, d string, err error) {
	m := luaMsg{T: "ret", ID: id, D: d}
	if err != nil {
		m.Err = err.Error()
	}
	select {
	case s.toHost <- m:
	case <-s.ended:
	case <-time.After(time.Second): // the host isn't reading; it will time out the call
	}
}

func (s *scriptRun) readLoop(r io.Reader) {
	defer func() {
		if p := recover(); p != nil {
			debugf("tab %d script reader panic: %v", s.t.id, p)
			s.fail("스크립트 처리 중 오류가 나서 멈췄습니다")
		}
	}()
	br := bufio.NewReaderSize(r, 64<<10)
	window, count := time.Now(), 0
	for {
		line, err := readLimitedLine(br, luaMaxLine)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.fail("스크립트가 너무 긴 메시지를 보내 멈췄습니다")
			}
			return
		}
		if time.Since(window) >= time.Second {
			window, count = time.Now(), 0
		}
		if count++; count > luaMsgPerSec {
			s.fail("스크립트가 메시지를 너무 많이 보내 멈췄습니다")
			return
		}
		var m luaMsg
		if json.Unmarshal(line, &m) != nil {
			s.fail("스크립트가 잘못된 메시지를 보내 멈췄습니다")
			return
		}
		switch m.T {
		case "send":
			select {
			case s.sends <- m:
			default:
				s.reply(m.ID, "", errors.New("이전 send가 아직 끝나지 않았습니다"))
			}
		case "screen":
			go s.screen(m.ID)
		case "print":
			s.t.emit("script:print", m.D)
		case "done":
			s.mu.Lock()
			s.result = &m
			s.mu.Unlock()
		default:
			s.fail("스크립트가 잘못된 메시지를 보내 멈췄습니다")
			return
		}
	}
}

// sendLoop writes the script's input to the session, at most luaSendPerSec.
func (s *scriptRun) sendLoop() {
	window, sent := time.Now(), 0
	for {
		select {
		case m := <-s.sends:
			if time.Since(window) >= time.Second {
				window, sent = time.Now(), 0
			}
			if sent+len(m.D) > luaSendPerSec {
				select {
				case <-time.After(time.Second - time.Since(window)):
				case <-s.ended:
					return
				}
				window, sent = time.Now(), 0
			}
			sent += len(m.D)
			s.reply(m.ID, "", s.t.sendScriptInput(m.D))
		case <-s.ended:
			return
		}
	}
}

// screen asks the frontend for the text on the tab's screen.
func (s *scriptRun) screen(id int) {
	ch := make(chan string, 1)
	s.mu.Lock()
	s.screens[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.screens, id)
		s.mu.Unlock()
	}()
	s.t.emit("script:screen", id)
	select {
	case text := <-ch:
		s.reply(id, text, nil)
	case <-time.After(luaScreenWait):
		s.reply(id, "", errors.New("화면 내용을 읽지 못했습니다"))
	case <-s.ended:
	}
}

// wait reaps the process and reports how the script ended.
func (s *scriptRun) wait() {
	err := s.cmd.Wait()
	memory := s.job.hitMemoryLimit()
	s.job.close()
	close(s.ended)
	s.t.script.CompareAndSwap(s, nil)

	s.mu.Lock()
	msg := ""
	switch {
	case s.result != nil:
		msg = s.result.Err
	case s.reason != "":
		msg = s.reason
	case memory:
		msg = fmt.Sprintf("메모리 한도(%dMB)를 넘어 스크립트를 끝냈습니다", luaMemoryLimit>>20)
	case s.killed:
		msg = "스크립트를 강제로 끝냈습니다"
	default:
		msg = fmt.Sprintf("스크립트가 비정상적으로 끝났습니다 (%v)", err)
	}
	if s.result != nil && s.reason != "" && msg == errScriptStopped.Error() {
		msg = s.reason
	}
	s.mu.Unlock()
	elapsed := time.Since(s.started)
	debugf("tab %d script %q ended after %v: %q", s.t.id, s.name, elapsed, msg)
	s.t.emit("script:end", elapsed.Milliseconds(), msg)
}

// sendScriptInput writes a script's send() to the session like typed input.
func (t *tab) sendScriptInput(data string) error {
	sess := t.session()
	if sess == nil {
		return errors.New("연결되어 있지 않습니다")
	}
	if t.zmodemActive() {
		return errors.New("파일 전송 중에는 보낼 수 없습니다")
	}
	_, err := sess.Write(t.codec.Encode(data))
	return err
}

// stopScript stops the tab's script, if any.
func (t *tab) stopScript(reason string) {
	if s := t.script.Load(); s != nil {
		s.stop(reason)
	}
}

// ---- App bindings ----

// RunScript runs Lua source in a tab; name appears in errors ("자동로그인:3: ...").
func (a *App) RunScript(tabID int, name, src string) error {
	t := a.findTab(tabID)
	if t == nil {
		return errors.New("탭이 없습니다")
	}
	return t.startScript(name, src)
}

// StopScript stops the script running in a tab.
func (a *App) StopScript(tabID int) {
	if t := a.findTab(tabID); t != nil {
		t.stopScript("스크립트를 멈췄습니다")
	}
}

// ScriptRunning returns the tab's running script, or nil.
func (a *App) ScriptRunning(tabID int) *ScriptStatus {
	t := a.findTab(tabID)
	if t == nil {
		return nil
	}
	s := t.script.Load()
	if s == nil {
		return nil
	}
	return &ScriptStatus{Name: s.name, Started: s.started.UnixMilli()}
}

// ScriptScreen is the frontend's answer to a "script:screen" event.
func (a *App) ScriptScreen(tabID, id int, text string) {
	t := a.findTab(tabID)
	if t == nil {
		return
	}
	s := t.script.Load()
	if s == nil {
		return
	}
	s.mu.Lock()
	ch := s.screens[id]
	s.mu.Unlock()
	if ch != nil {
		select {
		case ch <- text:
		default:
		}
	}
}
