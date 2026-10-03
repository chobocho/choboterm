package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// startShellServer runs an SSH server with the given host keys that accepts
// any password and grants pty/shell requests.
func startShellServer(t *testing.T, hostKeys ...ssh.Signer) string {
	t.Helper()
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
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

func ed25519Signer(t *testing.T) ssh.Signer {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ecdsaSigner(t *testing.T) ssh.Signer {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// useKnownHosts points the user profile at a temp dir whose known_hosts
// trusts key for addr.
func useKnownHosts(t *testing.T, addr string, key ssh.PublicKey) string {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".ssh"), 0o700)
	path := filepath.Join(home, ".ssh", "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func dialTestSSH(t *testing.T, addr string) (Session, error) {
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	return dialSSH(ConnectRequest{Host: host, Port: port, Login: "u", Pass: "p", Cols: 80, Rows: 24},
		func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }, nil)
}

// The server has ECDSA and Ed25519 keys; known_hosts only has the Ed25519 one
// (e.g. written by OpenSSH). The Go library prefers ECDSA, which used to be
// reported as a changed key.
func TestKnownHostsOtherKeyType(t *testing.T) {
	ed := ed25519Signer(t)
	addr := startShellServer(t, ecdsaSigner(t), ed)
	useKnownHosts(t, addr, ed.PublicKey())

	sess, err := dialTestSSH(t, addr)
	if err != nil {
		t.Fatalf("connect with known Ed25519 key: %v", err)
	}
	sess.Close()
}

func TestKnownHostsChangedKey(t *testing.T) {
	addr := startShellServer(t, ed25519Signer(t))
	path := useKnownHosts(t, addr, ed25519Signer(t).PublicKey()) // a different key

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
