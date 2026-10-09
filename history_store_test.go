package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryRemembersEncryptedPassword(t *testing.T) {
	requireSecretStore(t)
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

func TestTabThemeSavedPerHost(t *testing.T) {
	hist := filepath.Join(t.TempDir(), "hosts.json")
	orig := historyFile
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = orig }()

	a := NewApp()
	if ok, err := a.SetTabTheme(1, "dracula"); ok || err != nil {
		t.Fatalf("never connected tab: %v %v", ok, err)
	}
	_ = addHistory(HostEntry{Host: "busan", Port: 22, Login: "me"}, nil)
	_ = addHistory(HostEntry{Host: "seoul", Port: 22}, nil)
	tb := a.getTab(1)
	tb.host, tb.port = "busan", 22

	if ok, err := a.SetTabTheme(1, "dracula"); !ok || err != nil {
		t.Fatalf("set: %v %v", ok, err)
	}
	// Connecting again keeps it; other hosts are not touched.
	_ = addHistory(HostEntry{Host: "busan", Port: 22, Login: "me"}, nil)
	if got := a.TabTheme(1); got != "dracula" {
		t.Fatalf("theme = %q", got)
	}
	for _, h := range loadHistory() {
		if want := map[string]string{"busan": "dracula"}[h.Host]; prefsFor(h.Host, h.Port).Theme != want {
			t.Fatalf("%s theme = %q", h.Host, prefsFor(h.Host, h.Port).Theme)
		}
	}
	// "" goes back to the Settings theme.
	_, _ = a.SetTabTheme(1, "")
	if got := a.TabTheme(1); got != "" {
		t.Fatalf("after reset: %q", got)
	}
}

// requireSecretStore skips a test that saves passwords where they can't be
// (Linux without a keyring).
func requireSecretStore(t *testing.T) {
	t.Helper()
	if _, err := protectSecret([]byte("x")); errors.Is(err, errNoSecretStore) {
		t.Skip("no secret store:", err)
	}
}

func TestSecretRoundTrip(t *testing.T) {
	requireSecretStore(t)
	enc, err := protectSecret([]byte("비번 pw"))
	if err != nil || strings.Contains(string(enc), "pw") {
		t.Fatalf("protect: %q %v", enc, err)
	}
	if plain, err := unprotectSecret(enc); err != nil || string(plain) != "비번 pw" {
		t.Fatalf("unprotect: %q %v", plain, err)
	}
}
