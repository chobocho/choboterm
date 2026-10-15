package app

// Lua run outside any tab: the desktop cat's "run this" box. Each call
// starts its own script host (the same child process, sandbox and memory
// limit as a tab's console), evaluates the code once and ends it, so a
// snippet can neither hang the app nor leave anything behind.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"choboterm/internal/dbg"
	"choboterm/internal/luahost"
)

// LuaResult is what a snippet run with EvalLua gave.
type LuaResult struct {
	Output string `json:"output"` // what it printed, line by line
	Value  string `json:"value"`  // the values it returned, tab-separated ("" if none)
	Error  string `json:"error"`  // why it failed ("" if it didn't)
	Ms     int64  `json:"ms"`     // how long it ran
	// Addresses open_url gave for the page to open itself (a browser on
	// another PC); on this PC they are already open.
	Open []string `json:"open,omitempty"`
}

const luaEvalMaxOutput = 64 << 10 // printed text kept; the rest is cut

// luaEvalTimeout is how long a snippet may run before it is stopped; tests lower it.
var luaEvalTimeout = 10 * time.Second

// EvalLua runs code (a statement block, or an expression whose value is
// shown, as in the Lua console) in a fresh script host with no tab: send()
// and screen() fail, print() is collected.
func (a *App) EvalLua(code string) (LuaResult, error) {
	if len(code) > luaScriptMaxSrc {
		return LuaResult{}, fmt.Errorf("코드가 너무 큽니다 (%dKB까지)", luaScriptMaxSrc>>10)
	}
	cmd, err := luaHostCommand()
	if err != nil {
		return LuaResult{}, err
	}
	luahost.HideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return LuaResult{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return LuaResult{}, err
	}
	cmd.Stderr = io.Discard
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return LuaResult{}, fmt.Errorf("Lua를 시작하지 못했습니다: %w", err)
	}
	job, err := luahost.NewJob(cmd.Process.Pid, luaMemoryLimit)
	if err != nil {
		// Without the job there is no memory limit: don't run it.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return LuaResult{}, fmt.Errorf("Lua를 시작하지 못했습니다: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	defer func() {
		_ = stdin.Close() // the host stops when its input ends
		select {
		case <-exited:
		case <-time.After(luaStopGrace):
			_ = cmd.Process.Kill()
			<-exited
		}
		job.Close()
	}()

	// stdin is written from this goroutine only; a host that doesn't read
	// can't block us for long because the pipe has room for these messages.
	w := bufio.NewWriter(stdin)
	write := func(m luahost.LuaMsg) {
		data, _ := json.Marshal(m)
		_, _ = w.Write(append(data, '\n'))
		_ = w.Flush()
	}
	type reply struct {
		m   luahost.LuaMsg
		err error
	}
	msgs := make(chan reply, 16)
	gone := make(chan struct{}) // closed when we stop listening
	defer close(gone)
	send := func(r reply) bool {
		select {
		case msgs <- r:
			return true
		case <-gone:
			return false
		}
	}
	go func() {
		br := bufio.NewReaderSize(stdout, 64<<10)
		for {
			line, err := luahost.ReadLimitedLine(br, luahost.MaxLine)
			if err != nil {
				if errors.Is(err, io.EOF) {
					err = errors.New("Lua가 결과 없이 끝났습니다")
				} else {
					err = errors.New("Lua가 너무 긴 메시지를 보냈습니다")
				}
				send(reply{err: err})
				return
			}
			var m luahost.LuaMsg
			if json.Unmarshal(line, &m) != nil {
				send(reply{err: errors.New("Lua가 잘못된 메시지를 보냈습니다")})
				return
			}
			if !send(reply{m: m}) {
				return
			}
		}
	}()

	write(luahost.LuaMsg{T: "repl", Limit: luaMemoryLimit})
	write(luahost.LuaMsg{T: "eval", ID: 1, D: code})

	var out strings.Builder
	res := LuaResult{}
	finish := func() (LuaResult, error) {
		res.Output = strings.TrimSuffix(out.String(), "\n")
		res.Ms = time.Since(started).Milliseconds()
		dbg.Printf("lua eval: %d bytes of code, %v, error %q", len(code), time.Since(started), res.Error)
		return res, nil
	}
	timeout := time.NewTimer(luaEvalTimeout)
	defer timeout.Stop()
	interrupted := false
	for {
		select {
		case r := <-msgs:
			if r.err != nil {
				if job.HitMemoryLimit() {
					res.Error = fmt.Sprintf("메모리 한도(%dMB)를 넘어 멈췄습니다", luaMemoryLimit>>20)
				} else {
					res.Error = r.err.Error()
				}
				return finish()
			}
			switch r.m.T {
			case "print":
				if out.Len() < luaEvalMaxOutput {
					out.WriteString(r.m.D)
					out.WriteByte('\n')
					if out.Len() >= luaEvalMaxOutput {
						s := out.String()[:luaEvalMaxOutput]
						out.Reset()
						out.WriteString(strings.ToValidUTF8(s, "") + "\n…\n")
					}
				}
			case "send", "screen":
				write(luahost.LuaMsg{T: "ret", ID: r.m.ID, Err: "탭 없이 실행할 때는 " + r.m.T + "()를 쓸 수 없습니다"})
			case "openurl", "openfolder":
				ret := luahost.LuaMsg{T: "ret", ID: r.m.ID}
				o, err := a.open(a.local(), strings.TrimPrefix(r.m.T, "open"), r.m.D)
				if err != nil {
					ret.Err = err.Error()
				} else if o.InPage {
					res.Open = append(res.Open, o.Target)
				}
				write(ret)
			case "evaldone":
				res.Value, res.Error = r.m.D, r.m.Err
				if interrupted && res.Error != "" {
					res.Error = fmt.Sprintf("%g초가 지나 멈췄습니다", luaEvalTimeout.Seconds())
				}
				return finish()
			default:
				res.Error = "Lua가 잘못된 메시지를 보냈습니다"
				return finish()
			}
		case <-timeout.C:
			if interrupted {
				res.Error = "Lua가 멈추지 않아 강제로 끝냈습니다"
				_ = cmd.Process.Kill()
				return finish()
			}
			interrupted = true
			write(luahost.LuaMsg{T: "interrupt"})
			timeout.Reset(luaStopGrace)
		}
	}
}
