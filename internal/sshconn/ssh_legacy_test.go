package sshconn

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"choboterm/internal/sshconn/sshtest"
)

// startOldServer runs a shell server that only speaks what old switches and
// routers do: diffie-hellman-group1-sha1 and aes128-cbc.
func startOldServer(t *testing.T, kex, cipher string) (string, ssh.PublicKey) {
	t.Helper()
	key := sshtest.Ed25519Signer(t)
	addr := serveShell(t, &ssh.ServerConfig{
		Config:           ssh.Config{KeyExchanges: []string{kex}, Ciphers: []string{cipher}},
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}, key)
	return addr, key.PublicKey()
}

func TestLegacyAlgorithmsOnlyWhenAllowed(t *testing.T) {
	for _, c := range []struct{ kex, cipher, what, algo string }{
		{ssh.InsecureKeyExchangeDH1SHA1, ssh.CipherAES128CTR, "key exchange", ssh.InsecureKeyExchangeDH1SHA1},
		{ssh.KeyExchangeCurve25519, ssh.InsecureCipherAES128CBC, "client to server cipher", ssh.InsecureCipherAES128CBC},
		{ssh.KeyExchangeCurve25519, ssh.InsecureCipherTripleDESCBC, "client to server cipher", ssh.InsecureCipherTripleDESCBC},
	} {
		addr, key := startOldServer(t, c.kex, c.cipher)
		sshtest.UseKnownHosts(t, addr, key)
		host, port := splitAddr(addr)
		var asked []string
		LegacyAllowed = func(h string, p int) bool {
			asked = append(asked, h)
			return false
		}
		_, err := dialTestSSH(t, addr)
		var le *LegacyError
		if !errors.As(err, &le) || le.Host != host || le.Port != port || le.What != c.what || !slices.Equal(le.Algos, []string{c.algo}) {
			t.Fatalf("%s/%s: %#v (%v)", c.kex, c.cipher, le, err)
		}
		if !strings.Contains(err.Error(), "안전하지 않은 옛 알고리즘만 지원합니다 ("+c.algo+")") || len(asked) != 1 || asked[0] != host {
			t.Fatalf("%v, asked %q", err, asked)
		}

		// Allowed for the server: it connects.
		LegacyAllowed = func(h string, p int) bool { return h == host && p == port }
		sess, err := dialTestSSH(t, addr)
		if err != nil {
			t.Fatalf("%s/%s allowed: %v", c.kex, c.cipher, err)
		}
		sess.Close()
	}
	LegacyAllowed = nil
}

func TestLegacyNotOfferedForOtherFailures(t *testing.T) {
	// The server wants a key exchange we don't offer by default but that is
	// not an old one: allowing old algorithms would not help, so no question.
	addr, key := startOldServer(t, ssh.KeyExchangeDH16SHA512, ssh.CipherAES128CTR)
	sshtest.UseKnownHosts(t, addr, key)
	_, err := dialTestSSH(t, addr)
	var le *LegacyError
	if err == nil || errors.As(err, &le) || !strings.Contains(err.Error(), "no common algorithm") {
		t.Fatalf("err = %v", err)
	}
}

func TestLegacyStillPrefersGoodAlgorithms(t *testing.T) {
	// Allowed but not needed: a modern server still gets modern algorithms.
	key := sshtest.Ed25519Signer(t)
	addr := startShellServer(t, key)
	sshtest.UseKnownHosts(t, addr, key.PublicKey())
	LegacyAllowed = func(string, int) bool { return true }
	defer func() { LegacyAllowed = nil }()
	cfg := legacyConfig()
	if cfg.KeyExchanges[0] == ssh.InsecureKeyExchangeDH1SHA1 || cfg.Ciphers[0] == ssh.InsecureCipherAES128CBC {
		t.Fatalf("old algorithms first: %v %v", cfg.KeyExchanges, cfg.Ciphers)
	}
	sess, err := dialTestSSH(t, addr)
	if err != nil {
		t.Fatal(err)
	}
	sess.Close()
}
