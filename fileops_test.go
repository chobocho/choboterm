package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"golang.org/x/crypto/ssh"
)

// listTree returns all files and folders under root as slash paths.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func equalList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFileOperationsOverSFTP(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"d/a.txt":     "a",
		"d/sub/b.txt": "bb",
		"x.txt":       "x",
	})
	client, err := ssh.Dial("tcp", startSFTPServer(t, root), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	downloads := t.TempDir()
	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.hooks.downloadDir = downloads
	a.getTab(1).sess = &sshSession{client: client}
	res, err := a.FileOpen(1)
	if err != nil || res.Protocol != "SFTP" {
		t.Fatalf("open: %+v, %v", res, err)
	}
	wd := res.Home

	// New folder and rename; names must be a single path element.
	if err := a.FileMkdir(1, wd, "new"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		if err := a.FileMkdir(1, wd, bad); err == nil {
			t.Fatalf("mkdir %q accepted", bad)
		}
	}
	if err := a.FileRename(1, wd, "x.txt", "y.txt"); err != nil {
		t.Fatal(err)
	}

	// Upload a folder (with a subfolder) and a single file into "new".
	local := t.TempDir()
	writeFiles(t, local, map[string]string{
		"up/1.txt":       "one",
		"up/inner/2.txt": "two",
		"single.bin":     "s",
	})
	n, err := a.FileUploadPaths(1, wd+"/new", []string{filepath.Join(local, "up"), filepath.Join(local, "single.bin")})
	if err != nil || n != 3 {
		t.Fatalf("upload: %d, %v", n, err)
	}
	want := []string{
		"d/", "d/a.txt", "d/sub/", "d/sub/b.txt",
		"new/", "new/single.bin", "new/up/", "new/up/1.txt", "new/up/inner/", "new/up/inner/2.txt",
		"y.txt",
	}
	if got := listTree(t, root); !equalList(got, want) {
		t.Fatalf("remote after upload:\n%v\nwant\n%v", got, want)
	}

	// Download a folder and a file; the second time the names get " (1)".
	entries := []FileEntry{{Name: "d", IsDir: true}, {Name: "y.txt", Size: 1}}
	for i := 0; i < 2; i++ {
		r, err := a.FileDownloadMany(1, wd, entries)
		if err != nil || r.Count != 3 || r.Dir != downloads {
			t.Fatalf("download %d: %+v, %v", i, r, err)
		}
	}
	want = []string{
		"d (1)/", "d (1)/a.txt", "d (1)/sub/", "d (1)/sub/b.txt",
		"d/", "d/a.txt", "d/sub/", "d/sub/b.txt",
		"y (1).txt", "y.txt",
	}
	if got := listTree(t, downloads); !equalList(got, want) {
		t.Fatalf("local after download:\n%v\nwant\n%v", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(downloads, "d", "sub", "b.txt")); string(b) != "bb" {
		t.Fatalf("b.txt = %q", b)
	}

	// Delete folders with their contents and a file.
	n, err = a.FileDelete(1, wd, []FileEntry{{Name: "d", IsDir: true}, {Name: "new", IsDir: true}, {Name: "y.txt"}})
	if err != nil || n != 3 {
		t.Fatalf("delete: %d, %v", n, err)
	}
	if got := listTree(t, root); len(got) != 0 {
		t.Fatalf("remote after delete: %v", got)
	}
	if _, err := a.FileDelete(1, wd, []FileEntry{{Name: "missing.txt"}}); err == nil {
		t.Fatal("deleting a missing file succeeded")
	}
}

func TestFileStartDir(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"d/sub/a.txt": "a"})
	client, err := ssh.Dial("tcp", startSFTPServer(t, root), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.getTab(1).sess = &sshSession{client: client}
	res, err := a.FileOpen(1)
	if err != nil {
		t.Fatal(err)
	}
	wd := res.Home
	// The test server runs no commands, so ~ falls back to the SFTP start folder.
	for in, want := range map[string]string{
		"~":                 wd,
		"~/d/sub/":          wd + "/d/sub",
		wd + "/d/../d":      wd + "/d",
		wd + "/d/sub/a.txt": "", // a file
		wd + "/missing":     "",
		"d":                 "", // relative
		"~other/d":          "",
	} {
		if got := a.FileStartDir(1, in); got != want {
			t.Errorf("FileStartDir(%q) = %q, want %q", in, got, want)
		}
	}
}
