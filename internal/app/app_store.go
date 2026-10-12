package app

import (
	"errors"
	"os"

	"choboterm/internal/i18n"
	"choboterm/internal/store"
)

// TabTheme returns the color theme saved for the server of a tab's
// connection ("" = the theme in Settings).
func (a *App) TabTheme(tabID int) string {
	host, port, ok := a.tabTarget(tabID)
	if !ok {
		return ""
	}
	return store.PrefsFor(host, port).Theme
}

// SetTabTheme saves a color theme for the server of a tab's connection
// ("" = back to the theme in Settings). It reports false if the tab has
// never been connected, so there is no server to save it for.
func (a *App) SetTabTheme(tabID int, theme string) (bool, error) {
	host, port, ok := a.tabTarget(tabID)
	if !ok {
		return false, nil
	}
	return true, store.UpdatePrefs(host, port, func(p *store.ServerPrefs) { p.Theme = theme })
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

// GetSessions returns the saved sessions, sorted by group and name.
func (a *App) GetSessions() []store.SavedSession {
	return store.LoadSessions()
}

// SaveSession adds or updates a saved session and returns it as saved.
func (a *App) SaveSession(s store.SavedSession, savePass bool) (store.SavedSession, error) {
	return store.SaveSession(s, savePass)
}

// DeleteSession removes a saved session.
func (a *App) DeleteSession(id string) error {
	return store.DeleteSession(id)
}

// ExportSessions asks for a file and writes the saved sessions (without
// passwords) to it. It returns the path, or "" if the user cancelled.
func (a *App) ExportSessions() (string, error) {
	p, finish, err := a.picker().saveFile(pickOpts{
		Title:   i18n.Tr("세션 내보내기"),
		Name:    "choboterm-sessions.json",
		Filters: []fileFilter{{Name: i18n.Tr("세션 파일 (*.json)"), Pattern: "*.json"}},
	})
	if err != nil || p == "" {
		return "", err
	}
	data, err := store.EncodeExport(store.LoadSessions())
	if err == nil {
		err = os.WriteFile(p, data, 0o644)
	}
	if err != nil {
		finish(false)
		return "", err
	}
	return finish(true), nil
}

// ImportSessions asks for a file written by ExportSessions and adds its
// sessions. Cancelled = zero result.
func (a *App) ImportSessions() (store.ImportResult, error) {
	p, release, err := a.picker().openFile(pickOpts{
		Title:   i18n.Tr("세션 가져오기"),
		Filters: []fileFilter{{Name: i18n.Tr("세션 파일 (*.json)"), Pattern: "*.json"}},
	})
	defer release()
	if err != nil || p == "" {
		return store.ImportResult{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return store.ImportResult{}, err
	}
	list, err := store.DecodeExport(data)
	if err != nil {
		return store.ImportResult{}, err
	}
	return store.AddSessions(list)
}

// ImportPuTTY adds the SSH and Telnet sessions saved in PuTTY, in the group "PuTTY".
func (a *App) ImportPuTTY() (store.ImportResult, error) {
	list, err := store.PuttySessions()
	if err != nil {
		return store.ImportResult{}, err
	}
	if len(list) == 0 {
		return store.ImportResult{}, errors.New("PuTTY에 저장된 SSH / Telnet 세션이 없습니다")
	}
	return store.AddSessions(list)
}
