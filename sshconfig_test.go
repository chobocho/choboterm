package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kevinburke/ssh_config"
)

const testSSHConfig = `
Host pusan
    HostName 10.0.0.5
    User chobo
    Port 2222
    IdentityFile ~/.ssh/pusan_key

Host seoul seoul2
    HostName %h.example.com

Host *.corp
    User corpuser

Host *
    IdentityFile ~/.ssh/common
`

func parseTestConfig(t *testing.T) *ssh_config.Config {
	t.Helper()
	cfg, err := ssh_config.Decode(strings.NewReader(testSSHConfig))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestParseSSHCommand(t *testing.T) {
	cases := []struct {
		in, host, login string
		port            int
		ok              bool
	}{
		{"ssh pusan", "pusan", "", 0, true},
		{"ssh me@pusan", "pusan", "me", 0, true},
		{"ssh -p 2200 -l me pusan", "pusan", "me", 2200, true},
		{"ssh -p2200 pusan", "pusan", "", 2200, true},
		{"ssh -i key -o X=1 pusan -p 99", "pusan", "", 99, true},
		{"ssh.exe -v pusan uptime", "pusan", "", 0, true},
		{"SSH pusan", "pusan", "", 0, true},
		{"pusan", "", "", 0, false},
		{"ssh", "", "", 0, false},
		{"sshd pusan", "", "", 0, false},
	}
	for _, c := range cases {
		host, login, port, ok := parseSSHCommand(c.in)
		if host != c.host || login != c.login || port != c.port || ok != c.ok {
			t.Errorf("%q: got %q %q %d %v", c.in, host, login, port, ok)
		}
	}
}

func TestLookupSSHTarget(t *testing.T) {
	cfg := parseTestConfig(t)
	cases := []struct {
		in   string
		want SSHTarget
	}{
		{"pusan", SSHTarget{Host: "pusan", Port: 2222, Login: "chobo", HostName: "10.0.0.5", FromConfig: true}},
		{"ssh pusan", SSHTarget{Host: "pusan", Port: 2222, Login: "chobo", HostName: "10.0.0.5", FromConfig: true}},
		{"ssh -p 22 root@pusan", SSHTarget{Host: "pusan", Port: 22, Login: "root", HostName: "10.0.0.5", FromConfig: true}},
		{"seoul2", SSHTarget{Host: "seoul2", Port: 22, HostName: "seoul2.example.com", FromConfig: true}},
		// Not an alias: plain hosts stay untouched (they may be Telnet/FTP).
		{"db.corp", SSHTarget{Host: "db.corp"}},
		{"me@bbs.example.com", SSHTarget{Host: "bbs.example.com", Login: "me"}},
		// An ssh command line uses the wildcard entries too.
		{"ssh db.corp", SSHTarget{Host: "db.corp", Port: 22, Login: "corpuser"}},
	}
	for _, c := range cases {
		if got := lookupSSHTarget(cfg, c.in); got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
	if got := lookupSSHTarget(nil, "ssh host"); got != (SSHTarget{Host: "host", Port: 22}) {
		t.Errorf("no config: got %+v", got)
	}
}

func TestConfigAliases(t *testing.T) {
	got := configAliases(parseTestConfig(t))
	if want := []string{"pusan", "seoul", "seoul2"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSSHDialHostAndKeys(t *testing.T) {
	cfg := parseTestConfig(t)
	if got := sshDialHost(cfg, "pusan"); got != "10.0.0.5" {
		t.Errorf("dial host: %q", got)
	}
	if got := sshDialHost(cfg, "other"); got != "other" {
		t.Errorf("dial host: %q", got)
	}
	home, _ := os.UserHomeDir()
	want := []string{filepath.Join(home, ".ssh", "pusan_key"), filepath.Join(home, ".ssh", "common")}
	if got := sshIdentityFiles(cfg, "pusan", "chobo"); !slices.Equal(got, want) {
		t.Errorf("keys: got %v, want %v", got, want)
	}
}

func TestLoadSSHConfigFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte(testSSHConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	old := sshConfigPath
	defer func() { sshConfigPath = old }()

	sshConfigPath = func() string { return p }
	hosts := (&App{}).GetSSHConfigHosts()
	if len(hosts) != 3 || hosts[0] != (SSHConfigHost{Host: "pusan", HostName: "10.0.0.5", Port: 2222, Login: "chobo"}) {
		t.Errorf("hosts: %+v", hosts)
	}

	sshConfigPath = func() string { return filepath.Join(dir, "missing") }
	if hosts := (&App{}).GetSSHConfigHosts(); len(hosts) != 0 {
		t.Errorf("missing config: %+v", hosts)
	}
}

// "ssh alias" in the Host field connects to the alias's HostName/Port/User,
// while the tab and history keep the alias.
func TestConnectSSHConfigAlias(t *testing.T) {
	addr, key := startForwardingServer(t)
	useKnownHosts(t, addr, key)
	hist := filepath.Join(t.TempDir(), "hosts.json")
	origHist := historyFile
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = origHist }()
	useTempSettings(t)

	host, port, _ := strings.Cut(addr, ":")
	conf := filepath.Join(t.TempDir(), "config")
	os.WriteFile(conf, []byte("Host myserver\n  HostName "+host+"\n  Port "+port+"\n  User u\n"), 0o600)
	origConf := sshConfigPath
	sshConfigPath = func() string { return conf }
	defer func() { sshConfigPath = origConf }()

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.hooks.confirmKey = func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }
	// Port 23 is a stale value in the dialog; the command line decides.
	req := ConnectRequest{Host: "ssh myserver", Port: 23, Pass: "p", Cols: 80, Rows: 24}
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
