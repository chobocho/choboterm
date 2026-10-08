//go:build windows

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/sys/windows"
)

// serveAgentPipe serves keyring on a new named pipe (like the OpenSSH agent service).
func serveAgentPipe(t *testing.T, keyring agent.Agent) string {
	name := fmt.Sprintf(`\\.\pipe\choboterm-test-agent-%d`, time.Now().UnixNano())
	n16, _ := windows.UTF16PtrFromString(name)
	h, err := windows.CreateNamedPipe(n16, windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := windows.ConnectNamedPipe(h, nil); err != nil && err != windows.ERROR_PIPE_CONNECTED {
			windows.CloseHandle(h)
			return
		}
		f := os.NewFile(uintptr(h), name)
		defer f.Close()
		_ = agent.ServeAgent(keyring, f)
	}()
	return name
}

func TestAgentNamedPipe(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	addr, host := keyServer(t, signer.PublicKey(), false)
	useHome(t, addr, host)

	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatal(err)
	}
	pipe := serveAgentPipe(t, keyring)
	agentSources = func() []agentSource {
		return []agentSource{{"pipe", func() (io.ReadWriteCloser, error) { return dialAgentPath(pipe) }}}
	}

	done := make(chan error, 1)
	go func() {
		sess, err := dialAsk(t, addr, "", nil)
		if err == nil {
			sess.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("connect with agent key over a named pipe: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("connect hung (agent pipe not closed?)")
	}
}
