package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// sessionLog writes a tab's terminal output to a file while logging is on.
// It lives as long as the tab, so a reconnect keeps writing to the same file.
type sessionLog struct {
	mu    sync.Mutex
	f     *os.File
	path  string
	plain *plainText // nil: the output is written as received (with escape codes)
}

// logsDir is the default folder for session logs.
func logsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Documents", "choboterm", "logs")
	}
	return "logs"
}

// logFileName is "<host>_<port>_<yyyymmdd_hhmmss>.log" with characters
// Windows doesn't allow in file names (e.g. IPv6 colons) replaced.
func logFileName(host string, port int, at time.Time) string {
	if host == "" {
		host = "session"
	}
	host = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, host)
	if port > 0 {
		host = fmt.Sprintf("%s_%d", host, port)
	}
	return fmt.Sprintf("%s_%s.log", host, at.Format("20060102_150405"))
}

// start opens a new log file in dir. It returns the path, or "" with no
// error when logging is already on.
func (l *sessionLog) start(dir, host string, port int, plain bool) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return "", nil
	}
	if dir == "" {
		dir = logsDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("로그 폴더를 만들 수 없습니다: %w", err)
	}
	p := uniquePath(dir, logFileName(host, port, time.Now()))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("로그 파일을 만들 수 없습니다: %w", err)
	}
	l.f, l.path = f, p
	l.plain = nil
	if plain {
		l.plain = &plainText{}
	}
	return p, nil
}

// stop closes the log file and returns its path ("" if logging was off).
func (l *sessionLog) stop() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return ""
	}
	if l.plain != nil {
		_, _ = l.f.Write(l.plain.rest())
	}
	_ = l.f.Close()
	p := l.path
	l.f, l.path, l.plain = nil, "", nil
	return p
}

func (l *sessionLog) active() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// write adds terminal output (already decoded to UTF-8) to the log.
func (l *sessionLog) write(data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil || len(data) == 0 {
		return
	}
	if l.plain != nil {
		data = l.plain.feed(data)
	}
	if len(data) > 0 {
		_, _ = l.f.Write(data)
	}
}

// mark writes a line such as "===== 2026-10-03 19:00:00 접속: ... =====",
// on a line of its own.
func (l *sessionLog) mark(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	var b []byte
	if l.plain != nil {
		b = l.plain.rest() // ends the current line, if any
	} else if st, err := l.f.Stat(); err == nil && st.Size() > 0 {
		b = []byte("\r\n") // raw output may stop in the middle of a line
	}
	line := fmt.Sprintf("===== %s %s =====\r\n", time.Now().Format("2006-01-02 15:04:05"), msg)
	_, _ = l.f.Write(append(b, line...))
}

// plainText turns terminal output into readable text: escape sequences and
// control characters are removed, and carriage returns and backspaces are
// applied to the current line (so progress bars keep only their last state).
// A line is written once it ends with a line feed.
type plainText struct {
	state   int
	line    []rune
	col     int
	partial []byte // an incomplete UTF-8 sequence at the end of the last chunk
	params  []byte // CSI parameter bytes
}

const (
	ptText   = iota
	ptEsc    // after ESC
	ptEscInt // ESC + intermediate bytes, waiting for the final byte
	ptCSI    // ESC [
	ptString // OSC / DCS / SOS / PM / APC body, ended by BEL or ESC \
	ptStrEsc // ESC inside a string
)

func (p *plainText) feed(data []byte) []byte {
	if len(p.partial) > 0 {
		data = append(p.partial, data...)
		p.partial = nil
	}
	var out []byte
	for i := 0; i < len(data); {
		r, n := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && n <= 1 && !utf8.FullRune(data[i:]) {
			p.partial = append([]byte(nil), data[i:]...)
			break
		}
		i += n
		out = p.rune(r, out)
	}
	return out
}

func (p *plainText) rune(r rune, out []byte) []byte {
	switch p.state {
	case ptEsc:
		switch {
		case r == '[':
			p.state, p.params = ptCSI, p.params[:0]
		case r == ']' || r == 'P' || r == 'X' || r == '^' || r == '_':
			p.state = ptString
		case r >= 0x20 && r <= 0x2f:
			p.state = ptEscInt
		default:
			p.state = ptText
		}
		return out
	case ptEscInt:
		if r < 0x20 || r > 0x2f {
			p.state = ptText
		}
		return out
	case ptCSI:
		if r >= 0x40 && r <= 0x7e {
			p.state = ptText
			p.csi(r)
		} else if r < 0x80 {
			p.params = append(p.params, byte(r))
		}
		return out
	case ptString:
		if r == 0x07 {
			p.state = ptText
		} else if r == 0x1b {
			p.state = ptStrEsc
		}
		return out
	case ptStrEsc:
		if r == '\\' {
			p.state = ptText
		} else {
			p.state = ptString
		}
		return out
	}

	switch r {
	case 0x1b:
		p.state = ptEsc
	case '\n':
		out = append(out, p.flush()...)
	case '\r':
		p.col = 0
	case '\b':
		if p.col > 0 {
			p.col--
		}
	case '\t':
		p.put('\t')
	default:
		if r >= 0x20 && r != 0x7f && !(r >= 0x80 && r < 0xa0) {
			p.put(r)
		}
	}
	return out
}

// put writes r at the cursor, overwriting what is there.
func (p *plainText) put(r rune) {
	if p.col < len(p.line) {
		p.line[p.col] = r
	} else {
		for len(p.line) < p.col {
			p.line = append(p.line, ' ')
		}
		p.line = append(p.line, r)
	}
	p.col++
}

// csi applies the few cursor sequences that matter for a single line.
func (p *plainText) csi(final rune) {
	n := 1
	if _, err := fmt.Sscanf(string(p.params), "%d", &n); err != nil || n < 1 {
		n = 1
	}
	switch final {
	case 'K': // erase in line; only "to the end" matters as the line is not drawn yet
		if len(p.params) == 0 || string(p.params) == "0" {
			if p.col < len(p.line) {
				p.line = p.line[:p.col]
			}
		} else if string(p.params) == "2" {
			p.line = p.line[:0]
		}
	case 'C': // cursor forward
		p.col += n
	case 'D': // cursor back
		p.col = max(0, p.col-n)
	case 'G': // cursor to column
		p.col = n - 1
	}
}

// flush returns the current line with a line break and starts a new one.
func (p *plainText) flush() []byte {
	s := strings.TrimRight(string(p.line), " ")
	p.line, p.col = p.line[:0], 0
	return []byte(s + "\r\n")
}

// rest returns the unfinished line (e.g. a shell prompt) ended with a line
// break, or nothing when there is none.
func (p *plainText) rest() []byte {
	if len(p.line) == 0 {
		p.col = 0
		return nil
	}
	return p.flush()
}

// ---- App bindings ----

// startLog turns logging on for the tab (no-op when it already is).
func (t *tab) startLog() (string, error) {
	t.mu.Lock()
	host, port := t.host, t.port
	t.mu.Unlock()
	s := loadSettings()
	p, err := t.log.start(s.LogDir, host, port, !s.LogRaw)
	if err != nil || p == "" {
		return t.log.active(), err
	}
	if t.session() != nil {
		t.log.mark(fmt.Sprintf("로그 시작: %s:%d", host, port))
	}
	t.emit("log:changed", p)
	return p, nil
}

func (t *tab) stopLog() {
	if t.log.stop() != "" {
		t.emit("log:changed", "")
	}
}

// StartLog starts writing the tab's output to a new file in the log folder
// and returns its path.
func (a *App) StartLog(tabID int) (string, error) {
	return a.getTab(tabID).startLog()
}

// StopLog stops logging in a tab.
func (a *App) StopLog(tabID int) {
	if t := a.findTab(tabID); t != nil {
		t.stopLog()
	}
}

// LogPath returns the tab's log file, or "" when it isn't logging.
func (a *App) LogPath(tabID int) string {
	if t := a.findTab(tabID); t != nil {
		return t.log.active()
	}
	return ""
}

// ShowLogs opens the tab's log file selected in Explorer, or the log folder.
func (a *App) ShowLogs(tabID int) error {
	if p := a.LogPath(tabID); p != "" {
		return exec.Command("explorer", "/select,", p).Start()
	}
	dir := loadSettings().LogDir
	if dir == "" {
		dir = logsDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return exec.Command("explorer", dir).Start()
}

// ChooseLogDir asks for the log folder; "" means the dialog was cancelled.
func (a *App) ChooseLogDir(current string) (string, error) {
	if current == "" {
		current = logsDir()
	}
	if st, err := os.Stat(current); err != nil || !st.IsDir() {
		current = ""
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "로그 폴더 선택",
		DefaultDirectory: current,
	})
	if err != nil {
		return "", errors.New("폴더를 고르지 못했습니다: " + err.Error())
	}
	return dir, nil
}
