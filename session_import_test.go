package main

import (
	"strings"
	"testing"
)

func TestExportImportSessions(t *testing.T) {
	useTempHistory(t)
	_, _ = saveSession(SavedSession{Name: "web", Group: "회사", Host: "10.0.0.1", Port: 22, Login: "me", Pass: "secret"}, true)
	_, _ = saveSession(SavedSession{Name: "bbs", Host: "bbs.example.com", Port: 23, Encoding: EncodingEUCKR}, false)

	data, err := encodeExport(loadSessions())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "pass") {
		t.Fatalf("password exported: %s", data)
	}
	list, err := decodeExport(data)
	if err != nil || len(list) != 2 {
		t.Fatalf("decode: %+v, %v", list, err)
	}
	// Importing what is already there adds nothing.
	if r, err := addSessions(list); err != nil || r.Added != 0 || r.Skipped != 2 {
		t.Fatalf("re-import: %+v, %v", r, err)
	}

	useTempHistory(t) // another PC
	if r, err := addSessions(list); err != nil || r.Added != 2 {
		t.Fatalf("import: %+v, %v", r, err)
	}
	got := loadSessions()
	if len(got) != 2 || got[0].Name != "bbs" || got[0].Encoding != EncodingEUCKR || got[1].Group != "회사" || got[1].Login != "me" {
		t.Fatalf("imported %+v", got)
	}

	if _, err := decodeExport([]byte(`[{"host":"x"}]`)); err == nil {
		t.Fatal("foreign file accepted")
	}
}

func TestFromPuTTY(t *testing.T) {
	cases := []struct {
		key  string
		v    puttyValues
		ok   bool
		want SavedSession
	}{
		{"my%20server", puttyValues{HostName: "10.0.0.5", Protocol: "ssh", PortNumber: 8022, UserName: "u"}, true,
			SavedSession{Name: "my server", Group: "PuTTY", Host: "10.0.0.5", Port: 8022, Login: "u", Encoding: EncodingUTF8}},
		{"bbs", puttyValues{HostName: "guest@bbs.example.com", Protocol: "telnet", LineCodePage: "CP949"}, true,
			SavedSession{Name: "bbs", Group: "PuTTY", Host: "bbs.example.com", Port: 23, Login: "guest", Encoding: EncodingEUCKR}},
		{"Default%20Settings", puttyValues{Protocol: "ssh", PortNumber: 22}, false, SavedSession{}},
		{"com1", puttyValues{HostName: "", Protocol: "serial"}, false, SavedSession{}},
		{"raw", puttyValues{HostName: "h", Protocol: "raw", PortNumber: 7}, false, SavedSession{}},
	}
	for _, c := range cases {
		got, ok := fromPuTTY(c.key, c.v)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: %+v %v, want %+v %v", c.key, got, ok, c.want, c.ok)
		}
	}
}

// Reads the real registry; only checks that it doesn't fail.
func TestPuTTYSessionsRead(t *testing.T) {
	list, err := puttySessions()
	if err != nil && !strings.Contains(err.Error(), "PuTTY") {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Host == "" || s.Port <= 0 {
			t.Errorf("bad session %+v", s)
		}
	}
	t.Logf("%d PuTTY sessions", len(list))
}
