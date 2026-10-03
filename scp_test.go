package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseLsLine(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.Local)
	cases := []struct {
		line string
		want FileEntry
		ok   bool
	}{
		{"drwxr-xr-x  2 root root    4096 Oct  3 01:02 bin", FileEntry{Name: "bin", Size: 4096, IsDir: true, ModTime: "2026-10-03 01:02"}, true},
		{"-rw-r--r--  1 user users 123456 Dec 31 23:59 my file.txt", FileEntry{Name: "my file.txt", Size: 123456, ModTime: "2025-12-31 23:59"}, true},
		{"-rw-r--r--    1 0        0              17 Jan  5  2021 busybox.cfg", FileEntry{Name: "busybox.cfg", Size: 17, ModTime: "2021-01-05"}, true},
		{"-rw-r--r--. 1 a b 5 Oct  2 22:41 한글.txt", FileEntry{Name: "한글.txt", Size: 5, ModTime: "2026-10-02 22:41"}, true},
		{"crw-rw-rw-  1 root root 1, 3 Oct  3 01:02 null", FileEntry{}, false},
		{"total 48", FileEntry{}, false},
	}
	for _, c := range cases {
		got, ok := parseLsLine(c.line, now)
		if ok != c.ok || got != c.want {
			t.Errorf("%q:\n got %+v %v\nwant %+v %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

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
	wslRun(t, "rm -rf /tmp/scpt && mkdir -p '/tmp/scpt/sub dir' && printf 'hello' > /tmp/scpt/한글.txt", nil)

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
	t1.sess = &sshSession{client: client}
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

	payload := testPayload(700_000, 9)
	if err := fs.Upload("/tmp/scpt/sub dir/it's.bin", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	if got := wslRun(t, `cat "/tmp/scpt/sub dir/it's.bin"`, nil); !bytes.Equal(got, payload) {
		t.Fatalf("uploaded %d bytes, want %d", len(got), len(payload))
	}

	if err := fs.Download("/tmp/scpt/none.txt", &buf); err == nil {
		t.Fatal("expected error for missing file")
	}
	wslRun(t, "rm -rf /tmp/scpt", nil)
}
