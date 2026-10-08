package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// keyServer starts a server that accepts only the public key want (and any
// password if passwords is set). It returns the address and the host key.
func keyServer(t *testing.T, want ssh.PublicKey, passwords bool) (string, ssh.Signer) {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if want != nil && bytes.Equal(k.Marshal(), want.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	if passwords {
		cfg.PasswordCallback = func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }
	}
	host := ed25519Signer(t)
	return serveShell(t, cfg, host), host
}

// useHome points the user profile at a temp dir with known_hosts trusting
// host for addr, no agents, and no cached keys. It returns the .ssh folder.
func useHome(t *testing.T, addr string, host ssh.Signer) string {
	home := filepath.Dir(useKnownHosts(t, addr, host.PublicKey()))
	orig := agentSources
	agentSources = func() []agentSource { return nil }
	t.Cleanup(func() { agentSources = orig })
	unlockedKeys.Lock()
	clear(unlockedKeys.m)
	unlockedKeys.Unlock()
	return home
}

// writeKey saves priv to dir/name, encrypted with pass unless it is empty.
func writeKey(t *testing.T, dir, name string, priv interface{}, pass string) string {
	var block *pem.Block
	var err error
	if pass == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(pass))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func dialAsk(t *testing.T, addr, pass string, ask asker, keys ...string) (Session, error) {
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	return dialSSH(ConnectRequest{Host: host, Port: port, Login: "u", Pass: pass, Cols: 80, Rows: 24},
		func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }, ask, nil, keys)
}

// countingAsker answers with the given values in turn and counts the prompts.
func countingAsker(answers ...string) (asker, *int) {
	n := 0
	return func(title, msg string, fields []PromptField) ([]string, bool) {
		n++
		if n > len(answers) || answers[n-1] == "" {
			return nil, false
		}
		return []string{answers[n-1]}, true
	}, &n
}

func TestEncryptedKeyAskedWhenAccepted(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	addr, host := keyServer(t, sshPub, false)
	dir := useHome(t, addr, host)
	writeKey(t, dir, "id_ed25519", priv, "secret")

	// A wrong passphrase is asked again.
	ask, n := countingAsker("wrong", "secret")
	sess, err := dialAsk(t, addr, "", ask)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sess.Close()
	if *n != 2 {
		t.Fatalf("asked %d times, want 2", *n)
	}

	// The unlocked key is remembered: no prompt the next time.
	ask, n = countingAsker()
	sess, err = dialAsk(t, addr, "", ask)
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	sess.Close()
	if *n != 0 {
		t.Fatalf("asked %d times on reconnect", *n)
	}
}

func TestEncryptedKeyNotAskedWhenRejected(t *testing.T) {
	addr, host := keyServer(t, nil, true) // takes no keys, any password
	dir := useHome(t, addr, host)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	writeKey(t, dir, "id_ed25519", priv, "secret")

	ask, n := countingAsker()
	sess, err := dialAsk(t, addr, "pw", ask)
	if err != nil {
		t.Fatalf("connect with password: %v", err)
	}
	sess.Close()
	if *n != 0 {
		t.Fatalf("asked for the passphrase of a key the server rejects")
	}
}

func TestEncryptedKeyCancelled(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	addr, host := keyServer(t, sshPub, false)
	dir := useHome(t, addr, host)
	writeKey(t, dir, "id_ed25519", priv, "secret")

	ask, _ := countingAsker("")
	if _, err := dialAsk(t, addr, "", ask); err == nil || !strings.Contains(err.Error(), "취소") {
		t.Fatalf("err = %v", err)
	}
}

// RSA keys must sign with SHA-2 (rsa-sha2-*), which the lazy key passes through.
func TestEncryptedRSAKeyFromIdentityFile(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, _ := ssh.NewPublicKey(&priv.PublicKey)
	addr, host := keyServer(t, sshPub, false)
	dir := useHome(t, addr, host)
	path := writeKey(t, dir, "work_rsa", priv, "secret")

	ask, n := countingAsker("secret")
	sess, err := dialAsk(t, addr, "", ask, path)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sess.Close()
	if *n != 1 {
		t.Fatalf("asked %d times", *n)
	}
}

func TestAgentKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	addr, host := keyServer(t, signer.PublicKey(), false)
	useHome(t, addr, host)

	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	served := 0
	agentSources = func() []agentSource {
		return []agentSource{{"test", func() (io.ReadWriteCloser, error) {
			c1, c2 := net.Pipe()
			served++
			go agent.ServeAgent(keyring, c2)
			return c1, nil
		}}}
	}

	ask, n := countingAsker()
	sess, err := dialAsk(t, addr, "", ask)
	if err != nil {
		t.Fatalf("connect with agent key: %v", err)
	}
	sess.Close()
	if served != 1 || *n != 0 {
		t.Fatalf("agent used %d times, %d prompts", served, *n)
	}
}

// An unreachable agent is skipped.
func TestAgentMissing(t *testing.T) {
	addr, host := keyServer(t, nil, true)
	useHome(t, addr, host)
	agentSources = func() []agentSource {
		return []agentSource{{"none", func() (io.ReadWriteCloser, error) {
			return dialAgentPath(`\\.\pipe\choboterm-test-no-such-agent`)
		}}}
	}
	sess, err := dialAsk(t, addr, "pw", nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sess.Close()
}
