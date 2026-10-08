//go:build !windows

package main

// platformAgentSources: elsewhere only SSH_AUTH_SOCK is used.
func platformAgentSources() []agentSource { return nil }
