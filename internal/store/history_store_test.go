package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"choboterm/internal/codec"
	"choboterm/internal/secret"
)

func TestHistoryRemembersEncryptedPassword(t *testing.T) {
	requireSecretStore(t)
	file := filepath.Join(t.TempDir(), "hosts.json")
	orig := HistoryFile
	HistoryFile = func() (string, error) { return file, nil }
	defer func() { HistoryFile = orig }()

	pass := "s3cret!비번"
	if err := AddHistory(HostEntry{Host: "bbs.example.com", Port: 23, Login: "guest", Encoding: codec.EUCKR}, &pass); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("password stored in plain text:\n%s", raw)
	}

	list := LoadHistory()
	if len(list) != 1 || list[0].Pass != pass || list[0].Login != "guest" {
		t.Fatalf("list = %+v", list)
	}

	// Another host without a password must not disturb the saved one.
	if err := AddHistory(HostEntry{Host: "ssh.example.com", Port: 22}, nil); err != nil {
		t.Fatal(err)
	}
	// Reconnecting without changing the password (nil) keeps it.
	if err := AddHistory(HostEntry{Host: "bbs.example.com", Port: 23, Login: "guest"}, nil); err != nil {
		t.Fatal(err)
	}
	list = LoadHistory()
	if len(list) != 2 || list[0].Host != "bbs.example.com" || list[0].Pass != pass || list[1].Pass != "" {
		t.Fatalf("list = %+v", list)
	}

	// Connecting with an empty password forgets it.
	empty := ""
	if err := AddHistory(HostEntry{Host: "bbs.example.com", Port: 23, Login: "guest"}, &empty); err != nil {
		t.Fatal(err)
	}
	if list = LoadHistory(); list[0].Pass != "" {
		t.Fatalf("password not removed: %+v", list[0])
	}
}

// requireSecretStore skips a test that saves passwords where they can't be
// (Linux without a keyring).
func requireSecretStore(t *testing.T) {
	t.Helper()
	if _, err := secret.Protect([]byte("x")); errors.Is(err, secret.ErrNoStore) {
		t.Skip("no secret store:", err)
	}
}

func TestSecretRoundTrip(t *testing.T) {
	requireSecretStore(t)
	enc, err := secret.Protect([]byte("비번 pw"))
	if err != nil || strings.Contains(string(enc), "pw") {
		t.Fatalf("protect: %q %v", enc, err)
	}
	if plain, err := secret.Unprotect(enc); err != nil || string(plain) != "비번 pw" {
		t.Fatalf("unprotect: %q %v", plain, err)
	}
}
