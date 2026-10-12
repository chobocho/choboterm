// Package sshtest has what tests in several packages need to talk to a test
// SSH server.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func Ed25519Signer(t *testing.T) ssh.Signer {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// UseKnownHosts points the user profile at a temp dir whose known_hosts
// trusts key for addr.
func UseKnownHosts(t *testing.T, addr string, key ssh.PublicKey) string {
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

const Config = `
Host busan
    HostName 10.0.0.5
    User chobo
    Port 2222
    IdentityFile ~/.ssh/busan_key

Host seoul seoul2
    HostName %h.example.com

Host *.corp
    User corpuser

Host *
    IdentityFile ~/.ssh/common
`
