package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// zmodemState tracks the running ZMODEM transfer, if any.
type zmodemState struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (a *App) zmodemActive() bool {
	a.zm.mu.Lock()
	defer a.zm.mu.Unlock()
	return a.zm.cancel != nil
}

func (a *App) cancelZmodem() {
	a.zm.mu.Lock()
	c := a.zm.cancel
	a.zm.mu.Unlock()
	if c != nil {
		c()
	}
}

// termMessage prints a status line in the terminal (yellow).
func (a *App) termMessage(msg string) {
	msg = strings.ReplaceAll(msg, "\n", "\r\n")
	data := "\r\n\x1b[33m" + msg + "\x1b[0m\r\n"
	a.emit("term:data", base64.StdEncoding.EncodeToString([]byte(data)))
}

// runZmodem handles one sz (receive=true) or rz session detected in the output.
// Raw remote bytes arrive on in; unconsumed bytes are handed back on done.
func (a *App) runZmodem(sess Session, receive bool, in <-chan []byte, done chan<- []byte) {
	ctx, cancel := context.WithCancel(context.Background())
	a.zm.mu.Lock()
	a.zm.cancel = cancel
	a.zm.mu.Unlock()

	_, telnet := sess.(*telnetSession)
	p := &zmPeer{
		ctx:     ctx,
		in:      in,
		write:   func(b []byte) error { _, err := sess.Write(b); return err },
		escAll:  telnet, // Telnet may mangle CR and control bytes in transit
		timeout: 15 * time.Second,
	}

	var last time.Time
	o := zmOptions{
		Dir:        a.downloadDir(),
		DecodeName: a.codec.DecodeString,
		EncodeName: a.codec.EncodeString,
		Progress: func(x XferProgress) {
			if now := time.Now(); now.Sub(last) >= 100*time.Millisecond || x.Done == x.Total {
				last = now
				a.emit("xfer:progress", x)
			}
		},
	}

	var (
		msg string
		err error
	)
	if receive {
		a.termMessage("[Zmodem] 파일을 받는 중입니다... (Ctrl+C: 취소)")
		var saved []string
		saved, err = zmReceive(p, o)
		if len(saved) > 0 {
			msg = fmt.Sprintf("[Zmodem] %d개 파일을 받았습니다: %s\n  %s", len(saved), o.Dir, strings.Join(saved, "\n  "))
		}
	} else {
		files, derr := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Zmodem으로 보낼 파일 선택"})
		if derr != nil || len(files) == 0 {
			p.abort()
			err = errCancelled
		} else {
			a.termMessage(fmt.Sprintf("[Zmodem] %d개 파일을 보내는 중입니다... (Ctrl+C: 취소)", len(files)))
			if err = zmSend(p, files, o); err == nil {
				msg = fmt.Sprintf("[Zmodem] %d개 파일을 보냈습니다", len(files))
			}
		}
	}

	cancel()
	a.zm.mu.Lock()
	a.zm.cancel = nil
	a.zm.mu.Unlock()

	if err != nil {
		if ctx.Err() != nil {
			err = errCancelled
		}
		if msg != "" {
			msg += "\n"
		}
		msg += "[Zmodem] " + err.Error()
	}
	a.emit("xfer:end", XferEnd{OK: err == nil, Message: msg})
	a.termMessage(msg)

	if errors.Is(err, errCancelled) {
		// Give the remote a moment to print its own cancel message.
		time.Sleep(200 * time.Millisecond)
	}
	done <- p.buf
}

// emit sends an event to the frontend (replaceable in tests).
func (a *App) emit(name string, data ...interface{}) {
	if a.hooks.emit != nil {
		a.hooks.emit(name, data...)
		return
	}
	runtime.EventsEmit(a.ctx, name, data...)
}

func (a *App) setTitle(title string) {
	if a.hooks.emit != nil {
		return
	}
	runtime.WindowSetTitle(a.ctx, title)
}

func (a *App) downloadDir() string {
	if a.hooks.downloadDir != "" {
		return a.hooks.downloadDir
	}
	return downloadsDir()
}
