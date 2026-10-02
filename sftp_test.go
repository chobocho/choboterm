package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// startSFTPServer runs an in-process SSH server that only serves the sftp subsystem.
func startSFTPServer(t *testing.T, root string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
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
							ok := req.Type == "subsystem" && string(req.Payload[4:]) == "sftp"
							req.Reply(ok, nil)
							if ok {
								srv, _ := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
								_ = srv.Serve()
								ch.Close()
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestSFTPRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello 한글"), 0o644); err != nil {
		t.Fatal(err)
	}

	client, err := ssh.Dial("tcp", startSFTPServer(t, root), &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	fs, err := newSFTP(client)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	wd, err := fs.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	list, err := fs.List(wd)
	if err != nil {
		t.Fatal(err)
	}
	sortEntries(list)
	if len(list) != 2 || list[0].Name != "sub" || !list[0].IsDir || list[1].Name != "hello.txt" || list[1].Size != int64(len("hello 한글")) {
		t.Fatalf("list = %+v", list)
	}

	var buf bytes.Buffer
	if err := fs.Download(wd+"/hello.txt", &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello 한글" {
		t.Fatalf("download = %q", buf.String())
	}

	payload := bytes.Repeat([]byte("0123456789"), 100_000) // 1MB, several SFTP packets
	if err := fs.Upload(wd+"/sub/up.bin", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "sub", "up.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("uploaded %d bytes, want %d", len(got), len(payload))
	}
}
