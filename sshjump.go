package main

// ProxyJump: reaching an SSH server through one or more jump hosts
// (bastions), as "ssh -J" and ProxyJump in ~/.ssh/config do. Each hop logs in
// to the next one over a channel of the previous connection, so only the first
// jump host needs to be reachable from this PC.

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
	"golang.org/x/crypto/ssh"
)

// jumpHost is one hop of a ProxyJump list.
type jumpHost struct {
	name  string // as written: an alias of ~/.ssh/config or an address
	addr  string // host:port to connect to
	login string
	keys  []string
}

// sshCommandJump returns the -J value of an ssh command line ("" if none).
func sshCommandJump(s string) string {
	f := strings.Fields(s)
	if len(f) < 2 || !strings.EqualFold(strings.TrimSuffix(f[0], ".exe"), "ssh") {
		return ""
	}
	for i := 1; i < len(f); i++ {
		a := f[i]
		if len(a) < 2 || a[0] != '-' {
			return "" // options end at the host
		}
		if !strings.ContainsRune(sshArgFlags, rune(a[1])) {
			continue
		}
		val := a[2:]
		if val == "" && i+1 < len(f) {
			i++
			val = f[i]
		}
		if a[1] == 'J' {
			return val
		}
	}
	return ""
}

// proxyJumpFor returns the jump list for host: the -J of an ssh command line
// first, then ProxyJump in ~/.ssh/config. "none" means no jump.
func proxyJumpFor(cfg *ssh_config.Config, host, cmdJump string) string {
	j := strings.TrimSpace(cmdJump)
	if j == "" {
		j = strings.TrimSpace(configGet(cfg, host, "ProxyJump"))
	}
	if strings.EqualFold(j, "none") {
		return ""
	}
	return j
}

// parseJumps splits "[user@]host[:port],..." into hops. A host may be an
// alias of ~/.ssh/config; its HostName, User, Port and IdentityFile apply.
// login is used for hops that name no user anywhere.
func parseJumps(cfg *ssh_config.Config, spec, login string) ([]jumpHost, error) {
	var hops []jumpHost
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = strings.TrimPrefix(part, "ssh://")
		user := ""
		if at := strings.LastIndex(part, "@"); at >= 0 {
			user, part = part[:at], part[at+1:]
		}
		host, port := part, 0
		if h, p, err := net.SplitHostPort(part); err == nil {
			host = h
			if port, err = strconv.Atoi(p); err != nil || port <= 0 || port > 65535 {
				return nil, fmt.Errorf("점프 호스트의 포트가 올바르지 않습니다: %s", part)
			}
		}
		host = strings.Trim(host, "[]")
		if host == "" {
			return nil, fmt.Errorf("점프 호스트가 올바르지 않습니다: %q", spec)
		}
		if user == "" {
			user = configGet(cfg, host, "User")
		}
		if user == "" {
			user = login
		}
		if port == 0 {
			port, _ = strconv.Atoi(configGet(cfg, host, "Port"))
		}
		if port <= 0 {
			port = 22
		}
		hops = append(hops, jumpHost{
			name:  host,
			addr:  net.JoinHostPort(sshDialHost(cfg, host), strconv.Itoa(port)),
			login: user,
			keys:  sshIdentityFiles(cfg, host, user),
		})
	}
	return hops, nil
}

// dialJumps logs in to each hop in turn and returns the connections and a
// channel from the last one to target (host:port).
func dialJumps(hops []jumpHost, target string, confirm hostKeyConfirmer, ask asker) ([]*ssh.Client, net.Conn, error) {
	var clients []*ssh.Client
	fail := func(err error) ([]*ssh.Client, net.Conn, error) {
		closeClients(clients)
		return nil, nil, err
	}
	for _, h := range hops {
		var conn net.Conn
		if n := len(clients); n > 0 {
			c, err := clients[n-1].Dial("tcp", h.addr)
			if err != nil {
				return fail(fmt.Errorf("점프 호스트 %s에 접속하지 못했습니다: %w", h.name, err))
			}
			conn = c
		}
		var hopAsk asker
		if ask != nil {
			hopAsk = func(title, message string, fields []PromptField) ([]string, bool) {
				return ask(fmt.Sprintf("%s (점프 호스트 %s@%s)", title, h.login, h.name), message, fields)
			}
		}
		c, err := sshLogin(h.addr, h.login, "", confirm, hopAsk, conn, h.keys)
		if err != nil {
			return fail(fmt.Errorf("점프 호스트 %s: %w", h.name, err))
		}
		clients = append(clients, c)
	}
	conn, err := clients[len(clients)-1].Dial("tcp", target)
	if err != nil {
		return fail(fmt.Errorf("점프 호스트 %s에서 %s에 접속하지 못했습니다: %w", hops[len(hops)-1].name, target, err))
	}
	return clients, conn, nil
}
