package main

// The web server as seen from the window: Settings > Web starts and stops
// it, and closing the window while browsers are connected hides it instead.

import (
	"errors"
	"io/fs"
	"net"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// webPage is the page the web server serves (frontend/dist, set by main).
var webPage fs.FS

// errWindowOnly is returned when a web page calls what only the window may.
var errWindowOnly = errors.New("choboterm 창에서만 할 수 있습니다")

// WebInfo describes the web server for Settings > Web.
type WebInfo struct {
	Running     bool   `json:"running"`
	Enabled     bool   `json:"enabled"` // starts with the window
	Addr        string `json:"addr"`    // the address in the settings
	URL         string `json:"url"`     // to open in a browser, with the token
	TLS         bool   `json:"tls"`
	Fingerprint string `json:"fingerprint"` // of the TLS certificate
	Remote      bool   `json:"remote"`      // other PCs can connect
	Clients     int    `json:"clients"`     // pages open now
	Error       string `json:"error"`       // why it didn't start (e.g. the port is in use)
}

// WebStatus returns the web server's state.
func (a *App) WebStatus() WebInfo {
	info := WebInfo{Addr: webDefaultAddr}
	if s := loadSettings().Web; s != nil {
		info.Enabled = s.Enabled
		if s.Addr != "" {
			info.Addr = s.Addr
		}
	}
	if w := a.web.Load(); w != nil {
		info.Running, info.Addr = true, w.opts.Addr
		info.URL, info.TLS, info.Fingerprint = w.URL(false), w.tls, w.fp
		info.Clients = w.clientCount()
	}
	if host, _, err := net.SplitHostPort(info.Addr); err == nil {
		info.Remote = !loopbackHost(host)
	}
	if e := a.webErr.Load(); e != nil && !info.Running {
		info.Error = *e
	}
	return info
}

// WebStart (re)starts the web server on addr ("" = this PC only) and starts
// it with the window from now on.
func (a *App) WebStart(addr string) (WebInfo, error) {
	if a.caller != nil {
		return WebInfo{}, errWindowOnly
	}
	if err := a.startWebServer(webOptions{Addr: addr}); err != nil {
		return a.WebStatus(), err
	}
	_ = updateSettings(func(s *Settings) { s.Web = &WebSettings{Enabled: true, Addr: addr} })
	return a.WebStatus(), nil
}

// WebStop stops the web server (closing the browsers' tabs) and no longer
// starts it with the window.
func (a *App) WebStop() error {
	if a.caller != nil {
		return errWindowOnly
	}
	if w := a.web.Load(); w != nil {
		w.stop()
	}
	a.webErr.Store(nil)
	a.emitWebStatus()
	return updateSettings(func(s *Settings) {
		if s.Web != nil {
			s.Web.Enabled = false
		}
	})
}

// WebOpen opens the page in this PC's browser, which then gets the
// window's file dialogs.
func (a *App) WebOpen() error {
	if a.caller != nil {
		return errWindowOnly
	}
	w := a.web.Load()
	if w == nil {
		return errors.New("웹 서버가 꺼져 있습니다")
	}
	return openURL(w.localURL())
}

// WebNewToken makes a new access token; every browser has to log in again.
func (a *App) WebNewToken() (WebInfo, error) {
	if a.caller != nil {
		return WebInfo{}, errWindowOnly
	}
	if _, err := webToken(true); err != nil {
		return a.WebStatus(), err
	}
	if w := a.web.Load(); w != nil {
		if err := a.startWebServer(w.opts); err != nil {
			return a.WebStatus(), err
		}
	}
	return a.WebStatus(), nil
}

// startWebServer replaces the running web server (if any) with a new one.
// A failure is kept for Settings to show (see WebStatus).
func (a *App) startWebServer(opts webOptions) error {
	if webPage == nil {
		return errors.New("no page to serve")
	}
	if w := a.web.Load(); w != nil {
		w.stop()
	}
	w, err := startWeb(a, webPage, opts)
	if err != nil {
		msg := err.Error()
		a.webErr.Store(&msg)
		a.emitWebStatus()
		return err
	}
	a.webErr.Store(nil)
	w.mu.Lock()
	w.onChange = func(int) { a.emitWebStatus() }
	w.mu.Unlock()
	a.emitWebStatus()
	return nil
}

// emitWebStatus tells the window whether the web server runs and how many
// browser pages use it (shown in the window title).
func (a *App) emitWebStatus() {
	if a.ctx == nil && a.hooks.emit == nil {
		return
	}
	running, clients := false, 0
	if w := a.web.Load(); w != nil {
		running, clients = true, w.connectedCount()
	}
	a.emit("web:status", running, clients)
}

// windowTabsConnected reports whether a tab of the window (not of a web
// page) has a connection open.
func (a *App) windowTabsConnected() bool {
	return a.connectedWhere(func(id int) bool { return clientOf(id) == windowClient })
}

// WebHideWindow closes the window's tabs and hides the window while the web
// server keeps serving the browsers: the user chose that when closing the
// window (app:closeWeb). Starting choboterm again shows it (see handoff.go).
func (a *App) WebHideWindow() error {
	if a.caller != nil {
		return errWindowOnly
	}
	if a.ctx == nil || a.web.Load() == nil {
		return errors.New("웹 서버가 꺼져 있습니다")
	}
	a.mu.Lock()
	var ids []int
	for id := range a.tabs {
		if clientOf(id) == windowClient {
			ids = append(ids, id)
		}
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.CloseTab(id)
	}
	a.hidden.Store(true)
	runtime.WindowHide(a.ctx)
	debugf("web: window hidden; %d page(s) connected", a.web.Load().connectedCount())
	return nil
}

// showWindow brings the window back (choboterm started again): a hidden
// one starts over with a fresh page, as its tabs were closed.
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	if a.hidden.Swap(false) {
		runtime.WindowReloadApp(a.ctx)
	}
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}
