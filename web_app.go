package main

// The web server as seen from the window: Settings > Web starts and stops
// it, and closing the window while browsers are connected hides it instead.

import (
	"context"
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
func (a *App) startWebServer(opts webOptions) error {
	if webPage == nil {
		return errors.New("no page to serve")
	}
	if w := a.web.Load(); w != nil {
		w.stop()
	}
	w, err := startWeb(a, webPage, opts)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.onIdle = a.webIdle
	w.mu.Unlock()
	return nil
}

// windowTabsConnected reports whether a tab of the window (not of a web
// page) has a connection open.
func (a *App) windowTabsConnected() bool {
	return a.connectedWhere(func(id int) bool { return clientOf(id) == windowClient })
}

// hideForWeb is beforeClose with browsers still connected: the window's tabs
// are closed and the window hides, while the web server keeps serving.
// It reports whether the window was hidden.
func (a *App) hideForWeb(ctx context.Context) bool {
	w := a.web.Load()
	if w == nil || w.clientCount() == 0 || ctx == nil {
		return false
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
	a.quit.Store(false)
	runtime.WindowHide(ctx)
	debugf("web: window hidden; %d page(s) still open", w.clientCount())
	return true
}

// webIdle runs when the last browser page went away: a window hidden for
// the browsers now quits.
func (a *App) webIdle() {
	if a.hidden.Load() && a.ctx != nil {
		a.quit.Store(true)
		runtime.Quit(a.ctx)
	}
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
