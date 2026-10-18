package app

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"choboterm/internal/config"
	"choboterm/internal/sshconn"
	"choboterm/internal/sshconn/sshtest"
	"choboterm/internal/store"
	"choboterm/internal/terminal"
)

func TestConnectAsksForLegacySSH(t *testing.T) {
	// An old switch: only diffie-hellman-group1-sha1 and aes128-cbc.
	key := sshtest.Ed25519Signer(t)
	cfg := &ssh.ServerConfig{
		Config: ssh.Config{
			KeyExchanges: []string{ssh.InsecureKeyExchangeDH1SHA1},
			Ciphers:      []string{ssh.InsecureCipherAES128CBC},
		},
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
			go serveForwarding(nc, cfg)
		}
	}()
	addr := ln.Addr().String()
	sshtest.UseKnownHosts(t, addr, key.PublicKey())
	useTempHosts(t)
	useTempSettings(t)
	// The question is checked in Korean; CI machines would show it in English.
	if err := config.Update(func(s *config.Settings) { s.Language = "ko" }); err != nil {
		t.Fatal(err)
	}

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.hooks.confirmKey = func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }
	var asked []Prompt
	answer := false
	a.hooks.prompt = func(p Prompt) ([]string, bool) {
		asked = append(asked, p)
		return nil, answer
	}
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	req := terminal.ConnectRequest{Host: host, Port: port, Login: "u", Pass: "p", Cols: 80, Rows: 24}

	// No: the connection fails and nothing is remembered.
	_, err = a.Connect(1, req)
	var le *sshconn.LegacyError
	if !errors.As(err, &le) || len(asked) != 1 {
		t.Fatalf("refused: %v, asked %d", err, len(asked))
	}
	if !strings.Contains(asked[0].Message, addr+" 서버는 안전하지 않은 옛 알고리즘만 지원합니다.") ||
		!strings.Contains(asked[0].Message, "키 교환: diffie-hellman-group1-sha1") {
		t.Fatalf("question: %q", asked[0].Message)
	}
	if store.PrefsFor(host, port).LegacySSH {
		t.Fatal("remembered a no")
	}

	// Yes: connected at once, and remembered for the server.
	answer = true
	if p, err := a.Connect(1, req); err != nil || p != "ssh" || len(asked) != 2 {
		t.Fatalf("allowed: %q, %v, asked %d", p, err, len(asked))
	}
	a.CloseTab(1)
	if !store.PrefsFor(host, port).LegacySSH {
		t.Fatal("not remembered")
	}

	// Next time it isn't asked again.
	if p, err := a.Connect(2, req); err != nil || p != "ssh" || len(asked) != 2 {
		t.Fatalf("again: %q, %v, asked %d", p, err, len(asked))
	}
	a.CloseTab(2)
}
