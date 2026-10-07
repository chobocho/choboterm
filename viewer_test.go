package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/text/encoding/korean"
)

func TestFileViewOverSFTP(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("0123456789", 1000)
	writeFiles(t, root, map[string]string{"small.txt": "안녕", "big.txt": big})
	client, err := ssh.Dial("tcp", startSFTPServer(t, root), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	defer func(old int) { viewLimit = old }(viewLimit)
	viewLimit = 4096

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.getTab(1).sess = &sshSession{client: client}
	res, err := a.FileOpen(1)
	if err != nil {
		t.Fatal(err)
	}
	wd := res.Home

	decode := func(r ViewResult) string {
		b, err := base64.StdEncoding.DecodeString(r.Data)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	r, err := a.FileView(1, wd+"/small.txt", 6)
	if err != nil || r.Truncated || decode(r) != "안녕" {
		t.Fatalf("small: %+v, %v", r, err)
	}
	// A big file stops at the limit, and the connection stays usable.
	r, err = a.FileView(1, wd+"/big.txt", int64(len(big)))
	if err != nil || !r.Truncated || decode(r) != big[:4096] {
		t.Fatalf("big: truncated=%v len=%d, %v", r.Truncated, len(decode(r)), err)
	}
	if list, err := a.FileList(1, wd); err != nil || len(list) != 2 {
		t.Fatalf("list after view: %v, %v", list, err)
	}
	if _, err := a.FileView(1, wd+"/missing.txt", 0); err == nil {
		t.Fatal("viewing a missing file succeeded")
	}
}

func TestEncodeText(t *testing.T) {
	out, bad := encodeText("한글?", EncodingUTF8)
	if string(out) != "한글?" || bad != 0 {
		t.Fatalf("utf-8: %q %d", out, bad)
	}
	want, _ := korean.EUCKR.NewEncoder().String("한글?")
	out, bad = encodeText("한글?", EncodingEUCKR)
	if !bytes.Equal(out, []byte(want)) || bad != 0 {
		t.Fatalf("euc-kr: % x %d", out, bad)
	}
	// Emoji don't exist in CP949.
	out, bad = encodeText("a😀b😀", EncodingEUCKR)
	if string(out) != "a?b?" || bad != 2 {
		t.Fatalf("euc-kr missing: %q %d", out, bad)
	}
}

func TestFileSaveTextOverSFTP(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.txt": "old"})
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
	p := res.Home + "/a.txt"
	v, err := a.FileView(1, p, 3)
	if err != nil || v.Size != 3 || v.ModTime == "" {
		t.Fatalf("view: %+v, %v", v, err)
	}

	// Saved in EUC-KR.
	req := TextSave{Path: p, Text: "한글\r\n", Encoding: EncodingEUCKR, Size: v.Size, ModTime: v.ModTime}
	r, err := a.FileSaveText(1, req)
	if err != nil || !r.Saved || r.Size != 6 {
		t.Fatalf("save: %+v, %v", r, err)
	}
	want, _ := korean.EUCKR.NewEncoder().String("한글\r\n")
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != want {
		t.Fatalf("file = % x", b)
	}

	// Characters missing from EUC-KR need permission.
	req.Text, req.Size, req.ModTime = "😀", r.Size, r.ModTime
	if r, err = a.FileSaveText(1, req); err != nil || r.Saved || r.Bad != 1 {
		t.Fatalf("lossy: %+v, %v", r, err)
	}

	// Someone else changed the file: not overwritten unless asked.
	writeFiles(t, root, map[string]string{"a.txt": "changed elsewhere"})
	req.Text = "mine"
	if r, err = a.FileSaveText(1, req); err != nil || r.Saved || !r.Conflict {
		t.Fatalf("conflict: %+v, %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != "changed elsewhere" {
		t.Fatalf("overwritten: %q", b)
	}
	req.IgnoreConflict = true
	if r, err = a.FileSaveText(1, req); err != nil || !r.Saved {
		t.Fatalf("forced: %+v, %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); string(b) != "mine" {
		t.Fatalf("forced file = %q", b)
	}
}
