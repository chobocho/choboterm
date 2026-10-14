package app

// The app as the web server sees it (web.Host): the server knows pages,
// sockets and files; what a page's call or its going away means for the
// tabs is decided here.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"

	"choboterm/internal/dbg"
	"choboterm/internal/web"
)

// showWindowHook shows the window (tests replace it: no real window there).
var showWindowHook = (*App).showWindow

// appHost is the web.Host of an App: a is the window's view of the state
// (no caller).
type appHost struct{ a *App }

// startWeb starts the web server for a's state and page (the frontend/dist
// folder). The returned server is also stored in a.web.
func startWeb(a *App, page fs.FS, opts web.Options) (*web.Server, error) {
	w, err := web.Start(appHost{&App{appState: a.appState}}, page, opts)
	if err != nil {
		return nil, err
	}
	a.web.Store(w)
	return w, nil
}

// Invoke runs the call as an App whose caller is the page, so methods can
// tell who called (see App.caller).
func (h appHost) Invoke(c *web.Client, method string, args []json.RawMessage) (interface{}, error) {
	return webInvoke(&App{appState: h.a.appState, caller: c}, method, args)
}

// ClientClosed cancels the page's prompts (nobody will answer them) and
// closes its tabs.
func (h appHost) ClientClosed(id int) {
	cancelPrompts(func(tab int) bool { return web.ClientOf(tab) == id })
	a := h.a
	a.mu.Lock()
	var ids []int
	for tab := range a.tabs {
		if web.ClientOf(tab) == id {
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

func (h appHost) ServerStopped(w *web.Server) { h.a.web.CompareAndSwap(w, nil) }

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// webMethods are the methods a page may call: those Wails binds.
var webMethods = func() map[string]reflect.Method {
	m := map[string]reflect.Method{}
	t := reflect.TypeOf(&App{})
	for i := 0; i < t.NumMethod(); i++ {
		m[t.Method(i).Name] = t.Method(i)
	}
	return m
}()

// webInvoke calls method on a with JSON arguments, the way Wails does: a
// missing argument is the zero value, a trailing error result becomes the
// error, and other results are returned as they are.
func webInvoke(a *App, method string, args []json.RawMessage) (result interface{}, err error) {
	m, ok := webMethods[method]
	if !ok {
		return nil, fmt.Errorf("unknown method %q", method)
	}
	ft := m.Type
	in := []reflect.Value{reflect.ValueOf(a)}
	for i := 1; i < ft.NumIn(); i++ {
		v := reflect.New(ft.In(i))
		if i-1 < len(args) && len(args[i-1]) > 0 && string(args[i-1]) != "null" {
			if err := json.Unmarshal(args[i-1], v.Interface()); err != nil {
				return nil, fmt.Errorf("%s: argument %d: %w", method, i, err)
			}
		}
		in = append(in, v.Elem())
	}
	defer func() {
		if r := recover(); r != nil {
			dbg.Printf("web: %s panicked: %v", method, r)
			result, err = nil, fmt.Errorf("%s: %v", method, r)
		}
	}()
	out := m.Func.Call(in)
	if n := len(out); n > 0 && ft.Out(n-1) == errorType {
		if e := out[n-1].Interface(); e != nil {
			return nil, e.(error)
		}
		out = out[:n-1]
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out[0].Interface(), nil
}
