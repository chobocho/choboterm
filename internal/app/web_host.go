package app

// The app as the web server sees it (webHost): the server knows pages,
// sockets and files; what a page's call or its going away means for the
// tabs is decided here.

import (
	"encoding/json"
	"io/fs"
)

// webHost is what the web server needs of the app.
type webHost interface {
	// Invoke runs a method of App for page c, the way Wails would.
	Invoke(c *webClient, method string, args []json.RawMessage) (interface{}, error)
	// ClientClosed ends what page id left behind: its prompts and tabs.
	ClientClosed(id int)
	// HasWindow reports whether there is a desktop window (pages offer
	// window features only then, and only it can be shown).
	HasWindow() bool
	// ShowWindow brings the window to the front (choboterm started again).
	ShowWindow()
	// ServerStopped is called once w has stopped.
	ServerStopped(w *webServer)
}

// showWindowHook shows the window (tests replace it: no real window there).
var showWindowHook = (*App).showWindow

// appHost is the webHost of an App: a is the window's view of the state
// (no caller).
type appHost struct{ a *App }

// startWeb starts the web server for a's state and page (the frontend/dist
// folder). The returned server is also stored in a.web.
func startWeb(a *App, page fs.FS, opts webOptions) (*webServer, error) {
	w, err := serveWeb(appHost{&App{appState: a.appState}}, page, opts)
	if err != nil {
		return nil, err
	}
	a.web.Store(w)
	return w, nil
}

// Invoke runs the call as an App whose caller is the page, so methods can
// tell who called (see App.caller).
func (h appHost) Invoke(c *webClient, method string, args []json.RawMessage) (interface{}, error) {
	return webInvoke(&App{appState: h.a.appState, caller: c}, method, args)
}

// ClientClosed cancels the page's prompts (nobody will answer them) and
// closes its tabs.
func (h appHost) ClientClosed(id int) {
	cancelPrompts(func(tab int) bool { return clientOf(tab) == id })
	a := h.a
	a.mu.Lock()
	var ids []int
	for tab := range a.tabs {
		if clientOf(tab) == id {
			ids = append(ids, tab)
		}
	}
	a.mu.Unlock()
	for _, tab := range ids {
		a.CloseTab(tab)
	}
}

func (h appHost) HasWindow() bool { return h.a.ctx != nil }

func (h appHost) ShowWindow() { showWindowHook(h.a) }

func (h appHost) ServerStopped(w *webServer) { h.a.web.CompareAndSwap(w, nil) }
