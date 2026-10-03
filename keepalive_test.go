package main

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startSSHServer runs an SSH server that accepts any password and grants
// pty/shell requests. shell runs for each shell with its channel and raw
// connection; replyGlobal decides whether global requests (keepalives) get
// an answer.
func startSSHServer(t *testing.T, replyGlobal bool, shell func(ch ssh.Channel, nc net.Conn)) (string, ssh.PublicKey) {
	t.Helper()
	key := ed25519Signer(t)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
	cfg.AddHostKey(key)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go func() {
					for r := range reqs {
						if replyGlobal {
							r.Reply(false, nil) // like OpenSSH for unknown requests
						}
					}
				}()
				for nch := range chans {
					ch, chReqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						for req := range chReqs {
							req.Reply(req.Type == "pty-req" || req.Type == "shell", nil)
							if req.Type == "shell" {
								go shell(ch, nc)
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String(), key.PublicKey()
}

// readAll reads a session until it ends and returns the output and final error.
func readAll(s Session) (string, error) {
	var out strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := s.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			return out.String(), err
		}
	}
}

func TestSSHShellExitIsNotConnectionLost(t *testing.T) {
	addr, key := startSSHServer(t, true, func(ch ssh.Channel, _ net.Conn) {
		ch.Write([]byte("bye"))
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		ch.Close()
	})
	useKnownHosts(t, addr, key)
	sess, err := dialTestSSH(t, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	out, err := readAll(sess)
	if out != "bye" || err != io.EOF {
		t.Fatalf("out %q, err %v; want bye, EOF", out, err)
	}
}

func TestSSHDroppedConnectionIsLost(t *testing.T) {
	addr, key := startSSHServer(t, true, func(ch ssh.Channel, nc net.Conn) {
		ch.Write([]byte("hi"))
		time.Sleep(100 * time.Millisecond)
		nc.Close() // the network goes away without an exit status
	})
	useKnownHosts(t, addr, key)
	sess, err := dialTestSSH(t, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if _, err := readAll(sess); !errors.Is(err, errConnLost) {
		t.Fatalf("err = %v, want errConnLost", err)
	}
}

func TestSSHKeepAlive(t *testing.T) {
	orig := keepAliveTimeout
	keepAliveTimeout = 300 * time.Millisecond
	defer func() { keepAliveTimeout = orig }()

	idle := func(ssh.Channel, net.Conn) {}
	for _, reply := range []bool{true, false} {
		addr, key := startSSHServer(t, reply, idle)
		useKnownHosts(t, addr, key)
		sess, err := dialTestSSH(t, addr)
		if err != nil {
			t.Fatal(err)
		}
		err = sess.(keepAliver).keepAlive()
		sess.Close()
		if reply && err != nil {
			t.Fatalf("answered keepalive: %v", err)
		}
		if !reply && !errors.Is(err, errNoReply) {
			t.Fatalf("unanswered keepalive: %v, want errNoReply", err)
		}
	}
}

func TestTelnetKeepAliveSendsNOP(t *testing.T) {
	sess, remote := newPipeTelnet("", "")
	defer remote.Close()
	go func() {
		if err := sess.keepAlive(); err != nil {
			t.Error(err)
		}
	}()
	if got := readN(t, remote, 2); got[0] != tnIAC || got[1] != tnNOP {
		t.Fatalf("got % X, want IAC NOP", got)
	}
}

// deadSession never answers keepalives.
type deadSession struct {
	*pipeSession
	mu    sync.Mutex
	tries int
}

func (d *deadSession) keepAlive() error {
	d.mu.Lock()
	d.tries++
	d.mu.Unlock()
	return errNoReply
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
