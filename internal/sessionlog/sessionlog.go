// Package sessionlog writes a tab's output to a log file, as received or as
// plain text.
package sessionlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"choboterm/internal/i18n"
	"choboterm/internal/transfer"
)

// Log writes a tab's terminal output to a file while logging is on.
// It lives as long as the tab, so a reconnect keeps writing to the same file.
type Log struct {
	mu    sync.Mutex
	f     *os.File
	path  string
	plain *PlainText // nil: the output is written as received (with escape codes)
	stamp bool       // each line starts with the time it began to arrive
	bol   bool       // raw output: the next byte starts a line
}

// logNow is the clock for log timestamps (tests replace it).
var logNow = time.Now

// logStamp is the prefix of a log line: "[2026-10-09 21:30:15.123] ".
func logStamp(t time.Time) string {
	return "[" + t.Format("2006-01-02 15:04:05.000") + "] "
}

// LogsDir is the default folder for session logs.
func LogsDir() string {
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

// Start opens a new log file in dir; stamp puts the time before each line.
// It returns the path, or "" with no error when logging is already on.
func (l *Log) Start(dir, host string, port int, plain, stamp bool) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return "", nil
	}
	if dir == "" {
		dir = LogsDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("로그 폴더를 만들 수 없습니다: %w", err)
	}
	p := transfer.UniquePath(dir, logFileName(host, port, time.Now()))
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("로그 파일을 만들 수 없습니다: %w", err)
	}
	l.f, l.path = f, p
	l.plain = nil
	if plain {
		l.plain = &PlainText{stamp: stamp}
	}
	l.stamp, l.bol = stamp, true
	return p, nil
}

// Stop closes the log file and returns its path ("" if logging was off).
func (l *Log) Stop() string {
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

func (l *Log) Active() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}

// Write adds terminal output (already decoded to UTF-8) to the log.
func (l *Log) Write(data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil || len(data) == 0 {
		return
	}
	if l.plain != nil {
		data = l.plain.Feed(data)
	} else if l.stamp {
		data = l.stampRaw(data)
	}
	if len(data) > 0 {
		_, _ = l.f.Write(data)
	}
}

// stampRaw puts the time at the start of each line of raw output.
func (l *Log) stampRaw(data []byte) []byte {
	out := make([]byte, 0, len(data)+32)
	stamp := ""
	for len(data) > 0 {
		if l.bol {
			if stamp == "" {
				stamp = logStamp(logNow())
			}
			out = append(out, stamp...)
			l.bol = false
		}
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return append(out, data...)
		}
		out = append(out, data[:i+1]...)
		data = data[i+1:]
		l.bol = true
	}
	return out
}

// Mark writes a line such as "===== 2026-10-03 19:00:00 접속: ... =====",
// on a line of its own.
func (l *Log) Mark(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	var b []byte
	if l.plain != nil {
		b = l.plain.rest() // ends the current line, if any
	} else if st, err := l.f.Stat(); err == nil && st.Size() > 0 && !(l.stamp && l.bol) {
		b = []byte("\r\n") // raw output may stop in the middle of a line
	}
	line := fmt.Sprintf("===== %s %s =====\r\n", time.Now().Format("2006-01-02 15:04:05"), i18n.Tr(msg))
	_, _ = l.f.Write(append(b, line...))
	l.bol = true
}

// PlainText turns terminal output into readable text: escape sequences and
// control characters are removed, and carriage returns and backspaces are
// applied to the current line (so progress bars keep only their last state).
// A line is written once it ends with a line feed.
type PlainText struct {
	stamp   bool      // lines start with the time their first character came
	started time.Time // when the current line got its first character
	state   int
	Line    []rune
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

func (p *PlainText) Feed(data []byte) []byte {
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

func (p *PlainText) rune(r rune, out []byte) []byte {
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
func (p *PlainText) put(r rune) {
	if p.stamp && p.started.IsZero() {
		p.started = logNow()
	}
	if p.col < len(p.Line) {
		p.Line[p.col] = r
	} else {
		for len(p.Line) < p.col {
			p.Line = append(p.Line, ' ')
		}
		p.Line = append(p.Line, r)
	}
	p.col++
}

// csi applies the few cursor sequences that matter for a single line.
func (p *PlainText) csi(final rune) {
	n := 1
	if _, err := fmt.Sscanf(string(p.params), "%d", &n); err != nil || n < 1 {
		n = 1
	}
	switch final {
	case 'K': // erase in line; only "to the end" matters as the line is not drawn yet
		if len(p.params) == 0 || string(p.params) == "0" {
			if p.col < len(p.Line) {
				p.Line = p.Line[:p.col]
			}
		} else if string(p.params) == "2" {
			p.Line = p.Line[:0]
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
func (p *PlainText) flush() []byte {
	s := strings.TrimRight(string(p.Line), " ")
	p.Line, p.col = p.Line[:0], 0
	if p.stamp {
		at := p.started
		if at.IsZero() {
			at = logNow() // an empty line
		}
		p.started = time.Time{}
		s = logStamp(at) + s
	}
	return []byte(s + "\r\n")
}

// rest returns the unfinished line (e.g. a shell prompt) ended with a line
// break, or nothing when there is none.
func (p *PlainText) rest() []byte {
	if len(p.Line) == 0 {
		p.col = 0
		return nil
	}
	return p.flush()
}

// ---- App bindings ----
