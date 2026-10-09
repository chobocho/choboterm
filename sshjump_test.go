package main

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestSSHCommandJump(t *testing.T) {
	for in, want := range map[string]string{
		"ssh -J bastion web":            "bastion",
		"ssh -Jme@b1,b2:2222 web":       "me@b1,b2:2222",
		"ssh -p 2222 -J bastion me@web": "bastion",
		"ssh web -J bastion":            "", // after the host: part of the remote command
		"ssh web":                       "",
		"web":                           "",
	} {
		if got := sshCommandJump(in); got != want {
			t.Errorf("sshCommandJump(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseJumps(t *testing.T) {
	cfg, err := ssh_config.Decode(strings.NewReader(`
Host bastion
    HostName 10.0.0.1
    User jumper
    Port 2200
    ProxyJump none

Host web
    ProxyJump bastion
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := proxyJumpFor(cfg, "web", ""); got != "bastion" {
		t.Errorf("ProxyJump of web = %q", got)
	}
	if got := proxyJumpFor(cfg, "web", "other"); got != "other" {
		t.Errorf("-J should win over the config: %q", got)
	}
	if got := proxyJumpFor(cfg, "bastion", ""); got != "" {
		t.Errorf("ProxyJump none = %q", got)
	}
	hops, err := parseJumps(cfg, "bastion, me@10.0.0.2:2222,[::1]", "u")
	if err != nil {
		t.Fatal(err)
	}
	want := []jumpHost{
		{name: "bastion", addr: "10.0.0.1:2200", login: "jumper"},
		{name: "10.0.0.2", addr: "10.0.0.2:2222", login: "me"},
		{name: "::1", addr: "[::1]:22", login: "u"},
	}
	if len(hops) != len(want) {
		t.Fatalf("hops = %+v", hops)
	}
	for i, h := range hops {
		if h.name != want[i].name || h.addr != want[i].addr || h.login != want[i].login {
			t.Errorf("hop %d = %+v, want %+v", i, h, want[i])
		}
	}
	if _, err := parseJumps(cfg, "b:99999", "u"); err == nil {
		t.Error("a bad port was accepted")
	}
}

// startJumpServer runs an SSH server that accepts any password and forwards
// direct-tcpip channels (what a jump host does for ssh -J). It counts them.
func startJumpServer(t *testing.T, key ssh.Signer) (string, *int32) {
	t.Helper()
	var forwarded int32
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
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					if nch.ChannelType() != "direct-tcpip" {
						nch.Reject(ssh.UnknownChannelType, "")
						continue
					}
					// host string, port uint32, origin host string, origin port uint32
					p := nch.ExtraData()
					n := binary.BigEndian.Uint32(p)
					host := string(p[4 : 4+n])
					port := binary.BigEndian.Uint32(p[4+n:])
					target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(port))))
					if err != nil {
						nch.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					ch, chReqs, err := nch.Accept()
					if err != nil {
						target.Close()
						continue
					}
					atomic.AddInt32(&forwarded, 1)
					go ssh.DiscardRequests(chReqs)
					go func() { io.Copy(ch, target); ch.CloseWrite() }()
					go func() { io.Copy(target, ch); target.Close() }()
				}
			}()
		}
	}()
	return ln.Addr().String(), &forwarded
}

func TestDialThroughJumpHosts(t *testing.T) {
	jumpKey, jump2Key, webKey := ed25519Signer(t), ed25519Signer(t), ed25519Signer(t)
	jump, n1 := startJumpServer(t, jumpKey)
	jump2, n2 := startJumpServer(t, jump2Key)
	web := startShellServer(t, webKey)

	// known_hosts trusts all three.
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".ssh"), 0o700)
	var lines string
	for addr, k := range map[string]ssh.Signer{jump: jumpKey, jump2: jump2Key, web: webKey} {
		lines += knownhosts.Line([]string{knownhosts.Normalize(addr)}, k.PublicKey()) + "\n"
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}

	// Jump hosts get no password from the dialog: their prompts name the hop.
	var titles []string
	ask := func(title, msg string, fields []PromptField) ([]string, bool) {
		titles = append(titles, title)
		return []string{"p"}, true
	}
	noPrompt := func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }

	hops, err := parseJumps(nil, "j1@"+jump+",j2@"+jump2, "u")
	if err != nil {
		t.Fatal(err)
	}
	clients, conn, err := dialJumps(hops, web, noPrompt, ask)
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, _ := net.SplitHostPort(web)
	port, _ := strconv.Atoi(portStr)
	sess, err := dialSSH(ConnectRequest{Host: host, Port: port, Login: "u", Pass: "p", Cols: 80, Rows: 24}, noPrompt, ask, conn, nil)
	if err != nil {
		closeClients(clients)
		t.Fatal(err)
	}
	sess.(*sshSession).jumps = clients
	buf := make([]byte, 2)
	if _, err := io.ReadFull(sess, buf); err != nil || string(buf) != "$ " {
		t.Fatalf("shell output %q, %v", buf, err)
	}
	sess.Close()

	if atomic.LoadInt32(n1) != 1 || atomic.LoadInt32(n2) != 1 {
		t.Errorf("forwarded channels: jump1 %d, jump2 %d", *n1, *n2)
	}
	if len(titles) != 2 || !strings.Contains(titles[0], "j1@") || !strings.Contains(titles[1], "j2@") {
		t.Errorf("prompt titles = %q", titles)
	}

	// A target the last jump host can't reach is reported, and nothing stays open.
	if _, _, err := dialJumps(hops[:1], "127.0.0.1:1", noPrompt, ask); err == nil || !strings.Contains(err.Error(), "점프 호스트") {
		t.Errorf("unreachable target: %v", err)
	}
}
