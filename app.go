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
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Session is a live terminal connection (SSH or Telnet).
type Session interface {
	io.Reader
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
}

// ConnectRequest carries the values from the Connect dialog.
type ConnectRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Pass     string `json:"pass"`
	Encoding string `json:"encoding"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// App struct
type App struct {
	ctx   context.Context
	mu    sync.Mutex
	tabs  map[int]*tab
	hooks testHooks
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
}

// testHooks lets tests run the app without a Wails window.
type testHooks struct {
	emit        func(name string, data ...interface{})
	downloadDir string
	confirmKey  func(host, fingerprint string) bool
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

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	tabs := a.tabs
	a.tabs = make(map[int]*tab)
	a.mu.Unlock()
	for _, t := range tabs {
		t.disconnect()
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
	if req.Port <= 0 || req.Port > 65535 {
		return "", errors.New("Port가 올바르지 않습니다")
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		req.Cols, req.Rows = 80, 24
	}

	t := a.getTab(tabID)
	t.disconnect()

	proto := Protocol(req.Port)
	var conn net.Conn
	if proto == "" {
		c, p, err := dialDetect(net.JoinHostPort(req.Host, strconv.Itoa(req.Port)))
		if err != nil {
			return "", fmt.Errorf("접속 실패: %w", err)
		}
		conn, proto = c, p
	}

	if proto == "ftp" {
		fs, err := dialFTP(req, conn)
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
		sess, err = dialSSH(req, a.confirmHostKey, conn)
	} else {
		sess, err = dialTelnet(req, conn)
	}
	if err != nil {
		return "", err
	}

	enc := t.codec.Set(req.Encoding)
	t.mu.Lock()
	t.sess = sess
	t.mu.Unlock()

	// Telnet passwords are remembered (DPAPI-encrypted) and filled in next time.
	var savePass *string
	if proto == "telnet" {
		savePass = &req.Pass
	}
	_ = addHistory(HostEntry{Host: req.Host, Port: req.Port, Login: req.Login, Encoding: enc}, savePass)
	go t.pump(sess)
	return proto, nil
}

// pump reads session output and emits it to the frontend in batches,
// so that bulk output (e.g. cat of a large file) doesn't flood the event bridge.
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
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	var (
		pending []byte
		held    []byte      // possible start of a ZMODEM sequence split across reads
		zmIn    chan []byte // non-nil while a ZMODEM transfer owns the stream
		zmDone  chan []byte // receives unconsumed bytes when the transfer ends
	)
	flush := func() {
		if len(pending) > 0 {
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
				t.mu.Lock()
				current := t.sess == sess
				if current {
					t.sess = nil
				}
				t.mu.Unlock()
				if current {
					t.fileClose()
					msg := "연결이 종료되었습니다"
					if err != nil && !errors.Is(err, io.EOF) {
						msg += ": " + err.Error()
					}
					t.emit("term:closed", msg)
				}
				return
			}
			handle(c)
			if len(pending) >= maxBatch {
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
	}
}

func (t *tab) disconnect() {
	t.fileClose()
	t.mu.Lock()
	sess := t.sess
	t.sess = nil
	t.mu.Unlock()
	if sess != nil {
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
		Title: "알 수 없는 호스트",
		Message: fmt.Sprintf("%s 호스트를 처음 접속합니다.\n\n키 지문: %s\n\n이 호스트를 신뢰하고 계속하시겠습니까?",
			host, fingerprint),
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "No",
	})
	return err == nil && res == "Yes"
}
