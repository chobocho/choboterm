package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// SavedSession is a connection the user saved by name, shown in groups at
// the top of the Host list. Unlike the history it is never dropped.
// Pass is filled only when the user chose to save the password.
type SavedSession struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group"` // one level, "" = top level
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login"`
	Encoding string `json:"encoding"`
	Pass     string `json:"pass"`
}

// storedSession is the on-disk form: the password is DPAPI-encrypted, never plain.
type storedSession struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group,omitempty"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	PassEnc  string `json:"passEnc,omitempty"`
}

// serverPrefs are the settings of one server (host:port), shared by every
// session and history entry that connects there. They used to live in the
// history, where they were lost when the host dropped out of the last 20.
type serverPrefs struct {
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	Forwards []Forward `json:"forwards,omitempty"` // SSH port forwarding rules
	Theme    string    `json:"theme,omitempty"`    // "" = the theme in Settings
}

type sessionsDoc struct {
	Sessions []storedSession `json:"sessions"`
	Servers  []serverPrefs   `json:"servers"`
}

var sessionsMu sync.Mutex

// sessionsFile sits next to hosts.json, so tests that move the history
// move this file too.
var sessionsFile = func() (string, error) {
	p, err := historyFile()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "sessions.json"), nil
}

func readSessions() sessionsDoc {
	var doc sessionsDoc
	p, err := sessionsFile()
	if err != nil {
		return doc
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return doc
	}
	_ = json.Unmarshal(data, &doc)
	return doc
}

func writeSessions(doc sessionsDoc) error {
	p, err := sessionsFile()
	if err != nil {
		return err
	}
	if doc.Sessions == nil {
		doc.Sessions = []storedSession{}
	}
	if doc.Servers == nil {
		doc.Servers = []serverPrefs{}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(p, data)
}

// writeFileAtomic replaces p in one step, so another choboterm reading it
// never sees half a file.
func writeFileAtomic(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// updateSessions reads the file, lets change edit it and writes it back,
// all under the lock (the file is re-read so other windows' edits are kept).
func updateSessions(change func(*sessionsDoc) error) error {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	doc := readSessions()
	if err := change(&doc); err != nil {
		return err
	}
	return writeSessions(doc)
}

func newSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sortSessions orders by group (top level first), then by name.
func sortSessions(list []SavedSession) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.Group != b.Group {
			if a.Group == "" || b.Group == "" {
				return a.Group == ""
			}
			return strings.ToLower(a.Group) < strings.ToLower(b.Group)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

func loadSessions() []SavedSession {
	sessionsMu.Lock()
	doc := readSessions()
	sessionsMu.Unlock()
	list := make([]SavedSession, 0, len(doc.Sessions))
	for _, s := range doc.Sessions {
		list = append(list, SavedSession{
			ID: s.ID, Name: s.Name, Group: s.Group, Host: s.Host, Port: s.Port,
			Login: s.Login, Encoding: s.Encoding, Pass: decryptPass(s.PassEnc),
		})
	}
	sortSessions(list)
	return list
}

// saveSession adds s (empty ID) or replaces the session with its ID, and
// returns it as saved. The password is kept only if savePass is set.
func saveSession(s SavedSession, savePass bool) (SavedSession, error) {
	s.Host = strings.TrimSpace(s.Host)
	s.Name = strings.TrimSpace(s.Name)
	s.Group = strings.TrimSpace(strings.ReplaceAll(s.Group, "/", " "))
	if s.Host == "" {
		return s, errors.New("Host를 입력하세요")
	}
	if s.Name == "" {
		s.Name = s.Host
	}
	if !savePass {
		s.Pass = ""
	}
	err := updateSessions(func(doc *sessionsDoc) error {
		if s.ID == "" {
			s.ID = newSessionID()
		}
		st := storedSession{
			ID: s.ID, Name: s.Name, Group: s.Group, Host: s.Host, Port: s.Port,
			Login: s.Login, Encoding: s.Encoding, PassEnc: encryptPass(s.Pass),
		}
		for i := range doc.Sessions {
			if doc.Sessions[i].ID == s.ID {
				doc.Sessions[i] = st
				return nil
			}
		}
		doc.Sessions = append(doc.Sessions, st)
		// A new session takes over what was set for its server in the history.
		ensureServerPrefs(doc, s.Host, s.Port)
		return nil
	})
	return s, err
}

func deleteSession(id string) error {
	return updateSessions(func(doc *sessionsDoc) error {
		for i := range doc.Sessions {
			if doc.Sessions[i].ID == id {
				doc.Sessions = append(doc.Sessions[:i], doc.Sessions[i+1:]...)
				return nil
			}
		}
		return nil
	})
}

// ensureServerPrefs returns the prefs of host:port in doc, adding them
// (from the old place, the history entry) if they are not there yet.
func ensureServerPrefs(doc *sessionsDoc, host string, port int) *serverPrefs {
	for i := range doc.Servers {
		if doc.Servers[i].Host == host && doc.Servers[i].Port == port {
			return &doc.Servers[i]
		}
	}
	h, _ := historyEntry(host, port)
	doc.Servers = append(doc.Servers, serverPrefs{Host: host, Port: port, Forwards: h.Forwards, Theme: h.Theme})
	return &doc.Servers[len(doc.Servers)-1]
}

// prefsFor returns what is saved for the server host:port.
func prefsFor(host string, port int) serverPrefs {
	sessionsMu.Lock()
	doc := readSessions()
	sessionsMu.Unlock()
	for _, s := range doc.Servers {
		if s.Host == host && s.Port == port {
			return s
		}
	}
	h, _ := historyEntry(host, port)
	return serverPrefs{Host: host, Port: port, Forwards: h.Forwards, Theme: h.Theme}
}

// updatePrefs changes the prefs of the server host:port.
func updatePrefs(host string, port int, change func(*serverPrefs)) error {
	return updateSessions(func(doc *sessionsDoc) error {
		change(ensureServerPrefs(doc, host, port))
		return nil
	})
}

// GetSessions returns the saved sessions, sorted by group and name.
func (a *App) GetSessions() []SavedSession {
	return loadSessions()
}

// SaveSession adds or updates a saved session and returns it as saved.
func (a *App) SaveSession(s SavedSession, savePass bool) (SavedSession, error) {
	return saveSession(s, savePass)
}

// DeleteSession removes a saved session.
func (a *App) DeleteSession(id string) error {
	return deleteSession(id)
}
