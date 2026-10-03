package main

import (
	"os"
	"path/filepath"
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

	if s := a.GetSettings(); s != defaultSettings() {
		t.Fatalf("defaults = %+v", s)
	}
	s := a.GetSettings()
	s.FontSize = 18
	s.PasteNoConfirm = true
	if err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if got := a.GetSettings(); got != s {
		t.Fatalf("got %+v, want %+v", got, s)
	}

	// A file from an older version keeps defaults for fields it doesn't have.
	if err := os.WriteFile(file, []byte(`{"pasteNoConfirm":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.GetSettings(); got.FontSize != 15 || !got.PasteNoConfirm {
		t.Fatalf("partial file: %+v", got)
	}

	// A broken file falls back to defaults.
	if err := os.WriteFile(file, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.GetSettings(); got != defaultSettings() {
		t.Fatalf("broken file: %+v", got)
	}
}
