package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
)

// Session is a live terminal connection (SSH or Telnet).
type Session interface {
	io.Reader
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
}

// errConnLost means the connection broke instead of ending normally
// (e.g. the shell exited); the frontend may reconnect automatically.
var errConnLost = errors.New("네트워크 연결이 끊어졌습니다")

// errNoReply is a keepalive that got no answer in time.
var errNoReply = errors.New("응답이 없습니다")

// keepAliveTimeout is how long one keepalive may wait for an answer.
var keepAliveTimeout = 15 * time.Second

// keepAliveMisses is how many keepalives in a row may fail before the
// connection is considered dead and closed.
const keepAliveMisses = 3

// keepAliver is a session that can check its connection is alive.
type keepAliver interface {
	keepAlive() error
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// ConnectRequest carries the values from the Connect dialog.
type ConnectRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Pass     string `json:"pass"`
	Encoding string `json:"encoding"`
	Jump     string `json:"jump"` // SSH jump hosts (ssh -J); "" = ProxyJump of ~/.ssh/config, "none" = none
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// App struct
type App struct {
	ctx   context.Context
	mu    sync.Mutex
	tabs  map[int]*tab
	hooks testHooks
	// fwdMu serializes deciding which tab to a server runs its forwarding
	// rules (see forwardRule).
	fwdMu sync.Mutex
	shown sync.Once
}

// tab is one connection with its own terminal tab in the frontend.
// Events for a tab carry its id as the first argument.
type tab struct {
	id    int
	app   *App
	mu    sync.Mutex
	sess  Session
	codec *codec
	files fileState
	zm    zmodemState
	fwd   forwards
	log   sessionLog
	// The Lua script running in the tab, if any (see luarun.go).
	script atomic.Pointer[scriptRun]
	dead   error  // why keepalive closed sess (guarded by mu)
	host   string // where sess is connected (guarded by mu)
	port   int
}

// testHooks lets tests run the app without a Wails window.
type testHooks struct {
	emit        func(name string, data ...interface{})
	downloadDir string
	confirmKey  func(host, fingerprint string) bool
	prompt      func(Prompt) ([]string, bool)
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{tabs: make(map[int]*tab)}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// domReady shows the window at its saved position the first time the page loads.
func (a *App) domReady(ctx context.Context) {
	debugf("dom ready")
	a.shown.Do(func() {
		if w := loadSettings().Window; w != nil {
			restoreWindowBounds(*w)
		}
		runtime.WindowShow(ctx)
	})
}

// beforeClose remembers the window position for the next start.
func (a *App) beforeClose(ctx context.Context) bool {
	if w, ok := currentWindowState(); ok && w.valid() {
		_ = updateSettings(func(s *Settings) { s.Window = &w })
	}
	return false
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	tabs := a.tabs
	a.tabs = make(map[int]*tab)
	a.mu.Unlock()
	for _, t := range tabs {
		t.disconnect()
		t.log.stop()
	}
}

// getTab returns the tab with id, creating it on first use.
func (a *App) getTab(id int) *tab {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tabs[id]
	if t == nil {
		t = &tab{id: id, app: a, codec: newCodec(EncodingUTF8)}
		a.tabs[id] = t
	}
	return t
}

// findTab returns the tab with id, or nil.
func (a *App) findTab(id int) *tab {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tabs[id]
}

// emit sends an event to the frontend (replaceable in tests).
func (a *App) emit(name string, data ...interface{}) {
	if a.hooks.emit != nil {
		a.hooks.emit(name, data...)
		return
	}
	runtime.EventsEmit(a.ctx, name, data...)
}

func (t *tab) emit(name string, data ...interface{}) {
	t.app.emit(name, append([]interface{}{t.id}, data...)...)
}

func (t *tab) session() Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sess
}

// Protocol returns the protocol of a well-known port ("ssh" 22, "ftp" 21,
// "telnet" 23), or "" when it must be detected from the server greeting.
func Protocol(port int) string {
	switch port {
	case 22:
		return "ssh"
	case 21:
		return "ftp"
	case 23:
		return "telnet"
	}
	return ""
}

// Connect opens a session in the given tab and returns the protocol used
// ("ssh", "telnet" or "ftp"). Ports 22/21/23 decide the protocol directly;
// for other ports it is detected from the server greeting (e.g. SSH on 8022).
// An existing connection in that tab is closed first.
func (a *App) Connect(tabID int, req ConnectRequest) (string, error) {
	req.Host = strings.TrimSpace(req.Host)
	if req.Host == "" {
		return "", errors.New("Host를 입력하세요")
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		req.Cols, req.Rows = 80, 24
	}
	if cmdline, ok := localCommand(req.Host); ok {
		return a.connectLocal(tabID, req, cmdline)
	}
	if req.Port <= 0 || req.Port > 65535 {
		return "", errors.New("Port가 올바르지 않습니다")
	}

	// "ssh [-p port] [user@]name" and names from ~/.ssh/config: req keeps
	// the name (tab title, history), dial holds the address to connect to.
	cfg := loadSSHConfig()
	if j := sshCommandJump(req.Host); j != "" {
		req.Jump = j // ssh -J on the command line
	}
	if _, _, _, isCmd := parseSSHCommand(req.Host); isCmd {
		st := lookupSSHTarget(cfg, req.Host)
		req.Host, req.Port = st.Host, st.Port
		if st.Login != "" {
			req.Login = st.Login
		}
	}
	dial := req
	dial.Host = sshDialHost(cfg, req.Host)

	t := a.getTab(tabID)
	t.disconnect()

	proto := Protocol(req.Port)
	var (
		conn  net.Conn
		jumps []*ssh.Client
	)
	// Through jump hosts (ssh -J, ProxyJump): only SSH makes sense, and the
	// target may not be reachable from here, so it isn't probed first.
	if spec := proxyJumpFor(cfg, req.Host, req.Jump); spec != "" && (proto == "" || proto == "ssh") {
		proto = "ssh"
		if dial.Login == "" {
			dial.Login = configGet(cfg, req.Host, "User")
		}
		hops, err := parseJumps(cfg, spec, dial.Login)
		if err != nil {
			return "", err
		}
		target := net.JoinHostPort(dial.Host, strconv.Itoa(req.Port))
		if jumps, conn, err = dialJumps(hops, target, a.confirmHostKey, a.asker(tabID)); err != nil {
			return "", err
		}
	}
	if proto == "" {
		c, p, err := dialDetect(net.JoinHostPort(dial.Host, strconv.Itoa(req.Port)))
		if err != nil {
			return "", fmt.Errorf("접속 실패: %w", err)
		}
		conn, proto = c, p
	}

	if proto == "ftp" {
		fs, err := dialFTP(dial, conn)
		if err != nil {
			return "", err
		}
		t.files.mu.Lock()
		t.files.fs, t.files.proto = fs, "FTP"
		t.files.mu.Unlock()
		enc := t.codec.Set(req.Encoding)
		_ = addHistory(HostEntry{Host: req.Host, Port: req.Port, Login: req.Login, Encoding: enc}, nil)
		return proto, nil
	}

	var (
		sess Session
		err  error
	)
	if proto == "ssh" {
		if dial.Login == "" {
			dial.Login = configGet(cfg, req.Host, "User")
		}
		sess, err = dialSSH(dial, a.confirmHostKey, a.asker(tabID), conn, sshIdentityFiles(cfg, req.Host, dial.Login))
		if err != nil {
			closeClients(jumps)
		} else {
			sess.(*sshSession).jumps = jumps
		}
	} else {
		sess, err = dialTelnet(dial, conn)
	}
	if err != nil {
		return "", err
	}

	enc := t.codec.Set(req.Encoding)
	// Telnet passwords are remembered (DPAPI-encrypted) and filled in next time.
	var savePass *string
	if proto == "telnet" {
		savePass = &req.Pass
	}
	_ = addHistory(HostEntry{Host: req.Host, Port: req.Port, Login: req.Login, Encoding: enc, Jump: req.Jump}, savePass)
	t.start(sess, proto, req)
	return proto, nil
}

// connectLocal runs a local shell (cmd, PowerShell, WSL) in the tab.
// The pseudo console always speaks UTF-8.
func (a *App) connectLocal(tabID int, req ConnectRequest, cmdline string) (string, error) {
	t := a.getTab(tabID)
	t.disconnect()
	sess, err := startPty(cmdline, localHome(), req.Cols, req.Rows)
	if err != nil {
		return "", err
	}
	t.codec.Set(EncodingUTF8)
	req.Port = 0
	_ = addHistory(HostEntry{Host: req.Host, Encoding: EncodingUTF8}, nil)
	t.start(sess, "local", req)
	return "local", nil
}

// start makes sess the tab's session and starts reading it.
func (t *tab) start(sess Session, proto string, req ConnectRequest) {
	t.mu.Lock()
	t.sess = sess
	t.host, t.port = req.Host, req.Port
	t.mu.Unlock()

	go t.pump(sess)
	go t.keepAlive(sess, time.Duration(loadSettings().KeepAlive)*time.Second)
	if s, ok := sess.(*sshSession); ok {
		t.restoreForwards(s, req.Host, req.Port)
	}
	if t.log.active() != "" {
		t.log.mark(fmt.Sprintf("접속: %s %s:%d", proto, req.Host, req.Port))
	} else if loadSettings().LogAuto {
		_, _ = t.startLog() // a failure must not fail the connection
	}
}

// keepAlive checks the connection every interval while sess is the tab's
// session, and closes it after several checks in a row failed.
func (t *tab) keepAlive(sess Session, every time.Duration) {
	ka, ok := sess.(keepAliver)
	if !ok || every <= 0 {
		return
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	misses := 0
	for range tick.C {
		if t.session() != sess {
			return
		}
		err := ka.keepAlive()
		if err == nil {
			misses = 0
			continue
		}
		debugf("tab %d keepalive failed: %v", t.id, err)
		if misses++; misses < keepAliveMisses {
			continue
		}
		t.mu.Lock()
		current := t.sess == sess
		if current {
			t.dead = fmt.Errorf("서버가 %d번 연속 응답하지 않아 연결을 끊었습니다 (%v)", misses, err)
		}
		t.mu.Unlock()
		if current {
			_ = sess.Close()
		}
		return
	}
}

// pump reads session output and emits it to the frontend in batches,
// so that bulk output (e.g. cat of a large file) doesn't flood the event bridge.
// Output arriving after a quiet spell (a key echo) is sent at once; only output
// within flushInterval of the previous emit waits for the ticker.
func (t *tab) pump(sess Session) {
	chunks := make(chan []byte, 64)
	readErr := make(chan error, 1)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				readErr <- err
				close(chunks)
				return
			}
		}
	}()

	const maxBatch = 256 * 1024
	const flushInterval = 16 * time.Millisecond
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	var (
		pending  []byte
		held     []byte      // possible start of a ZMODEM sequence split across reads
		zmIn     chan []byte // non-nil while a ZMODEM transfer owns the stream
		zmDone   chan []byte // receives unconsumed bytes when the transfer ends
		lastEmit time.Time
	)
	flush := func() {
		if len(pending) > 0 {
			lastEmit = time.Now()
			if len(pending) <= 64 {
				debugf("tab %d emit %d bytes %q", t.id, len(pending), pending)
			} else {
				debugf("tab %d emit %d bytes", t.id, len(pending))
			}
			t.log.write(pending)
			if s := t.script.Load(); s != nil {
				s.feed(pending)
			}
			t.emit("term:data", base64.StdEncoding.EncodeToString(pending))
			pending = pending[:0]
		}
	}
	handle := func(data []byte) {
		if zmIn != nil {
			select {
			case zmIn <- data:
				return
			case left := <-zmDone:
				zmIn, zmDone = nil, nil
				data = append(left, data...)
			}
		}
		data = append(held, data...)
		held = nil
		if i, receive := findZmodemStart(data); i >= 0 {
			pending = append(pending, t.codec.Decode(data[:i])...)
			flush()
			zmIn, zmDone = make(chan []byte, 1024), make(chan []byte, 1)
			zmIn <- data[i:]
			go t.runZmodem(sess, receive, zmIn, zmDone)
			return
		}
		k := partialSigSuffix(data)
		held = append([]byte(nil), data[len(data)-k:]...)
		pending = append(pending, t.codec.Decode(data[:len(data)-k])...)
	}

	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				if zmIn != nil {
					close(zmIn) // the transfer sees EOF and stops
				}
				pending = append(pending, t.codec.Decode(held)...)
				flush()
				err := <-readErr
				debugf("tab %d read ended: %v", t.id, err)
				t.mu.Lock()
				current := t.sess == sess
				if current {
					t.sess = nil
					if t.dead != nil {
						err = t.dead
					}
				}
				t.dead = nil
				t.mu.Unlock()
				if current {
					t.fileClose()
					t.stopForwards()
					t.stopScript("연결이 끊어져 스크립트를 멈췄습니다")
					// A connection that broke (not one the server ended normally)
					// is reported as lost, so the frontend can reconnect.
					lost := err != nil && !errors.Is(err, io.EOF)
					msg := "연결이 종료되었습니다"
					if errors.Is(err, errConnLost) {
						msg = err.Error()
					} else if lost {
						msg = "연결이 끊어졌습니다: " + err.Error()
					}
					debugf("tab %d closed: %q lost=%v", t.id, msg, lost)
					t.log.mark(msg)
					t.emit("term:closed", msg, lost)
				}
				return
			}
			handle(c)
			if len(pending) >= maxBatch || time.Since(lastEmit) >= flushInterval {
				flush()
			}
		case left := <-zmDone: // nil channel (never ready) unless a transfer is running
			zmIn, zmDone = nil, nil
			if len(left) > 0 {
				handle(left)
			}
		case <-ticker.C:
			if len(held) > 0 && zmIn == nil {
				pending = append(pending, t.codec.Decode(held)...)
				held = nil
			}
			flush()
		}
	}
}

// Send writes keyboard input to a tab's session.
func (a *App) Send(tabID int, data string) {
	t := a.findTab(tabID)
	if t == nil {
		return
	}
	sess := t.session()
	if sess == nil {
		return
	}
	debugf("tab %d send %q", tabID, data)
	if t.zmodemActive() {
		// Keyboard input is not sent during a transfer; Ctrl+C / Ctrl+X cancel it.
		if strings.ContainsAny(data, "\x03\x18") {
			t.cancelZmodem()
		}
		return
	}
	_, _ = sess.Write(t.codec.Encode(data))
}

// SetEncoding switches a tab's character set ("UTF-8" or "EUC-KR")
// and returns the normalized name.
func (a *App) SetEncoding(tabID int, name string) string {
	return a.getTab(tabID).codec.Set(name)
}

// GetVersion returns the application version (e.g. "0.1.0").
func (a *App) GetVersion() string {
	return AppVersion
}

// Resize propagates a tab's terminal size to the remote side.
func (a *App) Resize(tabID, cols, rows int) {
	t := a.findTab(tabID)
	if t == nil {
		return
	}
	debugf("tab %d resize %dx%d", tabID, cols, rows)
	if sess := t.session(); sess != nil && cols > 0 && rows > 0 {
		_ = sess.Resize(cols, rows)
	}
}

// Disconnect closes a tab's connection but keeps the tab.
func (a *App) Disconnect(tabID int) {
	if t := a.findTab(tabID); t != nil {
		t.disconnect()
	}
}

// CloseTab closes a tab's connection and forgets the tab.
func (a *App) CloseTab(tabID int) {
	a.mu.Lock()
	t := a.tabs[tabID]
	delete(a.tabs, tabID)
	a.mu.Unlock()
	if t != nil {
		t.disconnect()
		t.log.stop()
	}
}

func (t *tab) disconnect() {
	t.fileClose()
	t.stopForwards()
	t.stopScript("연결을 끊어 스크립트를 멈췄습니다")
	t.mu.Lock()
	sess := t.sess
	t.sess = nil
	t.mu.Unlock()
	if sess != nil {
		t.log.mark("연결을 끊었습니다")
		_ = sess.Close()
	}
}

// GetHistory returns recently used hosts for the Host combo box.
func (a *App) GetHistory() []HostEntry {
	return loadHistory()
}

// confirmHostKey asks the user whether to trust an unknown SSH host key.
func (a *App) confirmHostKey(host, fingerprint string) bool {
	if a.hooks.confirmKey != nil {
		return a.hooks.confirmKey(host, fingerprint)
	}
	res, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:  runtime.QuestionDialog,
		Title: tr("알 수 없는 호스트"),
		Message: tr(fmt.Sprintf("%s 호스트를 처음 접속합니다.\n\n키 지문: %s\n\n이 호스트를 신뢰하고 계속하시겠습니까?",
			host, fingerprint)),
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "No",
	})
	return err == nil && res == "Yes"
}
