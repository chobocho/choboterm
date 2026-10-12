package sshconn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"choboterm/internal/sshconn/sshtest"
	"choboterm/internal/terminal"
)

// startShellServer runs an SSH server with the given host keys that accepts
// any password and grants pty/shell requests.
func startShellServer(t *testing.T, hostKeys ...ssh.Signer) string {
	t.Helper()
	return serveShell(t, &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}, hostKeys...)
}

// serveShell runs an SSH server with the given auth config and host keys
// that grants pty/shell requests.
func serveShell(t *testing.T, cfg *ssh.ServerConfig, hostKeys ...ssh.Signer) string {
	t.Helper()
	for _, k := range hostKeys {
		cfg.AddHostKey(k)
	}
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
							req.Reply(req.Type == "pty-req" || req.Type == "shell", nil)
							if req.Type == "shell" {
								ch.Write([]byte("$ "))
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func ecdsaSigner(t *testing.T) ssh.Signer {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func dialTestSSH(t *testing.T, addr string) (terminal.Session, error) {
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	return Dial(terminal.ConnectRequest{Host: host, Port: port, Login: "u", Pass: "p", Cols: 80, Rows: 24},
		func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }, nil, nil, nil)
}

// The server has ECDSA and Ed25519 keys; known_hosts only has the Ed25519 one
// (e.g. written by OpenSSH). The Go library prefers ECDSA, which used to be
// reported as a changed key.
func TestKnownHostsOtherKeyType(t *testing.T) {
	ed := sshtest.Ed25519Signer(t)
	addr := startShellServer(t, ecdsaSigner(t), ed)
	sshtest.UseKnownHosts(t, addr, ed.PublicKey())

	sess, err := dialTestSSH(t, addr)
	if err != nil {
		t.Fatalf("connect with known Ed25519 key: %v", err)
	}
	sess.Close()
}

func TestKnownHostsChangedKey(t *testing.T) {
	addr := startShellServer(t, sshtest.Ed25519Signer(t))
	path := sshtest.UseKnownHosts(t, addr, sshtest.Ed25519Signer(t).PublicKey()) // a different key

	_, err := dialTestSSH(t, addr)
	if err == nil {
		t.Fatal("expected a changed-key error")
	}
	if !strings.Contains(err.Error(), "다릅니다") || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "1번째 줄") {
		t.Fatalf("error = %v", err)
	}
}

func TestKnownHostsRSAEntry(t *testing.T) {
	// An "ssh-rsa" known_hosts entry must allow the SHA-2 RSA signatures servers use today.
	algos := (&knownHosts{lookup: func(string, net.Addr, ssh.PublicKey) error {
		return &knownhosts.KeyError{Want: []knownhosts.KnownKey{{Key: rsaPublicKey(t)}}}
	}}).algorithmsFor("h:22", nil)
	want := []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	if strings.Join(algos, ",") != strings.Join(want, ",") {
		t.Fatalf("algos = %v", algos)
	}
}

func rsaPublicKey(t *testing.T) ssh.PublicKey {
	// A fixed 1024-bit test key is enough to get an "ssh-rsa" PublicKey.
	const authorized = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQC6eNMYo4Fqfk0kGY5Jb0L1s6UYCN4ahRBgDhh9Q0RZ7rC+X7eqTDrMfmyM+OqRqb2rS6X8dHMK6bIFQHqYUJLnCZPfKA3nB1+dTVn1gH7mI5gXEHtFhqCCpK5RNh4Kz0yJQ9JdSW2vELC3BSbJxK7jRdyNGA4Fbu3Ts9JZTPOS6Q=="
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorized))
	if err != nil {
		t.Fatal(err)
	}
	return k
}
