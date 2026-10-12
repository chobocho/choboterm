package main

// The web server (choboterm --web, or Settings > Web): the same page as the
// window, served to browsers. Calls and events go over one WebSocket per page
// (web_client.go); file dialogs become uploads and downloads for browsers on
// other PCs (web_files.go). The window keeps working as before.
//
// Tab ids: the page picks them. A web page gets the base clientID<<tabBits
// and numbers its tabs base+1, base+2, ...; the window's base is 0, so the
// window's tab ids are unchanged and clientOf(tab) says whose tab it is.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"choboterm/frontend"
	"choboterm/internal/config"
	"choboterm/internal/dbg"
	"choboterm/internal/i18n"
)

const (
	tabBits      = 20 // up to a million tabs per page
	windowClient = 0
	// maxClientID keeps tab ids within 32 bits (the output frame's tab field).
	maxClientID = 1<<(32-tabBits) - 1
	// webDefaultAddr is where the server listens unless told otherwise:
	// this PC only.
	webDefaultAddr = "127.0.0.1:7681"
)

// clientOf returns the web client that owns tab tabID, or windowClient.
func clientOf(tabID int) int {
	if tabID < 1<<tabBits {
		return windowClient
	}
	return tabID >> tabBits
}

// webOptions configures the web server.
type webOptions struct {
	Addr     string // host:port; "" = webDefaultAddr
	CertFile string // TLS certificate and key; "" = self-signed when needed
	KeyFile  string
	Insecure bool // plain HTTP even when other PCs can connect
	NoToken  bool // disable login-token authentication (Termux-only option)
	// Grace is how long a page may stay disconnected (network trouble, a
	// sleeping laptop) before its tabs are closed. 0 = webGrace.
	Grace time.Duration
}

// webGrace is the default time a disconnected page keeps its tabs. Browsers
// may suspend background pages for a few minutes, so allow time to reconnect.
const webGrace = 10 * time.Minute

// webWrongTokenDelay slows down guessing the token (tests shorten it).
var webWrongTokenDelay = time.Second

// webServer serves the page and its WebSocket.
type webServer struct {
	app  *App // the shared state, as the window (caller nil)
	opts webOptions
	srv  *http.Server
	ln   net.Listener
	tls  bool
	fp   string // SHA-256 fingerprint of the certificate, to compare in the browser
	page fs.FS  // frontend/dist
	// index is the page's index.html, split where the client's script goes.
	indexHead, indexRest []byte
	token                string // the access token (web-token file)
	cookie, localCookie  string // cookie names (per port, so two servers don't mix)
	localKey             string // the local cookie's value
	stageRoot            string // temporary files of uploads and downloads

	mu        sync.Mutex
	clients   map[int]*webClient
	nextID    int
	codes     map[string]time.Time // one-time codes that mark a browser as local
	stages    map[string]*webStage
	downloads map[string]*webDownload
	closed    bool
	// onChange runs when a page was loaded or went away, with how many are
	// open now (the window shows it in its title).
	onChange func(clients int)
}

// startWeb starts the web server for a's state and page (the frontend/dist
// folder). The returned server is also stored in a.web.
func startWeb(a *App, page fs.FS, opts webOptions) (*webServer, error) {
	if opts.Addr == "" {
		opts.Addr = webDefaultAddr
	}
	if opts.Grace <= 0 {
		opts.Grace = webGrace
	}
	host, _, err := net.SplitHostPort(opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("주소가 올바르지 않습니다 (예: %s): %w", webDefaultAddr, err)
	}
	index, err := fs.ReadFile(page, "index.html")
	if err != nil {
		return nil, fmt.Errorf("페이지를 찾을 수 없습니다: %w", err)
	}
	var token string
	if opts.NoToken {
		if !frontend.Termux {
			return nil, errors.New("no-token mode is only supported in the Termux build")
		}
	} else {
		var err error
		token, err = webToken(false)
		if err != nil {
			return nil, err
		}
	}
	w := &webServer{
		app:       &App{appState: a.appState},
		opts:      opts,
		page:      page,
		token:     token,
		localKey:  randomHex(32),
		clients:   map[int]*webClient{},
		codes:     map[string]time.Time{},
		stages:    map[string]*webStage{},
		downloads: map[string]*webDownload{},
	}
	w.indexHead, w.indexRest = splitIndex(index)

	// Other PCs may connect: the token travels in the clear without TLS.
	var tlsCfg *tls.Config
	if opts.CertFile != "" || (!loopbackHost(host) && !opts.Insecure) {
		cert, fp, err := webCertificate(opts.CertFile, opts.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		w.tls, w.fp = true, fp
	}

	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("웹 서버를 시작하지 못했습니다: %w", err)
	}
	if tlsCfg != nil {
		ln = tls.NewListener(ln, tlsCfg)
	}
	w.ln = ln
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	w.cookie, w.localCookie = "cbt_"+port, "cbt_local_"+port
	w.stageRoot, err = os.MkdirTemp("", "choboterm-web-")
	if err != nil {
		ln.Close()
		return nil, err
	}
	w.srv = &http.Server{
		Handler:           w.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// No Read/WriteTimeout: uploads, downloads and WebSockets run long.
		ErrorLog: nil,
	}
	go func() { _ = w.srv.Serve(ln) }()
	a.web.Store(w)
	dbg.Printf("web: listening on %s", w.URL(false))
	return w, nil
}

// stop closes the server, every page's tabs and the temporary files.
func (w *webServer) stop() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	clients := make([]*webClient, 0, len(w.clients))
	for _, c := range w.clients {
		clients = append(clients, c)
	}
	w.mu.Unlock()
	w.app.web.CompareAndSwap(w, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = w.srv.Shutdown(ctx) // stops accepting; WebSockets are hijacked, closed below
	for _, c := range clients {
		c.close()
	}
	_ = os.RemoveAll(w.stageRoot)
}

// URL is the address to open in a browser, with the token unless bare.
func (w *webServer) URL(bare bool) string {
	scheme := "http"
	if w.tls {
		scheme = "https"
	}
	host, port, _ := net.SplitHostPort(w.ln.Addr().String())
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		// Listening on every address: show one other PCs can use.
		host = outboundIP()
	}
	u := scheme + "://" + net.JoinHostPort(host, port) + "/"
	if bare || w.opts.NoToken {
		return u
	}
	return u + "?token=" + url.QueryEscape(w.token)
}

// localURL is URL plus a one-time code that marks the browser opening it as
// one on this PC, which then gets the window's native file dialogs. Only
// this process hands out codes, to a browser it opens itself.
func (w *webServer) localURL() string {
	code := randomHex(16)
	w.mu.Lock()
	now := time.Now()
	for c, exp := range w.codes {
		if now.After(exp) {
			delete(w.codes, c)
		}
	}
	w.codes[code] = now.Add(2 * time.Minute)
	w.mu.Unlock()
	u := w.URL(false)
	if host, port, err := net.SplitHostPort(w.ln.Addr().String()); err == nil && !loopbackHost(host) {
		// Open the loopback address: the browser on this PC must arrive from it.
		scheme := "http"
		if w.tls {
			scheme = "https"
		}
		u = scheme + "://" + net.JoinHostPort("127.0.0.1", port) + "/"
		if !w.opts.NoToken {
			u += "?token=" + url.QueryEscape(w.token)
		}
	}
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + "local=" + code
}

// clientCount is the number of pages open now (including ones briefly
// disconnected, which keep their tabs for the grace time).
func (w *webServer) clientCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.clients)
}

// connectedCount is the number of pages connected right now: the browsers
// someone is looking at.
func (w *webServer) connectedCount() int {
	w.mu.Lock()
	clients := make([]*webClient, 0, len(w.clients))
	for _, c := range w.clients {
		clients = append(clients, c)
	}
	w.mu.Unlock()
	n := 0
	for _, c := range clients {
		c.mu.Lock()
		if c.conn != nil {
			n++
		}
		c.mu.Unlock()
	}
	return n
}

// changed tells onChange that the pages changed.
func (w *webServer) changed() {
	w.mu.Lock()
	f, closed := w.onChange, w.closed
	n := len(w.clients)
	w.mu.Unlock()
	if f != nil && !closed {
		go f(n)
	}
}

func (w *webServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", w.handleLogin)
	mux.HandleFunc("/web/show", w.handleShow)
	mux.HandleFunc("/web/shim.js", w.auth(w.handleShim))
	mux.HandleFunc("/web/ws", w.auth(w.handleWS))
	mux.HandleFunc("/web/stage", w.auth(w.handleStageNew))
	mux.HandleFunc("/web/stage/", w.auth(w.handleStage))
	mux.HandleFunc("/web/dl/", w.auth(w.handleDownload))
	mux.HandleFunc("/", w.handleRoot)
	return securityHeaders(mux)
}

// securityHeaders keeps the page out of frames and the token out of referrers.
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hd := rw.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Content-Security-Policy", "frame-ancestors 'none'")
		hd.Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(rw, r)
	})
}

// authed reports whether the request carries the access cookie.
func (w *webServer) authed(r *http.Request) bool {
	if w.opts.NoToken {
		return true
	}
	c, err := r.Cookie(w.cookie)
	return err == nil && w.tokenOK(c.Value)
}

func (w *webServer) tokenOK(s string) bool {
	return subtle.ConstantTimeCompare([]byte(s), []byte(w.token)) == 1
}

func (w *webServer) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if !w.authed(r) {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(rw, r)
	}
}

// isLocal reports whether the request comes from a browser on this PC that
// was opened by this process (it holds the local cookie). An address check
// alone isn't enough: ssh -L forwarding and reverse proxies also arrive from
// 127.0.0.1, and other users of this PC could connect too.
func (w *webServer) isLocal(r *http.Request) bool {
	if !loopbackRemote(r.RemoteAddr) {
		return false
	}
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return false // behind a proxy: the browser is elsewhere
		}
	}
	c, err := r.Cookie(w.localCookie)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(w.localKey)) == 1
}

func (w *webServer) setCookie(rw http.ResponseWriter, name, value string) {
	http.SetCookie(rw, &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, Secure: w.tls, SameSite: http.SameSiteStrictMode,
		MaxAge: 400 * 24 * 3600,
	})
}

// handleRoot serves the page (a new client each time it is loaded), takes
// the token from ?token=, and serves the page's files.
func (w *webServer) handleRoot(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		if !w.authed(r) {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
		rw.Header().Set("Cache-Control", "private, max-age=3600")
		http.FileServerFS(w.page).ServeHTTP(rw, r)
		return
	}
	q := r.URL.Query()
	if w.opts.NoToken {
		if code := q.Get("local"); code != "" && loopbackRemote(r.RemoteAddr) && w.useCode(code) {
			w.setCookie(rw, w.localCookie, w.localKey)
		}
	} else if tok := q.Get("token"); tok != "" {
		if !w.tokenOK(tok) {
			w.loginPage(rw, http.StatusUnauthorized, true)
			return
		}
		w.setCookie(rw, w.cookie, w.token)
		if code := q.Get("local"); code != "" && loopbackRemote(r.RemoteAddr) && w.useCode(code) {
			w.setCookie(rw, w.localCookie, w.localKey)
		}
		// Drop the token from the address bar and the history.
		http.Redirect(rw, r, "/", http.StatusSeeOther)
		return
	}
	if !w.authed(r) {
		w.loginPage(rw, http.StatusUnauthorized, false)
		return
	}
	c, err := w.newClient(w.isLocal(r))
	if err != nil {
		http.Error(rw, err.Error(), http.StatusServiceUnavailable)
		return
	}
	info, _ := json.Marshal(map[string]interface{}{
		"id": c.id, "key": c.key, "base": c.id << tabBits, "local": c.local,
		"platform": goruntime.GOOS, "arch": goruntime.GOARCH, "lang": i18n.Lang(),
		"window": w.app.ctx != nil,
	})
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	var b bytes.Buffer
	b.Write(w.indexHead)
	fmt.Fprintf(&b, "<script>window.__cbt=%s</script><script src=\"/web/shim.js\"></script>", info)
	b.Write(w.indexRest)
	_, _ = rw.Write(b.Bytes())
}

// useCode spends a one-time local code.
func (w *webServer) useCode(code string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	exp, ok := w.codes[code]
	delete(w.codes, code)
	return ok && time.Now().Before(exp)
}

// handleLogin takes the token typed into the login page.
func (w *webServer) handleLogin(rw http.ResponseWriter, r *http.Request) {
	if w.opts.NoToken {
		http.Redirect(rw, r, "/", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodPost {
		http.Redirect(rw, r, "/", http.StatusSeeOther)
		return
	}
	if !w.tokenOK(strings.TrimSpace(r.PostFormValue("token"))) {
		w.loginPage(rw, http.StatusUnauthorized, true)
		return
	}
	w.setCookie(rw, w.cookie, w.token)
	http.Redirect(rw, r, "/", http.StatusSeeOther)
}

func (w *webServer) loginPage(rw http.ResponseWriter, status int, wrong bool) {
	if wrong {
		time.Sleep(webWrongTokenDelay) // slow down guessing
	}
	msg := ""
	if wrong {
		msg = `<p class="err">` + i18n.Tr("토큰이 맞지 않습니다") + `</p>`
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.WriteHeader(status)
	fmt.Fprintf(rw, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>choboterm</title><style>
body{font:15px system-ui,sans-serif;background:#1e1e1e;color:#ddd;display:grid;place-items:center;min-height:100vh;margin:0}
form{background:#2b2b2b;padding:24px;border-radius:8px;width:min(360px,90vw)}
input{width:100%%;box-sizing:border-box;padding:8px;margin:12px 0;background:#111;color:#eee;border:1px solid #555;border-radius:4px}
button{padding:8px 16px}.err{color:#f66}p{margin:0 0 8px}</style></head>
<body><form method="post" action="/login"><p><b>choboterm</b></p><p>%s</p>%s
<input name="token" type="password" autocomplete="current-password" autofocus><button>%s</button></form></body></html>`,
		i18n.Tr("choboterm 창의 설정에서 보이는 접속 토큰을 입력하세요"), msg, i18n.Tr("접속"))
}

// splitIndex splits index.html right after <head>, where the client's script goes.
func splitIndex(index []byte) (head, rest []byte) {
	i := bytes.Index(bytes.ToLower(index), []byte("<head>"))
	if i < 0 {
		return nil, index
	}
	i += len("<head>")
	return index[:i], index[i:]
}

// ---- Clients ----

// newClient registers a page that was just loaded.
func (w *webServer) newClient(local bool) (*webClient, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, errors.New("web server stopped")
	}
	// Client ids count up and wrap, skipping those in use.
	id := 0
	for range maxClientID {
		w.nextID++
		if w.nextID > maxClientID {
			w.nextID = 1
		}
		if w.clients[w.nextID] == nil {
			id = w.nextID
			break
		}
	}
	if id == 0 {
		return nil, errors.New("too many pages")
	}
	c := newWebClient(w, id, local)
	w.clients[id] = c
	if f := w.onChange; f != nil {
		go f(len(w.clients))
	}
	return c, nil
}

func (w *webServer) client(id int) *webClient {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.clients[id]
}

// removeClient forgets a closed client.
func (w *webServer) removeClient(c *webClient) {
	w.mu.Lock()
	if w.clients[c.id] == c {
		delete(w.clients, c.id)
	}
	n, f := len(w.clients), w.onChange
	if w.closed {
		f = nil
	}
	w.mu.Unlock()
	if f != nil {
		f(n)
	}
}

// event sends an event to client id's page (see App.emitFor).
func (w *webServer) event(id int, name string, data []interface{}) {
	if c := w.client(id); c != nil {
		c.event(name, data)
	}
}

// output sends terminal output of a tab to client id's page.
func (w *webServer) output(id, tabID int, out []byte) {
	if c := w.client(id); c != nil {
		c.output(tabID, out)
	}
}

// ---- Helpers ----

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand doesn't fail on supported systems
	}
	return hex.EncodeToString(b)
}

// loopbackHost reports whether host (from an address to listen on) only
// accepts connections from this PC.
func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackRemote reports whether a request's RemoteAddr is this PC.
func loopbackRemote(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// outboundIP guesses this PC's address on the network, for the URL shown
// when listening on every address.
func outboundIP() string {
	c, err := net.Dial("udp", "192.0.2.1:9") // no packet is sent for UDP
	if err != nil {
		return "localhost"
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

// webConfigDir is where the token and the certificate are kept.
func webConfigDir() (string, error) {
	p, err := config.File()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(p)
	return dir, os.MkdirAll(dir, 0o700)
}

// webToken returns the access token, creating it (or a new one) as needed.
// Changing it logs every browser out.
func webToken(renew bool) (string, error) {
	dir, err := webConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "web-token")
	if !renew {
		if b, err := os.ReadFile(p); err == nil {
			if t := strings.TrimSpace(string(b)); len(t) >= 32 {
				return t, nil
			}
		}
	}
	t := randomHex(24)
	if err := os.WriteFile(p, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}
