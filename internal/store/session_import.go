package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"choboterm/internal/codec"
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

func EncodeExport(list []SavedSession) ([]byte, error) {
	out := sessionsExport{Format: exportFormat, Version: 1, Sessions: []exportSession{}}
	for _, s := range list {
		out.Sessions = append(out.Sessions, exportSession{
			Name: s.Name, Group: s.Group, Host: s.Host, Port: s.Port, Login: s.Login, Encoding: s.Encoding, Jump: s.Jump,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func DecodeExport(data []byte) ([]SavedSession, error) {
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

// AddSessions saves the sessions of an import, leaving out the ones that are
// already saved (same name, group and connection).
func AddSessions(list []SavedSession) (ImportResult, error) {
	var r ImportResult
	key := func(s SavedSession) string {
		if s.Encoding == "" {
			s.Encoding = codec.UTF8
		}
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", s.Name, s.Group, s.Host, s.Port, s.Login, s.Encoding)
	}
	have := map[string]bool{}
	for _, s := range LoadSessions() {
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
			s.Encoding = codec.UTF8
		}
		if have[key(s)] {
			r.Skipped++
			continue
		}
		if _, err := SaveSession(s, false); err != nil {
			return r, err
		}
		have[key(s)] = true
		r.Added++
	}
	return r, nil
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
	enc := codec.UTF8
	cp := strings.ToUpper(v.LineCodePage)
	if strings.Contains(cp, "949") || strings.Contains(cp, "EUC-KR") || strings.Contains(cp, "KS_C") {
		enc = codec.EUCKR
	}
	return SavedSession{Name: name, Group: "PuTTY", Host: host, Port: port, Login: login, Encoding: enc}, true
}
