package app

import (
	"errors"
	"fmt"
	"os"

	"choboterm/internal/config"
	"choboterm/internal/i18n"
	"choboterm/internal/osutil"
	"choboterm/internal/sessionlog"
)

// startLog turns logging on for the tab (no-op when it already is).
func (t *tab) startLog() (string, error) {
	t.mu.Lock()
	host, port := t.host, t.port
	t.mu.Unlock()
	s := config.Load()
	p, err := t.log.Start(s.LogDir, host, port, !s.LogRaw, s.LogTime)
	if err != nil || p == "" {
		return t.log.Active(), err
	}
	if t.session() != nil {
		t.log.Mark(fmt.Sprintf("로그 시작: %s:%d", host, port))
	}
	t.emit("log:changed", p)
	return p, nil
}

func (t *tab) stopLog() {
	if t.log.Stop() != "" {
		t.emit("log:changed", "")
	}
}

// StartLog starts writing the tab's output to a new file in the log folder
// and returns its path.
func (a *App) StartLog(tabID int) (string, error) {
	return a.getTab(tabID).startLog()
}

// StopLog stops logging in a tab.
func (a *App) StopLog(tabID int) {
	if t := a.findTab(tabID); t != nil {
		t.stopLog()
	}
}

// LogPath returns the tab's log file, or "" when it isn't logging.
func (a *App) LogPath(tabID int) string {
	if t := a.findTab(tabID); t != nil {
		return t.log.Active()
	}
	return ""
}

// ShowLogs opens the tab's log file selected in Explorer (its folder on
// Linux), or the log folder.
func (a *App) ShowLogs(tabID int) error {
	if !a.local() {
		return errNotInBrowser // Explorer would open on the other PC
	}
	if p := a.LogPath(tabID); p != "" {
		return osutil.ShowFile(p)
	}
	dir := config.Load().LogDir
	if dir == "" {
		dir = sessionlog.LogsDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return osutil.OpenFolder(dir)
}

// ChooseLogDir asks for the log folder; "" means the dialog was cancelled.
func (a *App) ChooseLogDir(current string) (string, error) {
	if current == "" {
		current = sessionlog.LogsDir()
	}
	if st, err := os.Stat(current); err != nil || !st.IsDir() {
		current = ""
	}
	dir, err := a.picker().Folder(pickOpts{
		Title: i18n.Tr("로그 폴더 선택"),
		Dir:   current,
	})
	if err != nil {
		return "", errors.New("폴더를 고르지 못했습니다: " + err.Error())
	}
	return dir, nil
}
