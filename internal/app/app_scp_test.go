package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"testing"

	"golang.org/x/crypto/ssh"

	"choboterm/internal/sshconn"
	"choboterm/internal/testutil"
)

// startExecOnlySSHServer serves "exec" requests by running the command in WSL
// and refuses the sftp subsystem, like Dropbear without sftp-server.
func startExecOnlySSHServer(t *testing.T) string {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, chReqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						for req := range chReqs {
							if req.Type != "exec" {
								req.Reply(false, nil)
								continue
							}
							req.Reply(true, nil)
							n := binary.BigEndian.Uint32(req.Payload)
							cmd := exec.Command("wsl.exe", "--", "sh", "-c", string(req.Payload[4:4+n]))
							cmd.Stdin, cmd.Stdout, cmd.Stderr = ch, ch, ch.Stderr()
							status := uint32(0)
							if err := cmd.Run(); err != nil {
								status = 1
								if ee, ok := err.(*exec.ExitError); ok {
									status = uint32(ee.ExitCode())
								}
							}
							ch.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, status))
							ch.Close()
							return
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// TestSCPFallback opens the file window on an SSH server without SFTP and
// checks that SCP is used for listing, download and upload.
//
//	CHOBOTERM_WSL=1 go test -run SCPFallback -v
func TestSCPFallback(t *testing.T) {
	if os.Getenv("CHOBOTERM_WSL") == "" {
		t.Skip("CHOBOTERM_WSL not set")
	}
	testutil.WSLRun(t, "rm -rf /tmp/scpt && mkdir -p '/tmp/scpt/sub dir' && printf 'hello' > /tmp/scpt/한글.txt", nil)

	client, err := ssh.Dial("tcp", startExecOnlySSHServer(t), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	t1 := a.getTab(1)
	t1.sess = &sshconn.Session{Client: client}
	res, err := a.FileOpen(1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Protocol != "SCP" {
		t.Fatalf("protocol = %q", res.Protocol)
	}

	list, err := a.FileList(1, "/tmp/scpt")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "sub dir" || !list[0].IsDir || list[1].Name != "한글.txt" || list[1].Size != 5 {
		t.Fatalf("list = %+v", list)
	}

	fs := t1.files.fs
	var buf bytes.Buffer
	if err := fs.Download("/tmp/scpt/한글.txt", &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello" {
		t.Fatalf("download = %q", buf.String())
	}

	payload := testutil.Payload(700_000, 9)
	if err := fs.Upload("/tmp/scpt/sub dir/it's.bin", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	if got := testutil.WSLRun(t, `cat "/tmp/scpt/sub dir/it's.bin"`, nil); !bytes.Equal(got, payload) {
		t.Fatalf("uploaded %d bytes, want %d", len(got), len(payload))
	}

	if err := fs.Download("/tmp/scpt/none.txt", &buf); err == nil {
		t.Fatal("expected error for missing file")
	}
	testutil.WSLRun(t, "rm -rf /tmp/scpt", nil)
}
