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
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Encoding string `json:"encoding"`
	Pass     string `json:"pass"`
}

// storedEntry is the on-disk form: the password is DPAPI-encrypted, never plain.
type storedEntry struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Encoding string `json:"encoding"`
	PassEnc  string `json:"passEnc,omitempty"`
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
