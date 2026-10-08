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
	// FontUTF8 and FontEUCKR are the terminal font families for each encoding.
	FontUTF8  string `json:"fontUtf8"`
	FontEUCKR string `json:"fontEucKr"`
	// Theme is the terminal color scheme (names in frontend/src/themes.ts).
	Theme string `json:"theme"`
	// CursorStyle is "block", "underline" or "bar"; CursorBlink makes it blink.
	CursorStyle string `json:"cursorStyle"`
	CursorBlink bool   `json:"cursorBlink"`
	// Scrollback is how many lines each terminal keeps above the screen.
	Scrollback int `json:"scrollback"`
	// Translucency is "off", "window" (tabs and terminals, text too; dialogs
	// stay opaque) or "background" (only the terminal background). Opacity is
	// in percent.
	Translucency string `json:"translucency"`
	Opacity      int    `json:"opacity"`
	// GlassGPU keeps the GPU renderer with "background" translucency, which
	// otherwise draws with the browser for smoother text.
	GlassGPU bool `json:"glassGpu"`
	// PasteNoConfirm skips the confirmation before pasting several lines.
	PasteNoConfirm bool `json:"pasteNoConfirm"`
	// KeepAlive is the keepalive interval in seconds for SSH and Telnet (0 = off).
	KeepAlive int `json:"keepAlive"`
	// AutoReconnect reconnects a tab whose connection broke.
	AutoReconnect bool `json:"autoReconnect"`
	// LogAuto starts a session log whenever a terminal connection opens.
	LogAuto bool `json:"logAuto"`
	// LogDir is the folder for session logs ("" = Documents\choboterm\logs).
	LogDir string `json:"logDir"`
	// LogRaw keeps escape codes in session logs instead of plain text.
	LogRaw bool `json:"logRaw"`
	// Macros are texts sent to the terminal, optionally bound to a key.
	Macros []Macro `json:"macros"`
	// Window is the main window's last position, kept by the Go side only.
	Window *WindowState `json:"window,omitempty"`
}

// Macro is a named text sent to the terminal. Key is "" or a function key
// such as "F5", "Shift+F5" or "Ctrl+F5". The frontend turns line breaks in
// Text into Enter and understands a few backslash escapes.
type Macro struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	Text string `json:"text"`
	Kind string `json:"kind"` // "" = text sent as typed, "lua" = a Lua script run in the tab
}

// WindowState is the main window's restored bounds (screen pixels) and
// whether it was maximized.
type WindowState struct {
	Left      int  `json:"left"`
	Top       int  `json:"top"`
	Right     int  `json:"right"`
	Bottom    int  `json:"bottom"`
	Maximized bool `json:"maximized"`
}

func (w WindowState) valid() bool {
	return w.Right-w.Left >= 200 && w.Bottom-w.Top >= 150
}

func defaultSettings() Settings {
	return Settings{
		FontSize: 15, FontUTF8: "D2Coding", FontEUCKR: "GulimChe",
		Theme: "choboterm", CursorStyle: "block", CursorBlink: true, Scrollback: 5000,
		Translucency: "off", Opacity: 85,
		KeepAlive: 60, AutoReconnect: true, Macros: []Macro{},
	}
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
	if s.Macros == nil {
		s.Macros = []Macro{} // the page expects a list, not null
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

// SaveSettings stores the user preferences. The window position is kept as stored.
func (a *App) SaveSettings(s Settings) error {
	return updateSettings(func(cur *Settings) {
		w := cur.Window
		*cur = s
		cur.Window = w
	})
}
