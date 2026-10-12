package main

import (
	"choboterm/internal/sshconn"
)

// GetSSHConfigHosts lists the Host entries of ~/.ssh/config.
func (a *App) GetSSHConfigHosts() []sshconn.SSHConfigHost {
	cfg := sshconn.LoadConfig()
	var out []sshconn.SSHConfigHost
	for _, alias := range sshconn.ConfigAliases(cfg) {
		t := sshconn.LookupTarget(cfg, alias)
		out = append(out, sshconn.SSHConfigHost{Host: alias, HostName: t.HostName, Port: t.Port, Login: t.Login})
	}
	return out
}

// LookupSSH interprets what was typed in the Host field (see lookupSSHTarget).
func (a *App) LookupSSH(input string) sshconn.SSHTarget {
	return sshconn.LookupTarget(sshconn.LoadConfig(), input)
}
