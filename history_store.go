package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// HostEntry is one remembered connection. Passwords are never stored.
type HostEntry struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Encoding string `json:"encoding"`
}

const maxHistory = 20

var historyMu sync.Mutex

func historyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "choboterm", "hosts.json"), nil
}

func loadHistory() []HostEntry {
	historyMu.Lock()
	defer historyMu.Unlock()
	return readHistory()
}

func readHistory() []HostEntry {
	p, err := historyPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var list []HostEntry
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// addHistory moves e to the top of the list (most recent first).
func addHistory(e HostEntry) error {
	historyMu.Lock()
	defer historyMu.Unlock()

	list := []HostEntry{e}
	for _, h := range readHistory() {
		if h.Host != e.Host || h.Port != e.Port {
			list = append(list, h)
		}
	}
	if len(list) > maxHistory {
		list = list[:maxHistory]
	}

	p, err := historyPath()
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
