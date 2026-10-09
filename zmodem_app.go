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

// zmodemState tracks a tab's running ZMODEM transfer, if any.
type zmodemState struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (t *tab) zmodemActive() bool {
	t.zm.mu.Lock()
	defer t.zm.mu.Unlock()
	return t.zm.cancel != nil
}

func (t *tab) cancelZmodem() {
	t.zm.mu.Lock()
	c := t.zm.cancel
	t.zm.mu.Unlock()
	if c != nil {
		c()
	}
}

// termMessage prints a status line in the tab's terminal (yellow).
func (t *tab) termMessage(msg string) {
	msg = strings.ReplaceAll(tr(msg), "\n", "\r\n")
	data := "\r\n\x1b[33m" + msg + "\x1b[0m\r\n"
	t.emit("term:data", base64.StdEncoding.EncodeToString([]byte(data)))
}

// runZmodem handles one sz (receive=true) or rz session detected in the output.
// Raw remote bytes arrive on in; unconsumed bytes are handed back on done.
func (t *tab) runZmodem(sess Session, receive bool, in <-chan []byte, done chan<- []byte) {
	a := t.app
	ctx, cancel := context.WithCancel(context.Background())
	t.zm.mu.Lock()
	t.zm.cancel = cancel
	t.zm.mu.Unlock()

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
		DecodeName: t.codec.DecodeString,
		EncodeName: t.codec.EncodeString,
		Progress: func(x XferProgress) {
			if now := time.Now(); now.Sub(last) >= 100*time.Millisecond || x.Done == x.Total {
				last = now
				t.emit("xfer:progress", x)
			}
		},
	}

	var (
		msg string
		err error
	)
	if receive {
		t.termMessage("[Zmodem] 파일을 받는 중입니다... (Ctrl+C: 취소)")
		var saved []string
		saved, err = zmReceive(p, o)
		if len(saved) > 0 {
			msg = fmt.Sprintf("[Zmodem] %d개 파일을 받았습니다: %s\n  %s", len(saved), o.Dir, strings.Join(saved, "\n  "))
		}
	} else {
		files, derr := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: tr("Zmodem으로 보낼 파일 선택")})
		if derr != nil || len(files) == 0 {
			p.abort()
			err = errCancelled
		} else {
			t.termMessage(fmt.Sprintf("[Zmodem] %d개 파일을 보내는 중입니다... (Ctrl+C: 취소)", len(files)))
			if err = zmSend(p, files, o); err == nil {
				msg = fmt.Sprintf("[Zmodem] %d개 파일을 보냈습니다", len(files))
			}
		}
	}

	cancel()
	t.zm.mu.Lock()
	t.zm.cancel = nil
	t.zm.mu.Unlock()

	if err != nil {
		if ctx.Err() != nil {
			err = errCancelled
		}
		if msg != "" {
			msg += "\n"
		}
		msg += "[Zmodem] " + err.Error()
	}
	t.emit("xfer:end", XferEnd{OK: err == nil, Message: msg})
	t.termMessage(msg)

	if errors.Is(err, errCancelled) {
		// Give the remote a moment to print its own cancel message.
		time.Sleep(200 * time.Millisecond)
	}
	done <- p.buf
}

func (a *App) downloadDir() string {
	if a.hooks.downloadDir != "" {
		return a.hooks.downloadDir
	}
	return downloadsDir()
}
