package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// sessionsExport is the file written by "내보내기". Passwords are never in it:
// they are encrypted for this Windows user only and would be useless elsewhere.
type sessionsExport struct {
	Format   string          `json:"format"`
	Version  int             `json:"version"`
	Sessions []exportSession `json:"sessions"`
}

type exportSession struct {
	Name     string `json:"name"`
	Group    string `json:"group,omitempty"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Login    string `json:"login,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	Jump     string `json:"jump,omitempty"`
}

const exportFormat = "choboterm-sessions"

// ImportResult says how many sessions an import added, and how many it left
// out because the same session was already saved.
type ImportResult struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

func encodeExport(list []SavedSession) ([]byte, error) {
	out := sessionsExport{Format: exportFormat, Version: 1, Sessions: []exportSession{}}
	for _, s := range list {
		out.Sessions = append(out.Sessions, exportSession{
			Name: s.Name, Group: s.Group, Host: s.Host, Port: s.Port, Login: s.Login, Encoding: s.Encoding, Jump: s.Jump,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func decodeExport(data []byte) ([]SavedSession, error) {
	var in sessionsExport
	if err := json.Unmarshal(data, &in); err != nil || in.Format != exportFormat {
		return nil, errors.New("choboterm에서 내보낸 세션 파일이 아닙니다")
	}
	list := make([]SavedSession, 0, len(in.Sessions))
	for _, s := range in.Sessions {
		list = append(list, SavedSession{
			Name: s.Name, Group: s.Group, Host: s.Host, Port: s.Port, Login: s.Login, Encoding: s.Encoding, Jump: s.Jump,
		})
	}
	return list, nil
}

// addSessions saves the sessions of an import, leaving out the ones that are
// already saved (same name, group and connection).
func addSessions(list []SavedSession) (ImportResult, error) {
	var r ImportResult
	key := func(s SavedSession) string {
		if s.Encoding == "" {
			s.Encoding = EncodingUTF8
		}
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", s.Name, s.Group, s.Host, s.Port, s.Login, s.Encoding)
	}
	have := map[string]bool{}
	for _, s := range loadSessions() {
		have[key(s)] = true
	}
	for _, s := range list {
		s.ID, s.Pass = "", ""
		if strings.TrimSpace(s.Host) == "" {
			continue
		}
		if s.Name == "" {
			s.Name = s.Host
		}
		if s.Encoding == "" {
			s.Encoding = EncodingUTF8
		}
		if have[key(s)] {
			r.Skipped++
			continue
		}
		if _, err := saveSession(s, false); err != nil {
			return r, err
		}
		have[key(s)] = true
		r.Added++
	}
	return r, nil
}

// ExportSessions asks for a file and writes the saved sessions (without
// passwords) to it. It returns the path, or "" if the user cancelled.
func (a *App) ExportSessions() (string, error) {
	p, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "세션 내보내기",
		DefaultFilename: "choboterm-sessions.json",
		Filters:         []runtime.FileFilter{{DisplayName: "세션 파일 (*.json)", Pattern: "*.json"}},
	})
	if err != nil || p == "" {
		return "", err
	}
	data, err := encodeExport(loadSessions())
	if err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0o644)
}

// ImportSessions asks for a file written by ExportSessions and adds its
// sessions. Cancelled = zero result.
func (a *App) ImportSessions() (ImportResult, error) {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "세션 가져오기",
		Filters: []runtime.FileFilter{{DisplayName: "세션 파일 (*.json)", Pattern: "*.json"}},
	})
	if err != nil || p == "" {
		return ImportResult{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ImportResult{}, err
	}
	list, err := decodeExport(data)
	if err != nil {
		return ImportResult{}, err
	}
	return addSessions(list)
}

// ImportPuTTY adds the SSH and Telnet sessions saved in PuTTY, in the group "PuTTY".
func (a *App) ImportPuTTY() (ImportResult, error) {
	list, err := puttySessions()
	if err != nil {
		return ImportResult{}, err
	}
	if len(list) == 0 {
		return ImportResult{}, errors.New("PuTTY에 저장된 SSH / Telnet 세션이 없습니다")
	}
	return addSessions(list)
}

// puttyValues are the settings of one PuTTY session that choboterm uses.
type puttyValues struct {
	HostName, Protocol, UserName, LineCodePage string
	PortNumber                                 int
}

// fromPuTTY turns a PuTTY session (its registry key name and values) into a
// saved session; ok is false for ones choboterm can't open (serial, raw...).
func fromPuTTY(key string, v puttyValues) (SavedSession, bool) {
	host := strings.TrimSpace(v.HostName)
	login := v.UserName
	if at := strings.LastIndex(host, "@"); at >= 0 { // "user@host" in the host field
		if login == "" {
			login = host[:at]
		}
		host = host[at+1:]
	}
	if host == "" {
		return SavedSession{}, false
	}
	port := v.PortNumber
	switch v.Protocol {
	case "ssh":
		if port <= 0 {
			port = 22
		}
	case "telnet":
		if port <= 0 {
			port = 23
		}
	default:
		return SavedSession{}, false
	}
	name, err := url.PathUnescape(key) // PuTTY writes "my server" as "my%20server"
	if err != nil {
		name = key
	}
	enc := EncodingUTF8
	cp := strings.ToUpper(v.LineCodePage)
	if strings.Contains(cp, "949") || strings.Contains(cp, "EUC-KR") || strings.Contains(cp, "KS_C") {
		enc = EncodingEUCKR
	}
	return SavedSession{Name: name, Group: "PuTTY", Host: host, Port: port, Login: login, Encoding: enc}, true
}
