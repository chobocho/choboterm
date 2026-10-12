// Package testutil has what tests in several packages share: a test SFTP
// server, test data, and running commands in WSL.
package testutil

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	mrand "math/rand"
	"net"
	"os/exec"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// StartSFTPServer runs an in-process SSH server that only serves the sftp subsystem.
func StartSFTPServer(t *testing.T, root string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

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
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, chReqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						for req := range chReqs {
							ok := req.Type == "subsystem" && string(req.Payload[4:]) == "sftp"
							req.Reply(ok, nil)
							if ok {
								srv, _ := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
								_ = srv.Serve()
								ch.Close()
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func Payload(n int, seed int64) []byte {
	b := make([]byte, n)
	mrand.New(mrand.NewSource(seed)).Read(b)
	return b
}

func WSLRun(t *testing.T, script string, stdin []byte) []byte {
	t.Helper()
	cmd := exec.Command("wsl.exe", "--", "sh", "-c", script)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: %v", script, err)
	}
	return out
}
