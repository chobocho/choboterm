package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"choboterm/internal/terminal"
)

// deadSession never answers keepalives.
type deadSession struct {
	*pipeSession
	mu    sync.Mutex
	tries int
}

func (d *deadSession) KeepAlive() error {
	d.mu.Lock()
	d.tries++
	d.mu.Unlock()
	return terminal.ErrNoReply
}

func TestKeepAliveClosesDeadConnection(t *testing.T) {
	type closedEvent struct {
		msg  string
		lost bool
	}
	closed := make(chan closedEvent, 1)
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "term:closed" {
			closed <- closedEvent{data[1].(string), data[2].(bool)}
		}
	}
	sess := &deadSession{pipeSession: newPipeSession()}
	tb := a.getTab(1)
	tb.sess = sess
	go tb.pump(sess)
	go tb.keepAlive(sess, 20*time.Millisecond)

	select {
	case ev := <-closed:
		if !ev.lost || !strings.Contains(ev.msg, "응답하지 않아") {
			t.Fatalf("closed event %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dead connection not closed")
	}
	sess.mu.Lock()
	tries := sess.tries
	sess.mu.Unlock()
	if tries != keepAliveMisses {
		t.Fatalf("closed after %d keepalives, want %d", tries, keepAliveMisses)
	}
	if tb.session() != nil {
		t.Fatal("tab still has the session")
	}
}

func TestRemoteCloseIsNotLost(t *testing.T) {
	lost := make(chan bool, 1)
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "term:closed" {
			lost <- data[2].(bool)
		}
	}
	sess := newPipeSession()
	tb := a.getTab(1)
	tb.sess = sess
	go tb.pump(sess)
	sess.out.Close() // EOF: the server ended the session
	if <-lost {
		t.Fatal("a normal end was reported as lost")
	}
}
