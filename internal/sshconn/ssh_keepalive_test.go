package sshconn

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"choboterm/internal/sshconn/sshtest"
	"choboterm/internal/terminal"
)

// startSSHServer runs an SSH server that accepts any password and grants
// pty/shell requests. shell runs for each shell with its channel and raw
// connection; replyGlobal decides whether global requests (keepalives) get
// an answer.
func startSSHServer(t *testing.T, replyGlobal bool, shell func(ch ssh.Channel, nc net.Conn)) (string, ssh.PublicKey) {
	t.Helper()
	key := sshtest.Ed25519Signer(t)
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
func readAll(s terminal.Session) (string, error) {
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
	sshtest.UseKnownHosts(t, addr, key)
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
	sshtest.UseKnownHosts(t, addr, key)
	sess, err := dialTestSSH(t, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if _, err := readAll(sess); !errors.Is(err, terminal.ErrLost) {
		t.Fatalf("err = %v, want errConnLost", err)
	}
}

func TestSSHKeepAlive(t *testing.T) {
	orig := terminal.KeepAliveTimeout
	terminal.KeepAliveTimeout = 300 * time.Millisecond
	defer func() { terminal.KeepAliveTimeout = orig }()

	idle := func(ssh.Channel, net.Conn) {}
	for _, reply := range []bool{true, false} {
		addr, key := startSSHServer(t, reply, idle)
		sshtest.UseKnownHosts(t, addr, key)
		sess, err := dialTestSSH(t, addr)
		if err != nil {
			t.Fatal(err)
		}
		err = sess.(interface{ KeepAlive() error }).KeepAlive()
		sess.Close()
		if reply && err != nil {
			t.Fatalf("answered keepalive: %v", err)
		}
		if !reply && !errors.Is(err, terminal.ErrNoReply) {
			t.Fatalf("unanswered keepalive: %v, want errNoReply", err)
		}
	}
}
