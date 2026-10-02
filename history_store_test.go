package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryRemembersEncryptedPassword(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hosts.json")
	orig := historyFile
	historyFile = func() (string, error) { return file, nil }
	defer func() { historyFile = orig }()

	pass := "s3cret!비번"
	if err := addHistory(HostEntry{Host: "bbs.example.com", Port: 23, Login: "guest", Encoding: EncodingEUCKR}, &pass); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("password stored in plain text:\n%s", raw)
	}

	list := loadHistory()
	if len(list) != 1 || list[0].Pass != pass || list[0].Login != "guest" {
		t.Fatalf("list = %+v", list)
	}

	// Another host without a password must not disturb the saved one.
	if err := addHistory(HostEntry{Host: "ssh.example.com", Port: 22}, nil); err != nil {
		t.Fatal(err)
	}
	// Reconnecting without changing the password (nil) keeps it.
	if err := addHistory(HostEntry{Host: "bbs.example.com", Port: 23, Login: "guest"}, nil); err != nil {
		t.Fatal(err)
	}
	list = loadHistory()
	if len(list) != 2 || list[0].Host != "bbs.example.com" || list[0].Pass != pass || list[1].Pass != "" {
		t.Fatalf("list = %+v", list)
	}

	// Connecting with an empty password forgets it.
	empty := ""
	if err := addHistory(HostEntry{Host: "bbs.example.com", Port: 23, Login: "guest"}, &empty); err != nil {
		t.Fatal(err)
	}
	if list = loadHistory(); list[0].Pass != "" {
		t.Fatalf("password not removed: %+v", list[0])
	}
}
