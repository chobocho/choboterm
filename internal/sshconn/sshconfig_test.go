package sshconn

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kevinburke/ssh_config"

	"choboterm/internal/sshconn/sshtest"
)

func parseTestConfig(t *testing.T) *ssh_config.Config {
	t.Helper()
	cfg, err := ssh_config.Decode(strings.NewReader(sshtest.Config))
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
		{"ssh busan", "busan", "", 0, true},
		{"ssh me@busan", "busan", "me", 0, true},
		{"ssh -p 2200 -l me busan", "busan", "me", 2200, true},
		{"ssh -p2200 busan", "busan", "", 2200, true},
		{"ssh -i key -o X=1 busan -p 99", "busan", "", 99, true},
		{"ssh.exe -v busan uptime", "busan", "", 0, true},
		{"SSH busan", "busan", "", 0, true},
		{"busan", "", "", 0, false},
		{"ssh", "", "", 0, false},
		{"sshd busan", "", "", 0, false},
	}
	for _, c := range cases {
		host, login, port, ok := ParseCommand(c.in)
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
		{"busan", SSHTarget{Host: "busan", Port: 2222, Login: "chobo", HostName: "10.0.0.5", FromConfig: true}},
		{"ssh busan", SSHTarget{Host: "busan", Port: 2222, Login: "chobo", HostName: "10.0.0.5", FromConfig: true}},
		{"ssh -p 22 root@busan", SSHTarget{Host: "busan", Port: 22, Login: "root", HostName: "10.0.0.5", FromConfig: true}},
		{"seoul2", SSHTarget{Host: "seoul2", Port: 22, HostName: "seoul2.example.com", FromConfig: true}},
		// Not an alias: plain hosts stay untouched (they may be Telnet/FTP).
		{"db.corp", SSHTarget{Host: "db.corp"}},
		{"me@bbs.example.com", SSHTarget{Host: "bbs.example.com", Login: "me"}},
		// An ssh command line uses the wildcard entries too.
		{"ssh db.corp", SSHTarget{Host: "db.corp", Port: 22, Login: "corpuser"}},
	}
	for _, c := range cases {
		if got := LookupTarget(cfg, c.in); got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
	if got := LookupTarget(nil, "ssh host"); got != (SSHTarget{Host: "host", Port: 22}) {
		t.Errorf("no config: got %+v", got)
	}
}

func TestConfigAliases(t *testing.T) {
	got := ConfigAliases(parseTestConfig(t))
	if want := []string{"busan", "seoul", "seoul2"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSSHDialHostAndKeys(t *testing.T) {
	cfg := parseTestConfig(t)
	if got := DialHost(cfg, "busan"); got != "10.0.0.5" {
		t.Errorf("dial host: %q", got)
	}
	if got := DialHost(cfg, "other"); got != "other" {
		t.Errorf("dial host: %q", got)
	}
	home, _ := os.UserHomeDir()
	want := []string{filepath.Join(home, ".ssh", "busan_key"), filepath.Join(home, ".ssh", "common")}
	if got := IdentityFiles(cfg, "busan", "chobo"); !slices.Equal(got, want) {
		t.Errorf("keys: got %v, want %v", got, want)
	}
}
