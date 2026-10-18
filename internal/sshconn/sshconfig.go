package sshconn

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
	Jump       string `json:"jump"`       // -J of an ssh command line
	ConfigJump string `json:"configJump"` // ProxyJump of the config entry
}

// ConfigPath is ~/.ssh/config (replaceable in tests).
var ConfigPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

// LoadConfig reads ~/.ssh/config, or returns nil when there is none.
func LoadConfig() *ssh_config.Config {
	p := ConfigPath()
	if p == "" {
		return nil
	}
	if _, err := os.Stat(p); err != nil {
		return nil
	}
	cfg, err := ssh_config.Decode(strings.NewReader(expandConfig(p, 0)))
	if err != nil {
		return nil
	}
	return cfg
}

// maxIncludeDepth is how deep Include files may nest, as in OpenSSH.
const maxIncludeDepth = 16

// expandConfig returns an ssh config file as the parser can read it:
//
//   - Include files are put in place, so their Host names are listed and
//     used like the others. A Host or Match in an included file ends with
//     it, as in OpenSSH: the block the Include stood in goes on afterwards.
//   - A Match the parser can't read (exec, user, final, several criteria)
//     would make it fail on the whole file, losing every name in it. Such a
//     block becomes one that matches no host, and the rest still works.
func expandConfig(path string, depth int) string {
	data, err := os.ReadFile(path)
	if err != nil || depth > maxIncludeDepth {
		return ""
	}
	var out strings.Builder
	block := "" // the Host or Match line in effect; "" = before the first (all hosts)
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		key, val := configLine(line)
		switch key {
		case "include":
			for _, f := range includeFiles(val) {
				out.WriteString(expandConfig(f, depth+1))
				out.WriteString("\n")
			}
			if block == "" {
				out.WriteString("Host *\n")
			} else {
				out.WriteString(block + "\n")
			}
			continue
		case "match":
			line = matchAsHost(val)
			block = line
		case "host":
			block = strings.TrimSpace(line)
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}

// configLine splits a config line into its lower-case keyword and its value
// ("Key value" or "Key=value"); comments and empty lines have no keyword.
func configLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return "", ""
	}
	i := strings.IndexAny(line, " \t=")
	if i < 0 {
		return strings.ToLower(line), ""
	}
	val := strings.TrimLeft(line[i:], " \t")
	val = strings.TrimSpace(strings.TrimPrefix(val, "="))
	return strings.ToLower(line[:i]), val
}

// includeFiles returns the files an Include names: patterns (quoted when they
// have spaces) relative to ~/.ssh unless absolute or starting with ~/, each
// pattern's files in name order.
func includeFiles(val string) []string {
	var files []string
	base := filepath.Dir(ConfigPath())
	for _, pat := range splitQuoted(val) {
		if strings.HasPrefix(pat, "~/") || strings.HasPrefix(pat, `~\`) {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			pat = filepath.Join(home, pat[2:])
		} else if !filepath.IsAbs(pat) {
			pat = filepath.Join(base, pat)
		}
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if st, err := os.Stat(m); err == nil && !st.IsDir() {
				files = append(files, m)
			}
		}
	}
	return files
}

// splitQuoted splits on spaces, keeping "quoted parts" together.
func splitQuoted(s string) []string {
	var (
		out    []string
		cur    strings.Builder
		quoted bool
	)
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// matchAsHost turns a Match line into the Host line that means the same:
// "Match all" and "Match host a,b" are kept; anything else can't be decided
// here (exec, user, canonical, ...), so its block applies to no host.
func matchAsHost(val string) string {
	f := strings.Fields(val)
	switch {
	case len(f) == 1 && strings.EqualFold(f[0], "all"):
		return "Host *"
	case len(f) == 2 && strings.EqualFold(f[0], "host"):
		return "Host " + strings.Join(strings.FieldsFunc(f[1], func(r rune) bool { return r == ',' }), " ")
	}
	return "Host !*"
}

// ConfigAliases returns the plain names of the Host entries (not patterns
// like "*.corp" or "!x"), in file order.
func ConfigAliases(cfg *ssh_config.Config) []string {
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
	for _, a := range ConfigAliases(cfg) {
		if a == name {
			return true
		}
	}
	return false
}

func ConfigGet(cfg *ssh_config.Config, alias, key string) string {
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

// ParseCommand splits "ssh [options] [user@]host" into its parts. ok is
// false when s is not an ssh command line.
func ParseCommand(s string) (host, login string, port int, ok bool) {
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

// LookupTarget interprets the Host field: an ssh command line, a
// ~/.ssh/config alias, or "user@host". Anything else comes back unchanged
// with Port 0.
func LookupTarget(cfg *ssh_config.Config, input string) SSHTarget {
	input = strings.TrimSpace(input)
	host, login, port, isCmd := ParseCommand(input)
	if !isCmd {
		host = input
		if at := strings.LastIndex(host, "@"); at > 0 && !strings.Contains(host, " ") {
			login, host = host[:at], host[at+1:]
		}
	}
	t := SSHTarget{Host: host, Port: port, Login: login, FromConfig: isAlias(cfg, host), Jump: CommandJump(input)}
	if !isCmd && !t.FromConfig {
		return t // a plain host: Telnet/FTP hosts must not pick up "Host *" settings
	}
	if t.Login == "" {
		t.Login = ConfigGet(cfg, host, "User")
	}
	if t.Port == 0 {
		t.Port, _ = strconv.Atoi(ConfigGet(cfg, host, "Port"))
	}
	if t.Port <= 0 {
		t.Port = 22
	}
	if hn := ConfigGet(cfg, host, "HostName"); hn != "" && t.FromConfig {
		t.HostName = expandTokens(hn, host, t.Login)
	}
	if t.FromConfig {
		t.ConfigJump = ConfigGet(cfg, host, "ProxyJump")
	}
	return t
}

// DialHost returns the address to connect to for a Host field value: the
// HostName of a ~/.ssh/config entry, or the name itself.
func DialHost(cfg *ssh_config.Config, host string) string {
	if hn := ConfigGet(cfg, host, "HostName"); hn != "" {
		return expandTokens(hn, host, "")
	}
	return host
}

// IdentityFiles returns the IdentityFile entries that apply to host.
func IdentityFiles(cfg *ssh_config.Config, host, login string) []string {
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
