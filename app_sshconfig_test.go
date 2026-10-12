package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"choboterm/internal/sshconn"
	"choboterm/internal/sshconn/sshtest"
	"choboterm/internal/terminal"
)

func TestLoadSSHConfigFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte(sshtest.Config), 0o600); err != nil {
		t.Fatal(err)
	}
	old := sshconn.ConfigPath
	defer func() { sshconn.ConfigPath = old }()

	sshconn.ConfigPath = func() string { return p }
	hosts := (&App{}).GetSSHConfigHosts()
	if len(hosts) != 3 || hosts[0] != (sshconn.SSHConfigHost{Host: "busan", HostName: "10.0.0.5", Port: 2222, Login: "chobo"}) {
		t.Errorf("hosts: %+v", hosts)
	}

	sshconn.ConfigPath = func() string { return filepath.Join(dir, "missing") }
	if hosts := (&App{}).GetSSHConfigHosts(); len(hosts) != 0 {
		t.Errorf("missing config: %+v", hosts)
	}
}

// "ssh alias" in the Host field connects to the alias's HostName/Port/User,
// while the tab and history keep the alias.
func TestConnectSSHConfigAlias(t *testing.T) {
	addr, key := startForwardingServer(t)
	sshtest.UseKnownHosts(t, addr, key)
	hist := filepath.Join(t.TempDir(), "hosts.json")
	origHist := historyFile
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = origHist }()
	useTempSettings(t)

	host, port, _ := strings.Cut(addr, ":")
	conf := filepath.Join(t.TempDir(), "config")
	os.WriteFile(conf, []byte("Host myserver\n  HostName "+host+"\n  Port "+port+"\n  User u\n"), 0o600)
	origConf := sshconn.ConfigPath
	sshconn.ConfigPath = func() string { return conf }
	defer func() { sshconn.ConfigPath = origConf }()

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.hooks.confirmKey = func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }
	// Port 23 is a stale value in the dialog; the command line decides.
	req := terminal.ConnectRequest{Host: "ssh myserver", Port: 23, Pass: "p", Cols: 80, Rows: 24}
	if p, err := a.Connect(1, req); err != nil || p != "ssh" {
		t.Fatalf("connect: %q, %v", p, err)
	}
	defer a.CloseTab(1)
	if tb := a.getTab(1); tb.host != "myserver" {
		t.Errorf("tab host = %q", tb.host)
	}
	h := loadHistory()
	if len(h) == 0 || h[0].Host != "myserver" || h[0].Login != "u" {
		t.Errorf("history: %+v", h)
	}
}
