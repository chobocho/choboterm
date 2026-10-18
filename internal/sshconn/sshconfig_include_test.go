package sshconn

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeConfigTree writes files under dir (slash paths) and points
// ConfigPath at dir/config and the home folder at home.
func writeConfigTree(t *testing.T, dir, home string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	old := ConfigPath
	ConfigPath = func() string { return filepath.Join(dir, "config") }
	t.Cleanup(func() { ConfigPath = old })
}

func TestConfigInclude(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	writeConfigTree(t, dir, home, map[string]string{
		"config": `
Include config.d/*
Include ~/extra.conf

Host busan
    HostName 10.0.0.5
    Include busan.d/port
    User chobo

Match host *.corp,*.lab
    User corpuser

Match all
    IdentityFile ~/.ssh/common
`,
		"config.d/10-work": `
Host work
    HostName work.example.com
    User worker
`,
		"config.d/20-lab": `
Host lab
    HostName 10.1.0.1
`,
		"config.d/30-loop": "Include config.d/30-loop\n",
		// a Host in a file included inside "Host busan" must not take the
		// rest of busan's block
		"busan.d/port": `
Port 2222
Host leak
    HostName leak.example.com
`,
		"../extra.conf": `
Host extra
    HostName extra.example.com
`,
	})
	cfg := LoadConfig()
	if cfg == nil {
		t.Fatal("no config")
	}
	if got, want := ConfigAliases(cfg), []string{"work", "lab", "extra", "busan", "leak"}; !slices.Equal(got, want) {
		t.Errorf("aliases %v, want %v", got, want)
	}
	for in, want := range map[string]SSHTarget{
		"work":      {Host: "work", Port: 22, Login: "worker", HostName: "work.example.com", FromConfig: true},
		"extra":     {Host: "extra", Port: 22, HostName: "extra.example.com", FromConfig: true},
		"busan":     {Host: "busan", Port: 2222, Login: "chobo", HostName: "10.0.0.5", FromConfig: true},
		"leak":      {Host: "leak", Port: 22, HostName: "leak.example.com", FromConfig: true},
		"ssh x.lab": {Host: "x.lab", Port: 22, Login: "corpuser"},
	} {
		if got := LookupTarget(cfg, in); got != want {
			t.Errorf("%q: got %+v, want %+v", in, got, want)
		}
	}
	if got := IdentityFiles(cfg, "work", "worker"); !slices.Contains(got, filepath.Join(home, ".ssh", "common")) {
		t.Errorf("Match all IdentityFile: %v", got)
	}
}

func TestConfigUnsupportedMatchKeepsTheRest(t *testing.T) {
	// One Match the parser can't do used to make the whole file unreadable,
	// so every name in it was lost. Such a block is skipped instead.
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	writeConfigTree(t, dir, home, map[string]string{
		"config": `
Host busan
    HostName 10.0.0.5
    User chobo

Match exec "test -f /tmp/vpn"
    User nobody
    Port 2200

Match host db user root
    Port 2201

Include more.conf
`,
		"more.conf": `
Match final
    User other

Host seoul
    HostName seoul.example.com
`,
	})
	cfg := LoadConfig()
	if cfg == nil {
		t.Fatal("no config")
	}
	if got, want := ConfigAliases(cfg), []string{"busan", "seoul"}; !slices.Equal(got, want) {
		t.Errorf("aliases %v, want %v", got, want)
	}
	for in, want := range map[string]SSHTarget{
		"busan":       {Host: "busan", Port: 22, Login: "chobo", HostName: "10.0.0.5", FromConfig: true},
		"seoul":       {Host: "seoul", Port: 22, HostName: "seoul.example.com", FromConfig: true},
		"ssh db":      {Host: "db", Port: 22},
		"ssh plain.x": {Host: "plain.x", Port: 22},
	} {
		if got := LookupTarget(cfg, in); got != want {
			t.Errorf("%q: got %+v, want %+v", in, got, want)
		}
	}
}
