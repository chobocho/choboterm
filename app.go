package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
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
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Login string `json:"login"`
	Pass     string `json:"pass"`
	Encoding string `json:"encoding"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// App struct
type App struct {
	ctx   context.Context
	mu    sync.Mutex
	sess  Session
	codec *codec
	title string
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{codec: newCodec(EncodingUTF8)}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	sess := a.sess
	a.sess = nil
	a.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
}

// Protocol returns "ssh" for port 22, otherwise "telnet".
func Protocol(port int) string {
	if port == 22 {
		return "ssh"
	}
	return "telnet"
}

// Connect opens a session. Port 22 uses SSH, any other port uses Telnet.
func (a *App) Connect(req ConnectRequest) error {
	req.Host = strings.TrimSpace(req.Host)
	if req.Host == "" {
		return errors.New("Host를 입력하세요")
	}
	if req.Port <= 0 || req.Port > 65535 {
		return errors.New("Port가 올바르지 않습니다")
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		req.Cols, req.Rows = 80, 24
	}

	a.Disconnect()

	var (
		sess Session
		err  error
	)
	if Protocol(req.Port) == "ssh" {
		sess, err = dialSSH(req, a.confirmHostKey)
	} else {
		sess, err = dialTelnet(req)
	}
	if err != nil {
		return err
	}

	enc := a.codec.Set(req.Encoding)
	a.mu.Lock()
	a.sess = sess
	a.title = fmt.Sprintf("choboterm - %s:%d", req.Host, req.Port)
	a.mu.Unlock()

	_ = addHistory(HostEntry{Host: req.Host, Port: req.Port, Login: req.Login, Encoding: enc})
	a.updateTitle()
	go a.pump(sess)
	return nil
}

// pump reads session output and emits it to the frontend in batches,
// so that bulk output (e.g. cat of a large file) doesn't flood the event bridge.
func (a *App) pump(sess Session) {
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
	var pending []byte
	flush := func() {
		if len(pending) > 0 {
			runtime.EventsEmit(a.ctx, "term:data", base64.StdEncoding.EncodeToString(pending))
			pending = pending[:0]
		}
	}
	for {
		select {
		case c, ok := <-chunks:
			if !ok {
				flush()
				err := <-readErr
				a.mu.Lock()
				current := a.sess == sess
				if current {
					a.sess = nil
					a.title = ""
				}
				a.mu.Unlock()
				if current {
					msg := "연결이 종료되었습니다"
					if err != nil && !errors.Is(err, io.EOF) {
						msg += ": " + err.Error()
					}
					a.updateTitle()
					runtime.EventsEmit(a.ctx, "term:closed", msg)
				}
				return
			}
			pending = append(pending, a.codec.Decode(c)...)
			if len(pending) >= maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Send writes keyboard input to the session.
func (a *App) Send(data string) {
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess != nil {
		_, _ = sess.Write(a.codec.Encode(data))
	}
}

// SetEncoding switches the character set of the current connection
// ("UTF-8" or "EUC-KR") and returns the normalized name.
func (a *App) SetEncoding(name string) string {
	name = a.codec.Set(name)
	a.updateTitle()
	return name
}

func (a *App) updateTitle() {
	a.mu.Lock()
	title := a.title
	a.mu.Unlock()
	if title == "" {
		title = "choboterm"
	}
	runtime.WindowSetTitle(a.ctx, title+" ["+a.codec.Name()+"]")
}

// Resize propagates the terminal size to the remote side.
func (a *App) Resize(cols, rows int) {
	a.mu.Lock()
	sess := a.sess
	a.mu.Unlock()
	if sess != nil && cols > 0 && rows > 0 {
		_ = sess.Resize(cols, rows)
	}
}

// Disconnect closes the current session, if any.
func (a *App) Disconnect() {
	a.mu.Lock()
	sess := a.sess
	a.sess = nil
	a.title = ""
	a.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
		a.updateTitle()
	}
}

// GetHistory returns recently used hosts for the Host combo box.
func (a *App) GetHistory() []HostEntry {
	return loadHistory()
}

// confirmHostKey asks the user whether to trust an unknown SSH host key.
func (a *App) confirmHostKey(host, fingerprint string) bool {
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
