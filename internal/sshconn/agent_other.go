//go:build !windows

package sshconn

// platformAgentSources: elsewhere only SSH_AUTH_SOCK is used.
func platformAgentSources() []agentSource { return nil }
