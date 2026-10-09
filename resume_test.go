package main

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// openSFTPTab connects tab 1 of a new App to an SFTP server serving root.
func openSFTPTab(t *testing.T, root string) (*App, string) {
	t.Helper()
	client, err := ssh.Dial("tcp", startSFTPServer(t, root), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.getTab(1).sess = &sshSession{client: client}
	res, err := a.FileOpen(1)
	if err != nil {
		t.Fatal(err)
	}
	return a, res.Home
}

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDownloadResume(t *testing.T) {
	root := t.TempDir()
	data := randomBytes(t, 300_000)
	if err := os.WriteFile(filepath.Join(root, "big.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	a, wd := openSFTPTab(t, root)
	downloads := t.TempDir()
	a.hooks.downloadDir = downloads
	entries := []FileEntry{{Name: "big.bin", Size: int64(len(data))}}
	local := filepath.Join(downloads, "big.bin")
	part := local + partSuffix

	for _, resume := range []bool{true, false} {
		os.Remove(local)
		// A broken download left the first 100 KB; with "처음부터" it is garbage
		// that must be thrown away.
		head := append([]byte(nil), data[:100_000]...)
		if !resume {
			head = bytes.Repeat([]byte{'x'}, 100_000)
		}
		if err := os.WriteFile(part, head, 0o644); err != nil {
			t.Fatal(err)
		}
		asked := 0
		a.hooks.prompt = func(p Prompt) ([]string, bool) {
			asked++
			if p.OK != "이어받기" || p.Cancel != "처음부터" {
				t.Errorf("prompt buttons %q / %q", p.OK, p.Cancel)
			}
			return nil, resume
		}
		if _, err := a.FileDownloadMany(1, wd, entries); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(local)
		if err != nil || !bytes.Equal(got, data) || asked != 1 {
			t.Fatalf("resume=%v: %d bytes, equal=%v, asked %d, %v", resume, len(got), bytes.Equal(got, data), asked, err)
		}
		if _, err := os.Stat(part); !os.IsNotExist(err) {
			t.Fatalf("resume=%v: .part left behind", resume)
		}
	}

	// No .part: no question.
	os.Remove(local)
	a.hooks.prompt = func(Prompt) ([]string, bool) { t.Fatal("asked without a .part"); return nil, false }
	if _, err := a.FileDownloadMany(1, wd, entries); err != nil {
		t.Fatal(err)
	}
}

func TestUploadResume(t *testing.T) {
	root := t.TempDir()
	a, wd := openSFTPTab(t, root)
	src := filepath.Join(t.TempDir(), "up.bin")
	data := randomBytes(t, 300_000)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(root, "up.bin")
	asked := 0
	a.hooks.prompt = func(p Prompt) ([]string, bool) {
		asked++
		return nil, true
	}

	// A smaller file on the server that no broken upload left: replaced, no question.
	os.WriteFile(remote, []byte("old version"), 0o644)
	if _, err := a.FileUploadPaths(1, wd, []string{src}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(remote); !bytes.Equal(got, data) || asked != 0 {
		t.Fatalf("plain upload: equal=%v asked=%d", bytes.Equal(got, data), asked)
	}

	// An upload of this file broke after 120 KB: continued from there.
	os.WriteFile(remote, data[:120_000], 0o644)
	key := a.getTab(1).uploadKey(wd + "/up.bin")
	brokenUploads.Lock()
	brokenUploads.m[key] = localID(src)
	brokenUploads.Unlock()
	if _, err := a.FileUploadPaths(1, wd, []string{src}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(remote); !bytes.Equal(got, data) || asked != 1 {
		t.Fatalf("resumed upload: %d bytes, equal=%v asked=%d", len(got), bytes.Equal(got, data), asked)
	}
	brokenUploads.Lock()
	_, still := brokenUploads.m[key]
	brokenUploads.Unlock()
	if still {
		t.Fatal("finished upload still marked broken")
	}
}
