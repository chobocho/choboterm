package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Telnet commands and options (RFC 854, 857, 858, 1073, 1091).
const (
	tnIAC  = 255
	tnDONT = 254
	tnDO   = 253
	tnWONT = 252
	tnWILL = 251
	tnSB   = 250
	tnSE   = 240

	optBinary = 0
	optEcho   = 1
	optSGA    = 3
	optTType  = 24
	optNAWS   = 31

	ttypeIS   = 0
	ttypeSEND = 1
)

const terminalType = "XTERM"

type telnetSession struct {
	conn net.Conn
	wmu  sync.Mutex

	// parser state (only touched by Read)
	state  int
	cmd    byte
	sb     []byte
	sbIAC  bool
	pend   []byte
	cols   int
	rows   int
	naws   bool
	binary bool

	// auto-login
	login, pass   string
	sentLogin     bool
	sentPass      bool
	tail          []byte
	loginDeadline time.Time
}

const (
	stData = iota
	stIAC
	stCmd
	stSB
)

func dialTelnet(req ConnectRequest) (Session, error) {
	addr := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Telnet 접속 실패: %w", err)
	}
	return &telnetSession{
		conn:          conn,
		cols:          req.Cols,
		rows:          req.Rows,
		login:         req.Login,
		pass:          req.Pass,
		sentLogin:     req.Login == "",
		sentPass:      req.Pass == "",
		loginDeadline: time.Now().Add(60 * time.Second),
	}, nil
}

func (t *telnetSession) Close() error { return t.conn.Close() }

func (t *telnetSession) rawWrite(p []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	_, err := t.conn.Write(p)
	return err
}

// Write escapes IAC bytes and converts a bare CR to CR NUL as NVT requires.
func (t *telnetSession) Write(p []byte) (int, error) {
	t.wmu.Lock()
	binary := t.binary
	t.wmu.Unlock()
	out := make([]byte, 0, len(p)+8)
	for i, b := range p {
		out = append(out, b)
		switch {
		case b == tnIAC:
			out = append(out, tnIAC)
		case b == '\r' && !binary && (i+1 >= len(p) || p[i+1] != '\n'):
			out = append(out, 0)
		}
	}
	return len(p), t.rawWrite(out)
}

func (t *telnetSession) Resize(cols, rows int) error {
	t.wmu.Lock()
	t.cols, t.rows = cols, rows
	naws := t.naws
	t.wmu.Unlock()
	if !naws {
		return nil
	}
	return t.sendNAWS()
}

func (t *telnetSession) setBinary(on bool) {
	t.wmu.Lock()
	t.binary = on
	t.wmu.Unlock()
}

func (t *telnetSession) sendNAWS() error {
	t.wmu.Lock()
	cols, rows := t.cols, t.rows
	t.wmu.Unlock()
	msg := []byte{tnIAC, tnSB, optNAWS}
	for _, v := range []int{cols, rows} {
		hi, lo := byte(v>>8), byte(v)
		msg = append(msg, hi)
		if hi == tnIAC {
			msg = append(msg, tnIAC)
		}
		msg = append(msg, lo)
		if lo == tnIAC {
			msg = append(msg, tnIAC)
		}
	}
	msg = append(msg, tnIAC, tnSE)
	return t.rawWrite(msg)
}

// Read returns terminal data with telnet negotiation stripped out.
func (t *telnetSession) Read(p []byte) (int, error) {
	for len(t.pend) == 0 {
		buf := make([]byte, len(p))
		n, err := t.conn.Read(buf)
		if n > 0 {
			t.pend = t.parse(buf[:n])
			t.autoLogin(t.pend)
		}
		if err != nil {
			if len(t.pend) > 0 {
				break
			}
			return 0, err
		}
	}
	n := copy(p, t.pend)
	t.pend = t.pend[n:]
	return n, nil
}

func (t *telnetSession) parse(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for _, b := range in {
		switch t.state {
		case stData:
			if b == tnIAC {
				t.state = stIAC
			} else {
				out = append(out, b)
			}
		case stIAC:
			switch b {
			case tnIAC:
				out = append(out, tnIAC)
				t.state = stData
			case tnDO, tnDONT, tnWILL, tnWONT:
				t.cmd = b
				t.state = stCmd
			case tnSB:
				t.sb = t.sb[:0]
				t.sbIAC = false
				t.state = stSB
			default:
				t.state = stData
			}
		case stCmd:
			t.negotiate(t.cmd, b)
			t.state = stData
		case stSB:
			if t.sbIAC {
				t.sbIAC = false
				if b == tnSE {
					t.subnegotiate(t.sb)
					t.state = stData
					continue
				}
				t.sb = append(t.sb, b)
				continue
			}
			if b == tnIAC {
				t.sbIAC = true
				continue
			}
			t.sb = append(t.sb, b)
		}
	}
	return out
}

func (t *telnetSession) negotiate(cmd, opt byte) {
	reply := func(c byte) { _ = t.rawWrite([]byte{tnIAC, c, opt}) }
	switch cmd {
	case tnDO:
		switch opt {
		case optNAWS:
			reply(tnWILL)
			t.wmu.Lock()
			t.naws = true
			t.wmu.Unlock()
			_ = t.sendNAWS()
		case optTType, optSGA, optBinary:
			reply(tnWILL)
			if opt == optBinary {
				t.setBinary(true)
			}
		default:
			reply(tnWONT)
		}
	case tnDONT:
		if opt == optBinary {
			t.setBinary(false)
		}
		reply(tnWONT)
	case tnWILL:
		switch opt {
		case optEcho, optSGA, optBinary:
			reply(tnDO)
		default:
			reply(tnDONT)
		}
	case tnWONT:
		reply(tnDONT)
	}
}

func (t *telnetSession) subnegotiate(sb []byte) {
	if len(sb) >= 2 && sb[0] == optTType && sb[1] == ttypeSEND {
		msg := append([]byte{tnIAC, tnSB, optTType, ttypeIS}, terminalType...)
		msg = append(msg, tnIAC, tnSE)
		_ = t.rawWrite(msg)
	}
}

// autoLogin answers the first login/password prompts with the dialog values.
func (t *telnetSession) autoLogin(data []byte) {
	if t.sentLogin && t.sentPass {
		return
	}
	if time.Now().After(t.loginDeadline) {
		t.sentLogin, t.sentPass = true, true
		return
	}
	t.tail = append(t.tail, data...)
	if len(t.tail) > 256 {
		t.tail = t.tail[len(t.tail)-256:]
	}
	text := strings.ToLower(strings.TrimRight(string(t.tail), " \t"))

	if !t.sentLogin && hasPromptSuffix(text, "login:", "username:", "user:", "user name:", "아이디:") {
		t.sentLogin = true
		t.tail = t.tail[:0]
		_, _ = t.Write([]byte(t.login + "\r"))
		return
	}
	if !t.sentPass && hasPromptSuffix(text, "password:", "passwd:", "비밀번호:", "암호:") {
		t.sentPass = true
		t.tail = t.tail[:0]
		_, _ = t.Write([]byte(t.pass + "\r"))
	}
}

func hasPromptSuffix(text string, prompts ...string) bool {
	for _, p := range prompts {
		if strings.HasSuffix(text, p) {
			return true
		}
	}
	return false
}
