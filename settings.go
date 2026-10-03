package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the user preferences kept in %AppData%\choboterm\settings.json.
type Settings struct {
	// FontSize is the terminal font size in pixels.
	FontSize int `json:"fontSize"`
	// PasteNoConfirm skips the confirmation before pasting several lines.
	PasteNoConfirm bool `json:"pasteNoConfirm"`
}

func defaultSettings() Settings {
	return Settings{FontSize: 15}
}

var settingsMu sync.Mutex

// settingsFile is the settings file path; tests may replace it.
var settingsFile = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "choboterm", "settings.json"), nil
}

// loadSettings reads the settings file. Missing fields keep their defaults.
func loadSettings() Settings {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return readSettings()
}

func readSettings() Settings {
	s := defaultSettings()
	p, err := settingsFile()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return s
	}
	if json.Unmarshal(data, &s) != nil {
		return defaultSettings()
	}
	return s
}

// updateSettings applies change to the stored settings and writes them back.
func updateSettings(change func(*Settings)) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := readSettings()
	change(&s)
	p, err := settingsFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// GetSettings returns the user preferences.
func (a *App) GetSettings() Settings {
	return loadSettings()
}

// SaveSettings stores the user preferences.
func (a *App) SaveSettings(s Settings) error {
	return updateSettings(func(cur *Settings) { *cur = s })
}
