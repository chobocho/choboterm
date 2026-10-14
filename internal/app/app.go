// Package app is choboterm itself: the tabs and their connections, everything
// the page calls (the methods of App, which Wails binds), the web server's
// side of the app (internal/web serves the same page to browsers), and the
// window.
package app

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

	"choboterm/internal/codec"
	"choboterm/internal/config"
	"choboterm/internal/dbg"
	"choboterm/internal/dialog"
	"choboterm/internal/i18n"
	"choboterm/internal/localshell"
	"choboterm/internal/osutil"
	"choboterm/internal/serial"
	"choboterm/internal/sessionlog"
	"choboterm/internal/sshconn"
	"choboterm/internal/store"
	"choboterm/internal/telnet"
	"choboterm/internal/terminal"
	"choboterm/internal/transfer"
	"choboterm/internal/version"
	"choboterm/internal/web"
)

// keepAliveMisses is how many keepalives in a row may fail before the
// connection is considered dead and closed.
const keepAliveMisses = 3

// keepAliver is a session that can check its connection is alive.
type keepAliver interface {
	KeepAlive() error
}

// App is what the page calls (the methods Wails binds). The desktop window
// uses the App made by NewApp; each web client (see web_host.go) calls through its
// own App that shares the same appState, so a method can tell who called it
// (caller) and, for example, open the file dialog on the right screen.
type App struct {
	*appState
	caller *web.Client // nil: the desktop window (or a test)
}

// appState is the state shared by the window and every web client.
// It must have no exported methods: they would become Wails bindings.
type appState struct {
	ctx   context.Context // the Wails window; nil without one (tests, headless web)
	mu    sync.Mutex
	tabs  map[int]*tab
	hooks testHooks
	// fwdMu serializes deciding which tab to a server runs its forwarding
	// rules (see forwardRule).
	fwdMu sync.Mutex
	shown sync.Once
	quit  atomic.Bool // the user agreed to close the window (see beforeClose)
	// web is the web server while it runs (see web.go).
	web atomic.Pointer[web.Server]
	// webStart is the web server to start with the window (main's --web
	// or Settings > Web); nil = none.
	webStart *web.Options
	hidden   atomic.Bool            // the window was closed but the web server kept running (see WebHideWindow)
	webErr   atomic.Pointer[string] // why the web server didn't start, for Settings
}

// tab is one connection with its own terminal tab in the frontend.
// Events for a tab carry its id as the first argument.
type tab struct {
	id    int
	app   *App
	mu    sync.Mutex
	sess  terminal.Session
	codec *codec.Codec
	files fileState
	zm    zmodemState
	fwd   forwards
	log   sessionlog.Log
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
	picker      dialog.Picker // replaces the file dialogs
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{appState: &appState{tabs: make(map[int]*tab)}}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.webStart != nil {
		if err := a.startWebServer(*a.webStart); err != nil {
			dbg.Printf("web: %v", err)
		}
	}
}

// domReady shows the window at its saved position the first time the page loads.
func (a *App) domReady(ctx context.Context) {
	dbg.Printf("dom ready")
	a.shown.Do(func() {
		if w := config.Load().Window; w != nil {
			osutil.RestoreWindowBounds(*w)
		}
		runtime.WindowShow(ctx)
	})
}

// beforeClose remembers the window position for the next start. With a tab
// still connected, or browsers using the web server, the window stays open
// and the page asks first; Quit closes it (WebHideWindow hides it).
func (a *App) beforeClose(ctx context.Context) bool {
	if w, ok := osutil.CurrentWindowState(); ok && w.Valid() {
		_ = config.Update(func(s *config.Settings) { s.Window = &w })
	}
	if a.quit.Load() {
		return false
	}
	if w := a.web.Load(); w != nil {
		if n := w.ConnectedCount(); n > 0 {
			a.emit("app:closeWeb", n)
			return true
		}
	}
	if a.windowTabsConnected() {
		a.emit("app:close")
		return true
	}
	return false
}

// connectedWhere reports whether a tab whose id passes keep has a connection open.
func (a *App) connectedWhere(keep func(id int) bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, t := range a.tabs {
		if !keep(id) {
			continue
		}
		if t.session() != nil {
			return true
		}
		// An FTP tab has no session, only files. A file operation holds the lock.
		if !t.files.mu.TryLock() {
			return true
		}
		open := t.files.fs != nil
		t.files.mu.Unlock()
		if open {
			return true
		}
	}
	return false
}

// Quit closes the window without asking again. A web page can't quit the app.
func (a *App) Quit() {
	if a.caller != nil || a.ctx == nil {
		return
	}
	a.quit.Store(true)
	runtime.Quit(a.ctx)
}

func (a *App) shutdown(ctx context.Context) {
	if w := a.web.Load(); w != nil {
		w.Stop()
	}
	a.mu.Lock()
	tabs := a.tabs
	a.tabs = make(map[int]*tab)
	a.mu.Unlock()
	for _, t := range tabs {
		t.disconnect()
		t.log.Stop()
	}
}

// getTab returns the tab with id, creating it on first use.
func (a *App) getTab(id int) *tab {
	a.mu.Lock()
	defer a.mu.Unlock()
	t := a.tabs[id]
	if t == nil {
		t = &tab{id: id, app: a, codec: codec.New(codec.UTF8)}
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

// emit sends an event to the desktop window (replaceable in tests).
func (a *App) emit(name string, data ...interface{}) {
	a.emitFor(0, name, data...)
}

// emitFor sends an event to the page that owns tab tabID: the desktop window
// for the window's tabs (and tab 0), otherwise the web client (see clientOf).
func (a *App) emitFor(tabID int, name string, data ...interface{}) {
	if a.hooks.emit != nil {
		a.hooks.emit(name, data...)
		return
	}
	if c := web.ClientOf(tabID); c != web.WindowClient {
		if w := a.web.Load(); w != nil {
			w.Event(c, name, data)
		}
		return
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data...)
	}
}

func (t *tab) emit(name string, data ...interface{}) {
	t.app.emitFor(t.id, name, append([]interface{}{t.id}, data...)...)
}

// emitOutput shows terminal output in the tab: a term:data event with the
// bytes in base64 for the window, a binary message for a web client.
// out may be reused by the caller once this returns.
func (t *tab) emitOutput(out []byte) {
	if c := web.ClientOf(t.id); c != web.WindowClient && t.app.hooks.emit == nil {
		if w := t.app.web.Load(); w != nil {
			w.Output(c, t.id, out)
		}
		return
	}
	t.emit("term:data", base64.StdEncoding.EncodeToString(out))
}

func (t *tab) session() terminal.Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sess
}

// Connect opens a session in the given tab and returns the protocol used
// ("ssh", "telnet", "ftp", "local" or "serial"). Ports 22/21/23 decide the protocol directly;
// for other ports it is detected from the server greeting (e.g. SSH on 8022).
// An existing connection in that tab is closed first.
func (a *App) Connect(tabID int, req terminal.ConnectRequest) (string, error) {
	req.Host = strings.TrimSpace(req.Host)
	if req.Host == "" {
		return "", errors.New("Host를 입력하세요")
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		req.Cols, req.Rows = 80, 24
	}
	if cmdline, ok := localshell.Command(req.Host); ok {
		return a.connectLocal(tabID, req, cmdline)
	}
	if serial.IsPort(req.Host) {
		return a.connectSerial(tabID, req)
	}
	if req.Port <= 0 || req.Port > 65535 {
		return "", errors.New("Port가 올바르지 않습니다")
	}

	// "ssh [-p port] [user@]name" and names from ~/.ssh/config: req keeps
	// the name (tab title, history), dial holds the address to connect to.
	cfg := sshconn.LoadConfig()
	if j := sshconn.CommandJump(req.Host); j != "" {
		req.Jump = j // ssh -J on the command line
	}
	if _, _, _, isCmd := sshconn.ParseCommand(req.Host); isCmd {
		st := sshconn.LookupTarget(cfg, req.Host)
		req.Host, req.Port = st.Host, st.Port
		if st.Login != "" {
			req.Login = st.Login
		}
	}
	dial := req
	dial.Host = sshconn.DialHost(cfg, req.Host)

	t := a.getTab(tabID)
	t.disconnect()

	proto := terminal.Protocol(req.Port)
	var (
		conn  net.Conn
		jumps []*ssh.Client
	)
	// Through jump hosts (ssh -J, ProxyJump): only SSH makes sense, and the
	// target may not be reachable from here, so it isn't probed first.
	if spec := sshconn.ProxyJumpFor(cfg, req.Host, req.Jump); spec != "" && (proto == "" || proto == "ssh") {
		proto = "ssh"
		if dial.Login == "" {
			dial.Login = sshconn.ConfigGet(cfg, req.Host, "User")
		}
		hops, err := sshconn.ParseJumps(cfg, spec, dial.Login)
		if err != nil {
			return "", err
		}
		target := net.JoinHostPort(dial.Host, strconv.Itoa(req.Port))
		if jumps, conn, err = sshconn.DialJumps(hops, target, a.hostKeyConfirmer(tabID), a.asker(tabID)); err != nil {
			return "", err
		}
	}
	if proto == "" {
		c, p, err := terminal.DialDetect(net.JoinHostPort(dial.Host, strconv.Itoa(req.Port)))
		if err != nil {
			return "", fmt.Errorf("접속 실패: %w", err)
		}
		conn, proto = c, p
	}

	if proto == "ftp" {
		fs, err := transfer.DialFTP(dial, conn)
		if err != nil {
			return "", err
		}
		t.files.mu.Lock()
		t.files.fs, t.files.proto = fs, "FTP"
		t.files.mu.Unlock()
		enc := t.codec.Set(req.Encoding)
		_ = store.AddHistory(store.HostEntry{Host: req.Host, Port: req.Port, Login: req.Login, Encoding: enc}, nil)
		return proto, nil
	}

	var (
		sess terminal.Session
		err  error
	)
	if proto == "ssh" {
		if dial.Login == "" {
			dial.Login = sshconn.ConfigGet(cfg, req.Host, "User")
		}
		sess, err = sshconn.Dial(dial, a.hostKeyConfirmer(tabID), a.asker(tabID), conn, sshconn.IdentityFiles(cfg, req.Host, dial.Login))
		if err != nil {
			sshconn.CloseClients(jumps)
		} else {
			sess.(*sshconn.Session).Jumps = jumps
		}
	} else {
		sess, err = telnet.Dial(dial, conn)
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
	_ = store.AddHistory(store.HostEntry{Host: req.Host, Port: req.Port, Login: req.Login, Encoding: enc, Jump: req.Jump}, savePass)
	t.start(sess, proto, req)
	return proto, nil
}

// connectLocal runs a local shell (cmd, PowerShell, WSL) in the tab.
// The pseudo console always speaks UTF-8.
func (a *App) connectLocal(tabID int, req terminal.ConnectRequest, cmdline string) (string, error) {
	t := a.getTab(tabID)
	t.disconnect()
	sess, err := localshell.Start(cmdline, localshell.Home(), req.Cols, req.Rows)
	if err != nil {
		return "", err
	}
	t.codec.Set(codec.UTF8)
	req.Port = 0
	_ = store.AddHistory(store.HostEntry{Host: req.Host, Encoding: codec.UTF8}, nil)
	t.start(sess, "local", req)
	return "local", nil
}

// start makes sess the tab's session and starts reading it.
func (t *tab) start(sess terminal.Session, proto string, req terminal.ConnectRequest) {
	t.mu.Lock()
	t.sess = sess
	t.host, t.port = req.Host, req.Port
	t.mu.Unlock()

	// The page clears the tab's screen for the new session now, not when
	// Connect returns: the session's first output can get there before that.
	t.emit("term:open", t.codec.Name())
	go t.pump(sess)
	go t.keepAlive(sess, time.Duration(config.Load().KeepAlive)*time.Second)
	if s, ok := sess.(*sshconn.Session); ok {
		t.restoreForwards(s, req.Host, req.Port)
	}
	if t.log.Active() != "" {
		t.log.Mark(fmt.Sprintf("접속: %s %s:%d", proto, req.Host, req.Port))
	} else if config.Load().LogAuto {
		_, _ = t.startLog() // a failure must not fail the connection
	}
}

// keepAlive checks the connection every interval while sess is the tab's
// session, and closes it after several checks in a row failed.
func (t *tab) keepAlive(sess terminal.Session, every time.Duration) {
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
		err := ka.KeepAlive()
		if err == nil {
			misses = 0
			continue
		}
		dbg.Printf("tab %d keepalive failed: %v", t.id, err)
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
func (t *tab) pump(sess terminal.Session) {
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
				dbg.Printf("tab %d emit %d bytes %q", t.id, len(pending), pending)
			} else {
				dbg.Printf("tab %d emit %d bytes", t.id, len(pending))
			}
			t.log.Write(pending)
			if s := t.script.Load(); s != nil {
				s.feed(pending)
			}
			t.emitOutput(pending)
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
		if i, receive := transfer.FindZmodemStart(data); i >= 0 {
			pending = append(pending, t.codec.Decode(data[:i])...)
			flush()
			zmIn, zmDone = make(chan []byte, 1024), make(chan []byte, 1)
			zmIn <- data[i:]
			go t.runZmodem(sess, receive, zmIn, zmDone)
			return
		}
		k := transfer.PartialSigSuffix(data)
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
				dbg.Printf("tab %d read ended: %v", t.id, err)
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
					if errors.Is(err, terminal.ErrLost) {
						msg = err.Error()
					} else if lost {
						msg = "연결이 끊어졌습니다: " + err.Error()
					}
					dbg.Printf("tab %d closed: %q lost=%v", t.id, msg, lost)
					t.log.Mark(msg)
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
	dbg.Printf("tab %d send %q", tabID, data)
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
	return version.Version
}

// Resize propagates a tab's terminal size to the remote side.
func (a *App) Resize(tabID, cols, rows int) {
	t := a.findTab(tabID)
	if t == nil {
		return
	}
	dbg.Printf("tab %d resize %dx%d", tabID, cols, rows)
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
		t.log.Stop()
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
		t.log.Mark("연결을 끊었습니다")
		_ = sess.Close()
	}
}

// GetHistory returns recently used hosts for the Host combo box.
func (a *App) GetHistory() []store.HostEntry {
	return store.LoadHistory()
}

// hostKeyConfirmer returns the host key question for connections of a tab.
func (a *App) hostKeyConfirmer(tabID int) func(host, fingerprint string) bool {
	return func(host, fingerprint string) bool { return a.confirmHostKey(tabID, host, fingerprint) }
}

// confirmHostKey asks the user whether to trust an unknown SSH host key.
func (a *App) confirmHostKey(tabID int, host, fingerprint string) bool {
	if a.hooks.confirmKey != nil {
		return a.hooks.confirmKey(host, fingerprint)
	}
	if a.caller != nil || a.ctx == nil {
		// A web page: ask in the page, like other questions while connecting.
		return a.choose(tabID, i18n.Tr("알 수 없는 호스트"),
			i18n.Tr(fmt.Sprintf("%s 호스트를 처음 접속합니다.\n\n키 지문: %s\n\n이 호스트를 신뢰하고 계속하시겠습니까?",
				host, fingerprint)), "Yes", "No")
	}
	res, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:  runtime.QuestionDialog,
		Title: i18n.Tr("알 수 없는 호스트"),
		Message: i18n.Tr(fmt.Sprintf("%s 호스트를 처음 접속합니다.\n\n키 지문: %s\n\n이 호스트를 신뢰하고 계속하시겠습니까?",
			host, fingerprint)),
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "No",
	})
	return err == nil && res == "Yes"
}
