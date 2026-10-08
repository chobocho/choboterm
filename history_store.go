package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// HostEntry is one remembered connection as shown in the Host list.
// Pass is filled only for hosts with a saved (Telnet) password.
type HostEntry struct {
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	Login    string    `json:"login"`
	Encoding string    `json:"encoding"`
	Pass     string    `json:"pass"`
	Forwards []Forward `json:"forwards"`
	Theme    string    `json:"theme"` // color theme for this host ("" = the one in Settings)
}

// storedEntry is the on-disk form: the password is DPAPI-encrypted, never plain.
type storedEntry struct {
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	Login    string    `json:"login"`
	Encoding string    `json:"encoding"`
	PassEnc  string    `json:"passEnc,omitempty"`
	Forwards []Forward `json:"forwards,omitempty"` // SSH port forwarding rules
	Theme    string    `json:"theme,omitempty"`
}

const maxHistory = 20

var historyMu sync.Mutex

// historyFile is the hosts file path; tests may replace it.
var historyFile = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "choboterm", "hosts.json"), nil
}

func loadHistory() []HostEntry {
	historyMu.Lock()
	defer historyMu.Unlock()
	stored := readHistory()
	list := make([]HostEntry, 0, len(stored))
	for _, s := range stored {
		list = append(list, HostEntry{
			Host:     s.Host,
			Port:     s.Port,
			Login:    s.Login,
			Encoding: s.Encoding,
			Pass:     decryptPass(s.PassEnc),
			Forwards: s.Forwards,
			Theme:    s.Theme,
		})
	}
	return list
}

func readHistory() []storedEntry {
	p, err := historyFile()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var list []storedEntry
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

func encryptPass(pass string) string {
	if pass == "" {
		return ""
	}
	enc, err := protectSecret([]byte(pass))
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(enc)
}

func decryptPass(passEnc string) string {
	if passEnc == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(passEnc)
	if err != nil {
		return ""
	}
	plain, err := unprotectSecret(raw)
	if err != nil {
		return ""
	}
	return string(plain)
}

// addHistory moves e to the top of the list (most recent first).
// If pass is non-nil the saved password is replaced (an empty string removes it);
// if nil, any password already saved for this host is kept.
func addHistory(e HostEntry, pass *string) error {
	historyMu.Lock()
	defer historyMu.Unlock()

	old := readHistory()
	entry := storedEntry{Host: e.Host, Port: e.Port, Login: e.Login, Encoding: e.Encoding}
	for _, h := range old {
		if h.Host == e.Host && h.Port == e.Port {
			entry.PassEnc = h.PassEnc
			entry.Forwards = h.Forwards
			entry.Theme = h.Theme
		}
	}
	if pass != nil {
		entry.PassEnc = encryptPass(*pass)
	}

	list := []storedEntry{entry}
	for _, h := range old {
		if h.Host != e.Host || h.Port != e.Port {
			list = append(list, h)
		}
	}
	if len(list) > maxHistory {
		list = list[:maxHistory]
	}

	return writeHistory(list)
}

func writeHistory(list []storedEntry) error {
	p, err := historyFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// historyEntry returns what is saved for host:port.
func historyEntry(host string, port int) (storedEntry, bool) {
	historyMu.Lock()
	defer historyMu.Unlock()
	for _, h := range readHistory() {
		if h.Host == host && h.Port == port {
			return h, true
		}
	}
	return storedEntry{}, false
}

// updateHistoryEntry changes the entry of host:port, which is already in the
// history (it was added when connecting).
func updateHistoryEntry(host string, port int, change func(*storedEntry)) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	entries := readHistory()
	for i := range entries {
		if entries[i].Host == host && entries[i].Port == port {
			change(&entries[i])
			return writeHistory(entries)
		}
	}
	return nil
}

// historyForwards returns the port forwarding rules saved for host:port.
func historyForwards(host string, port int) []Forward {
	h, _ := historyEntry(host, port)
	return h.Forwards
}

// setHistoryForwards saves the port forwarding rules of host:port.
func setHistoryForwards(host string, port int, list []Forward) error {
	return updateHistoryEntry(host, port, func(e *storedEntry) { e.Forwards = list })
}

// TabTheme returns the color theme saved for the server of a tab's
// connection ("" = the theme in Settings).
func (a *App) TabTheme(tabID int) string {
	host, port, ok := a.tabTarget(tabID)
	if !ok {
		return ""
	}
	h, _ := historyEntry(host, port)
	return h.Theme
}

// SetTabTheme saves a color theme for the server of a tab's connection
// ("" = back to the theme in Settings). It reports false if the tab has
// never been connected, so there is no server to save it for.
func (a *App) SetTabTheme(tabID int, theme string) (bool, error) {
	host, port, ok := a.tabTarget(tabID)
	if !ok {
		return false, nil
	}
	return true, updateHistoryEntry(host, port, func(e *storedEntry) { e.Theme = theme })
}

// tabTarget is where a tab's terminal session is (or was last) connected.
func (a *App) tabTarget(tabID int) (string, int, bool) {
	t := a.findTab(tabID)
	if t == nil {
		return "", 0, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.host, t.port, t.host != ""
}
