// Package store keeps what is remembered between runs: the connection history,
// saved sessions and per-server settings, with import and export.
package store

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"choboterm/internal/secret"
)

// HostEntry is one remembered connection as shown in the Host list.
// Pass is filled only for hosts with a saved (Telnet) password.
type HostEntry struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Encoding string `json:"encoding"`
	Pass     string `json:"pass"`
	Jump     string `json:"jump"` // SSH jump hosts, as in ConnectRequest
}

// storedEntry is the on-disk form: the password is DPAPI-encrypted, never plain.
type storedEntry struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Encoding string `json:"encoding"`
	PassEnc  string `json:"passEnc,omitempty"`
	Jump     string `json:"jump,omitempty"`
	// Before sessions.json these were kept here; they are only read now,
	// as the starting value of the server's prefs (see prefsFor).
	Forwards []Forward `json:"forwards,omitempty"`
	Theme    string    `json:"theme,omitempty"`
}

const MaxHistory = 20

var historyMu sync.Mutex

// HistoryFile is the hosts file path; tests may replace it.
var HistoryFile = func() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "choboterm", "hosts.json"), nil
}

func LoadHistory() []HostEntry {
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
			Jump:     s.Jump,
		})
	}
	return list
}

func readHistory() []storedEntry {
	p, err := HistoryFile()
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
	enc, err := secret.Protect([]byte(pass))
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
	plain, err := secret.Unprotect(raw)
	if err != nil {
		return ""
	}
	return string(plain)
}

// AddHistory moves e to the top of the list (most recent first).
// If pass is non-nil the saved password is replaced (an empty string removes it);
// if nil, any password already saved for this host is kept.
func AddHistory(e HostEntry, pass *string) error {
	historyMu.Lock()
	defer historyMu.Unlock()

	old := readHistory()
	entry := storedEntry{Host: e.Host, Port: e.Port, Login: e.Login, Encoding: e.Encoding, Jump: e.Jump}
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
	if len(list) > MaxHistory {
		list = list[:MaxHistory]
	}

	return writeHistory(list)
}

func writeHistory(list []storedEntry) error {
	p, err := HistoryFile()
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

// HistoryEntry returns what is saved for host:port.
func HistoryEntry(host string, port int) (storedEntry, bool) {
	historyMu.Lock()
	defer historyMu.Unlock()
	for _, h := range readHistory() {
		if h.Host == host && h.Port == port {
			return h, true
		}
	}
	return storedEntry{}, false
}

// HistoryForwards returns the port forwarding rules saved for host:port.
func HistoryForwards(host string, port int) []Forward {
	return PrefsFor(host, port).Forwards
}

// SetHistoryForwards saves the port forwarding rules of host:port.
func SetHistoryForwards(host string, port int, list []Forward) error {
	return UpdatePrefs(host, port, func(p *ServerPrefs) { p.Forwards = list })
}
