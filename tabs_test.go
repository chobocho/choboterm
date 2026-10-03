package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"sync"
	"testing"
	"time"
)

// pipeSession is a fake remote: the test writes "remote output" into out,
// and keyboard input written by the app is collected in in.
type pipeSession struct {
	r      *io.PipeReader
	out    *io.PipeWriter
	mu     sync.Mutex
	in     bytes.Buffer
	closed bool
}

func newPipeSession() *pipeSession {
	r, w := io.Pipe()
	return &pipeSession{r: r, out: w}
}

func (p *pipeSession) Read(b []byte) (int, error) { return p.r.Read(b) }
func (p *pipeSession) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.in.Write(b)
}
func (p *pipeSession) Resize(cols, rows int) error { return nil }
func (p *pipeSession) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	return p.r.Close()
}

func TestTabsAreIndependent(t *testing.T) {
	var (
		mu     sync.Mutex
		screen = map[int]*bytes.Buffer{1: {}, 2: {}}
		closed = make(chan int, 2)
	)
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		id := data[0].(int)
		switch name {
		case "term:data":
			b, _ := base64.StdEncoding.DecodeString(data[1].(string))
			mu.Lock()
			screen[id].Write(b)
			mu.Unlock()
		case "term:closed":
			closed <- id
		}
	}

	s1, s2 := newPipeSession(), newPipeSession()
	t1, t2 := a.getTab(1), a.getTab(2)
	t1.sess, t2.sess = s1, s2
	t2.codec.Set(EncodingEUCKR)
	go t1.pump(s1)
	go t2.pump(s2)

	s1.out.Write([]byte("hello from one"))
	s2.out.Write([]byte{0xC7, 0xD1, 0xB1, 0xDB}) // "한글" in EUC-KR, only tab 2 decodes it

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got1, got2 := screen[1].String(), screen[2].String()
		mu.Unlock()
		if got1 == "hello from one" && got2 == "한글" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("screens: %q / %q", got1, got2)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Keyboard input goes to the right tab, encoded with that tab's charset.
	a.Send(1, "ls\r")
	a.Send(2, "가")
	if s1.in.String() != "ls\r" || !bytes.Equal(s2.in.Bytes(), []byte{0xB0, 0xA1}) {
		t.Fatalf("input: %q / % X", s1.in.String(), s2.in.Bytes())
	}

	// Remote closing tab 1 doesn't touch tab 2.
	s1.out.Close()
	select {
	case id := <-closed:
		if id != 1 {
			t.Fatalf("closed tab %d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no term:closed for tab 1")
	}
	if t2.session() == nil {
		t.Fatal("tab 2 lost its session")
	}

	// Closing tab 2 from the UI closes its session and forgets the tab.
	a.CloseTab(2)
	if !s2.closed || a.findTab(2) != nil {
		t.Fatal("tab 2 not closed")
	}
}
