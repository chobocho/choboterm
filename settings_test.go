package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func useTempSettings(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "settings.json")
	orig := settingsFile
	settingsFile = func() (string, error) { return file, nil }
	t.Cleanup(func() { settingsFile = orig })
	return file
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	file := useTempSettings(t)
	a := NewApp()

	if s := a.GetSettings(); !reflect.DeepEqual(s, defaultSettings()) {
		t.Fatalf("defaults = %+v", s)
	}
	s := a.GetSettings()
	s.FontSize = 18
	s.PasteNoConfirm = true
	s.Macros = []Macro{{Name: "목록", Key: "F5", Text: "ls -al\n"}}
	if err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if got := a.GetSettings(); !reflect.DeepEqual(got, s) || got.Window != nil {
		t.Fatalf("got %+v, want %+v", got, s)
	}

	// The window position belongs to the Go side; saving from the page keeps it.
	w := WindowState{Left: 10, Top: 20, Right: 910, Bottom: 620, Maximized: true}
	if err := updateSettings(func(s *Settings) { s.Window = &w }); err != nil {
		t.Fatal(err)
	}
	s.FontSize = 20
	if err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if got := a.GetSettings(); got.FontSize != 20 || got.Window == nil || *got.Window != w {
		t.Fatalf("window lost: %+v", got)
	}

	// A file from an older version keeps defaults for fields it doesn't have.
	if err := os.WriteFile(file, []byte(`{"pasteNoConfirm":true,"macros":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := a.GetSettings()
	if got.FontSize != 15 || got.KeepAlive != 60 || !got.AutoReconnect || !got.PasteNoConfirm || got.Macros == nil ||
		got.Theme != "choboterm" || got.CursorStyle != "block" || !got.CursorBlink || got.Scrollback != 5000 {
		t.Fatalf("partial file: %+v", got)
	}

	// A broken file falls back to defaults.
	if err := os.WriteFile(file, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.GetSettings(); !reflect.DeepEqual(got, defaultSettings()) {
		t.Fatalf("broken file: %+v", got)
	}
}
