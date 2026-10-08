package main

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// SSHTarget is what the Host field means: a name from ~/.ssh/config
// ("busan") or an ssh command line ("ssh -p 2222 me@busan").
type SSHTarget struct {
	Host       string `json:"host"`       // name to keep in the Host field (the alias, not its HostName)
	Port       int    `json:"port"`       // 0: leave the Port field as it is
	Login      string `json:"login"`      // "": leave the Login field as it is
	HostName   string `json:"hostName"`   // address the alias stands for, "" when not from the config
	FromConfig bool   `json:"fromConfig"` // the name is a Host entry of ~/.ssh/config
}

// sshConfigPath is ~/.ssh/config (replaceable in tests).
var sshConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// loadSSHConfig reads ~/.ssh/config, or returns nil when there is none.
func loadSSHConfig() *ssh_config.Config {
	p := sshConfigPath()
	if p == "" {
		return nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil
	}
	return cfg
}

// configAliases returns the plain names of the Host entries (not patterns
// like "*.corp" or "!x"), in file order.
func configAliases(cfg *ssh_config.Config) []string {
	var names []string
	seen := map[string]bool{}
	if cfg == nil {
		return nil
	}
	for _, h := range cfg.Hosts {
		for _, p := range h.Patterns {
			s := p.String()
			if strings.ContainsAny(s, "*?!") || seen[s] {
				continue
			}
			seen[s] = true
			names = append(names, s)
		}
	}
	return names
}

func isAlias(cfg *ssh_config.Config, name string) bool {
	for _, a := range configAliases(cfg) {
		if a == name {
			return true
		}
	}
	return false
}

func configGet(cfg *ssh_config.Config, alias, key string) string {
	if cfg == nil {
		return ""
	}
	v, _ := cfg.Get(alias, key)
	return v
}

// expandTokens replaces the ssh_config tokens choboterm can know: %h (the
// alias), %r (remote user), %u (local user), %d (home), %% and a leading ~.
func expandTokens(s, alias, remoteUser string) string {
	home, _ := os.UserHomeDir()
	local := ""
	if u, err := user.Current(); err == nil {
		local = u.Username
		if i := strings.LastIndexAny(local, `\`); i >= 0 {
			local = local[i+1:] // DOMAIN\name
		}
	}
	if strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		s = filepath.Join(home, s[2:])
	}
	r := strings.NewReplacer("%%", "%", "%h", alias, "%r", remoteUser, "%u", local, "%d", home)
	return r.Replace(s)
}

// sshArgFlags are the ssh options that take an argument.
const sshArgFlags = "BbcDEeFIiJLlmOoPpQRSWw"

// parseSSHCommand splits "ssh [options] [user@]host" into its parts. ok is
// false when s is not an ssh command line.
func parseSSHCommand(s string) (host, login string, port int, ok bool) {
	f := strings.Fields(s)
	if len(f) < 2 || !strings.EqualFold(strings.TrimSuffix(f[0], ".exe"), "ssh") {
		return "", "", 0, false
	}
	for i := 1; i < len(f); i++ {
		a := f[i]
		if len(a) < 2 || a[0] != '-' {
			if host == "" {
				host = a
			}
			continue // later words are the remote command
		}
		flag := a[1]
		if !strings.ContainsRune(sshArgFlags, rune(flag)) {
			continue
		}
		val := a[2:]
		if val == "" && i+1 < len(f) {
			i++
			val = f[i]
		}
		switch flag {
		case 'p':
			port, _ = strconv.Atoi(val)
		case 'l':
			login = val
		}
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		login, host = host[:at], host[at+1:]
	}
	return host, login, port, host != ""
}

// lookupSSHTarget interprets the Host field: an ssh command line, a
// ~/.ssh/config alias, or "user@host". Anything else comes back unchanged
// with Port 0.
func lookupSSHTarget(cfg *ssh_config.Config, input string) SSHTarget {
	input = strings.TrimSpace(input)
	host, login, port, isCmd := parseSSHCommand(input)
	if !isCmd {
		host = input
		if at := strings.LastIndex(host, "@"); at > 0 && !strings.Contains(host, " ") {
			login, host = host[:at], host[at+1:]
		}
	}
	t := SSHTarget{Host: host, Port: port, Login: login, FromConfig: isAlias(cfg, host)}
	if !isCmd && !t.FromConfig {
		return t // a plain host: Telnet/FTP hosts must not pick up "Host *" settings
	}
	if t.Login == "" {
		t.Login = configGet(cfg, host, "User")
	}
	if t.Port == 0 {
		t.Port, _ = strconv.Atoi(configGet(cfg, host, "Port"))
	}
	if t.Port <= 0 {
		t.Port = 22
	}
	if hn := configGet(cfg, host, "HostName"); hn != "" && t.FromConfig {
		t.HostName = expandTokens(hn, host, t.Login)
	}
	return t
}

// sshDialHost returns the address to connect to for a Host field value: the
// HostName of a ~/.ssh/config entry, or the name itself.
func sshDialHost(cfg *ssh_config.Config, host string) string {
	if hn := configGet(cfg, host, "HostName"); hn != "" {
		return expandTokens(hn, host, "")
	}
	return host
}

// sshIdentityFiles returns the IdentityFile entries that apply to host.
func sshIdentityFiles(cfg *ssh_config.Config, host, login string) []string {
	if cfg == nil {
		return nil
	}
	files, _ := cfg.GetAll(host, "IdentityFile")
	for i, f := range files {
		files[i] = expandTokens(strings.Trim(f, `"`), host, login)
	}
	return files
}

// SSHConfigHost is one Host entry of ~/.ssh/config, for the Host list.
type SSHConfigHost struct {
	Host     string `json:"host"`
	HostName string `json:"hostName"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
}

// GetSSHConfigHosts lists the Host entries of ~/.ssh/config.
func (a *App) GetSSHConfigHosts() []SSHConfigHost {
	cfg := loadSSHConfig()
	var out []SSHConfigHost
	for _, alias := range configAliases(cfg) {
		t := lookupSSHTarget(cfg, alias)
		out = append(out, SSHConfigHost{Host: alias, HostName: t.HostName, Port: t.Port, Login: t.Login})
	}
	return out
}

// LookupSSH interprets what was typed in the Host field (see lookupSSHTarget).
func (a *App) LookupSSH(input string) SSHTarget {
	return lookupSSHTarget(loadSSHConfig(), input)
}
