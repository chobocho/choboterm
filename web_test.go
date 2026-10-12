package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gorilla/websocket"
)

// isClosed and input read a pipeSession under its lock (the pump and the
// web calls use it from other goroutines).
func (p *pipeSession) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *pipeSession) input() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.in.String()
}

var testPage = fstest.MapFS{
	"index.html":    {Data: []byte("<!doctype html><html><head><title>t</title></head><body>page</body></html>")},
	"assets/app.js": {Data: []byte("console.log(1)")},
}

// webFixture is a web server on a random port with a logged-in browser.
type webFixture struct {
	t    *testing.T
	a    *App
	w    *webServer
	base string // http://127.0.0.1:port
	jar  http.CookieJar
	hc   *http.Client
}

func startTestWeb(t *testing.T, opts webOptions) *webFixture {
	t.Helper()
	isolateSettings(t)
	old := webWrongTokenDelay
	webWrongTokenDelay = 0
	t.Cleanup(func() { webWrongTokenDelay = old })
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	if opts.Grace == 0 {
		opts.Grace = 5 * time.Second
	}
	a := NewApp()
	w, err := startWeb(a, testPage, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.stop)
	return fixtureFor(t, a, w)
}

// fixtureFor is a logged-out browser for the running server w of a.
func fixtureFor(t *testing.T, a *App, w *webServer) *webFixture {
	jar, _ := cookiejar.New(nil)
	f := &webFixture{t: t, a: a, w: w, base: strings.TrimSuffix(w.URL(true), "/"), jar: jar}
	f.hc = &http.Client{Jar: jar, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	return f
}

func (f *webFixture) get(path string) (int, string) {
	f.t.Helper()
	r, err := f.hc.Get(f.base + path)
	if err != nil {
		f.t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b)
}

func (f *webFixture) login() {
	f.t.Helper()
	if code, _ := f.get("/?token=" + url.QueryEscape(f.w.token)); code != 200 {
		f.t.Fatalf("login: %d", code)
	}
}

type pageInfo struct {
	ID    int    `json:"id"`
	Key   string `json:"key"`
	Base  int    `json:"base"`
	Local bool   `json:"local"`
}

var cbtRe = regexp.MustCompile(`window\.__cbt=(\{.*?\})</script>`)

// page loads the page and returns its client info.
func (f *webFixture) page() pageInfo {
	f.t.Helper()
	code, body := f.get("/")
	m := cbtRe.FindStringSubmatch(body)
	if code != 200 || m == nil {
		f.t.Fatalf("page %d: %s", code, body)
	}
	var p pageInfo
	if err := json.Unmarshal([]byte(m[1]), &p); err != nil {
		f.t.Fatal(err)
	}
	return p
}

// wsConn is the test side of a page's WebSocket.
type wsConn struct {
	t    *testing.T
	c    *websocket.Conn
	seq  uint64
	call uint64
}

func (f *webFixture) dial(p pageInfo, seq uint64, origin string) (*wsConn, *http.Response, error) {
	u := strings.Replace(f.base, "http", "ws", 1) + fmt.Sprintf("/web/ws?c=%d&k=%s&seq=%d", p.ID, p.Key, seq)
	if origin == "" {
		origin = f.base
	}
	d := websocket.Dialer{Jar: f.jar, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	c, resp, err := d.Dial(u, http.Header{"Origin": {origin}})
	if err != nil {
		return nil, resp, err
	}
	return &wsConn{t: f.t, c: c, seq: seq}, resp, nil
}

func (f *webFixture) connect(p pageInfo) *wsConn {
	f.t.Helper()
	c, _, err := f.dial(p, 0, "")
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { c.c.Close() })
	return c
}

// msg is one message from the server.
type msg struct {
	bin  []byte
	R    *uint64           `json:"r"`
	V    json.RawMessage   `json:"v"`
	X    *string           `json:"x"`
	E    string            `json:"e"`
	D    []json.RawMessage `json:"d"`
	Gone bool              `json:"gone"`
}

func (c *wsConn) read() msg {
	c.t.Helper()
	_ = c.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, data, err := c.c.ReadMessage()
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	c.seq++
	if typ == websocket.BinaryMessage {
		return msg{bin: data}
	}
	var m msg
	if err := json.Unmarshal(data, &m); err != nil {
		c.t.Fatalf("bad json %s", data)
	}
	return m
}

// readUntil skips messages until one matches.
func (c *wsConn) readUntil(match func(msg) bool) msg {
	c.t.Helper()
	for i := 0; i < 1000; i++ {
		if m := c.read(); match(m) {
			return m
		}
	}
	c.t.Fatal("no matching message")
	return msg{}
}

func (c *wsConn) send(v interface{}) {
	c.t.Helper()
	b, _ := json.Marshal(v)
	if err := c.c.WriteMessage(websocket.TextMessage, b); err != nil {
		c.t.Fatal(err)
	}
}

// callAsync sends a call and returns its id.
func (c *wsConn) callAsync(method string, args ...interface{}) uint64 {
	c.call++
	if args == nil {
		args = []interface{}{}
	}
	c.send(map[string]interface{}{"c": c.call, "m": method, "a": args})
	return c.call
}

// call runs a method and returns its result or error.
func (c *wsConn) callID(id uint64) (json.RawMessage, *string) {
	c.t.Helper()
	m := c.readUntil(func(m msg) bool { return m.R != nil && *m.R == id })
	return m.V, m.X
}

func (c *wsConn) callOK(method string, args ...interface{}) json.RawMessage {
	c.t.Helper()
	v, x := c.callID(c.callAsync(method, args...))
	if x != nil {
		c.t.Fatalf("%s: %s", method, *x)
	}
	return v
}

func TestWebNeedsToken(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	for _, p := range []string{"/", "/assets/app.js", "/web/shim.js", "/web/ws", "/web/dl/x"} {
		if code, _ := f.get(p); code != http.StatusUnauthorized {
			t.Errorf("%s without token: %d", p, code)
		}
	}
	if code, body := f.get("/?token=wrong"); code != http.StatusUnauthorized || !strings.Contains(body, "<form") {
		t.Fatalf("wrong token: %d", code)
	}
	// The login form works too.
	r, err := f.hc.PostForm(f.base+"/login", url.Values{"token": {f.w.token}})
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("login form: %v %v", r, err)
	}
	r.Body.Close()
	if code, body := f.get("/assets/app.js"); code != 200 || body != "console.log(1)" {
		t.Fatalf("asset: %d %q", code, body)
	}
	if code, body := f.get("/web/shim.js"); code != 200 || !strings.Contains(body, "window.go") {
		t.Fatalf("shim: %d", code)
	}
}

func TestWebTokenIsKept(t *testing.T) {
	isolateSettings(t)
	a, err := webToken(false)
	if err != nil || len(a) < 32 {
		t.Fatalf("%q %v", a, err)
	}
	if b, _ := webToken(false); b != a {
		t.Fatal("token changed")
	}
	if c, _ := webToken(true); c == a {
		t.Fatal("renew kept the token")
	}
}

func TestWebPageGetsItsOwnClient(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p1, p2 := f.page(), f.page()
	if p1.ID == p2.ID || p1.Base != p1.ID<<tabBits || p1.ID < 1 || p1.Local {
		t.Fatalf("pages %+v %+v", p1, p2)
	}
	if clientOf(p1.Base+1) != p1.ID || clientOf(p1.Base+(1<<tabBits)-1) != p1.ID || clientOf(5) != windowClient {
		t.Fatal("clientOf")
	}
}

func TestWebCalls(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	c := f.connect(p)

	var v string
	json.Unmarshal(c.callOK("GetVersion"), &v)
	if v != AppVersion {
		t.Fatalf("version %q", v)
	}
	// An error result rejects the call, like Wails.
	if _, x := c.callID(c.callAsync("Connect", p.Base+1, map[string]interface{}{"host": " "})); x == nil || *x != "Host를 입력하세요" {
		t.Fatalf("error: %v", x)
	}
	if _, x := c.callID(c.callAsync("NoSuchMethod")); x == nil {
		t.Fatal("unknown method accepted")
	}
	// Missing arguments are zero values; a struct comes back as an object.
	json.Unmarshal(c.callOK("SetEncoding", p.Base+1), &v)
	if v != EncodingUTF8 {
		t.Fatalf("SetEncoding: %q", v)
	}
	var s Settings
	if err := json.Unmarshal(c.callOK("GetSettings"), &s); err != nil || s.FontSize == 0 {
		t.Fatalf("GetSettings: %+v %v", s, err)
	}
	// What only the window may do.
	if _, x := c.callID(c.callAsync("WebStop")); x == nil {
		t.Fatal("a page stopped the server")
	}
	if f.a.web.Load() == nil {
		t.Fatal("server gone")
	}
}

func TestWebOutputAndEventsGoToTheTabsPage(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	c := f.connect(p)
	other := f.connect(f.page())

	id := p.Base + 1
	sess := newPipeSession()
	tb := f.a.getTab(id)
	tb.sess = sess
	go tb.pump(sess)
	sess.out.Write([]byte("hello\x1b[0m"))
	m := c.readUntil(func(m msg) bool { return m.bin != nil })
	if m.bin[0] != webOpOutput || int(binary.LittleEndian.Uint32(m.bin[1:5])) != id || string(m.bin[5:]) != "hello\x1b[0m" {
		t.Fatalf("frame % X", m.bin)
	}
	// Events of the tab: JSON with the tab id first.
	tb.emit("log:changed", "x.log")
	m = c.readUntil(func(m msg) bool { return m.E != "" })
	if m.E != "log:changed" || string(m.D[0]) != fmt.Sprint(id) || string(m.D[1]) != `"x.log"` {
		t.Fatalf("event %+v", m)
	}
	// Keyboard input from the page.
	c.callOK("Send", id, "ls\r")
	if sess.input() != "ls\r" {
		t.Fatalf("input %q", sess.input())
	}
	// The window's tabs (and other pages) don't get them; without a window
	// (no Wails context) window events are just dropped.
	f.a.getTab(1).emit("log:changed", "w.log")
	f.a.emitFor(1, "auth:prompt", Prompt{Tab: 1})
	other.callOK("GetVersion") // anything for 'other' arrives before this result
	sess.out.Close()
	c.readUntil(func(m msg) bool { return m.E == "term:closed" })
}

func TestWebRejectsOtherOrigins(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	if _, resp, err := f.dial(p, 0, "http://evil.example"); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("evil origin: %v %v", resp, err)
	}
	// Without the cookie: unauthorized even with the page's key.
	d := websocket.Dialer{}
	u := strings.Replace(f.base, "http", "ws", 1) + fmt.Sprintf("/web/ws?c=%d&k=%s", p.ID, p.Key)
	if _, resp, err := d.Dial(u, http.Header{"Origin": {f.base}}); err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: %v", err)
	}
}

func TestWebUnknownPageIsGone(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	p.Key = "bad"
	c, _, err := f.dial(p, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if m := c.read(); !m.Gone {
		t.Fatalf("%+v", m)
	}
}

func TestWebReconnectResends(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	c := f.connect(p)
	id := p.Base + 1
	sess := newPipeSession()
	f.a.getTab(id).sess = sess

	// Three events; the page reads (and implicitly has) only the first.
	for i := 0; i < 3; i++ {
		f.a.getTab(id).emit("log:changed", fmt.Sprint(i))
	}
	if m := c.read(); string(m.D[1]) != `"0"` {
		t.Fatalf("first %+v", m)
	}
	c.c.Close()

	// Reconnect saying we have 1 message: 2 and 3 come again.
	c2, _, err := f.dial(p, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.c.Close()
	for _, want := range []string{`"1"`, `"2"`} {
		if m := c2.read(); string(m.D[1]) != want {
			t.Fatalf("replay got %s want %s", m.D[1], want)
		}
	}
	// A call resent after reconnecting (same id) runs once.
	c2.send(map[string]interface{}{"c": 7, "m": "Send", "a": []interface{}{id, "x"}})
	c2.send(map[string]interface{}{"c": 7, "m": "Send", "a": []interface{}{id, "x"}})
	c2.callID(7)
	c2.call = 7
	c2.callOK("GetVersion")
	if sess.input() != "x" {
		t.Fatalf("ran %q", sess.input())
	}
}

func TestWebDisconnectedPageClosesItsTabs(t *testing.T) {
	f := startTestWeb(t, webOptions{Grace: 200 * time.Millisecond})
	var idle atomic.Int32 // onChange calls saying no page is left
	f.w.onChange = func(n int) {
		if n == 0 {
			idle.Add(1)
		}
	}
	f.login()
	p := f.page()
	c := f.connect(p)
	id := p.Base + 1
	sess := newPipeSession()
	f.a.getTab(id).sess = sess
	win := newPipeSession()
	f.a.getTab(1).sess = win // a window tab stays

	c.callOK("GetVersion")
	c.c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for f.a.findTab(id) != nil {
		if time.Now().After(deadline) {
			t.Fatal("tab not closed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sess.isClosed() || win.isClosed() || f.a.findTab(1) == nil {
		t.Fatalf("closed: page %v window %v", sess.isClosed(), win.isClosed())
	}
	for f.w.clientCount() != 0 || idle.Load() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("clients %d idle %d", f.w.clientCount(), idle.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A page that reconnects in time keeps its tabs.
	p2 := f.page()
	c2 := f.connect(p2)
	f.a.getTab(p2.Base + 1).sess = newPipeSession()
	c2.callOK("GetVersion")
	c2.c.Close()
	time.Sleep(50 * time.Millisecond)
	c3, _, err := f.dial(p2, c2.seq, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c3.c.Close()
	time.Sleep(400 * time.Millisecond)
	if f.a.findTab(p2.Base+1) == nil {
		t.Fatal("reconnected page lost its tab")
	}
}

func TestWebLocalBrowser(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	u, _ := url.Parse(f.w.localURL())
	if code, _ := f.get("/?" + u.RawQuery); code != 200 {
		t.Fatalf("local login %d", code)
	}
	if p := f.page(); !p.Local {
		t.Fatal("not local")
	}
	// The code works once; and a proxy header means the browser is elsewhere.
	jar, _ := cookiejar.New(nil)
	other := &http.Client{Jar: jar}
	r, _ := other.Get(f.base + "/?" + u.RawQuery)
	r.Body.Close()
	req, _ := http.NewRequest("GET", f.base+"/", nil)
	r, _ = other.Do(req)
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), `"local":false`) {
		t.Fatal("code reused")
	}
	req, _ = http.NewRequest("GET", f.base+"/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	r, _ = f.hc.Do(req)
	b, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(b), `"local":false`) {
		t.Fatal("proxied request counted as local")
	}
}

// A remote browser picks files by uploading them and gets saved files as downloads.
func TestWebBrowserPickerUploadAndDownload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	c := f.connect(p)

	call := c.callAsync("ConsoleOpen")
	m := c.readUntil(func(m msg) bool { return m.E == "web:pick" })
	var pick webPick
	json.Unmarshal(m.D[0], &pick)
	if pick.Multiple || pick.Accept != ".lua" || pick.ID == "" {
		t.Fatalf("pick %+v", pick)
	}
	put := func(path, body string) int {
		req, _ := http.NewRequest("PUT", f.base+"/web/stage/"+pick.ID+path, strings.NewReader(body))
		req.Header.Set("X-Cbt-Key", p.Key)
		r, err := f.hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	if code := put("/file?p=../evil.lua", "x"); code != http.StatusBadRequest {
		t.Fatalf("path escape: %d", code)
	}
	if code := put("/file?p=s.lua", "print('hi')"); code != http.StatusNoContent {
		t.Fatalf("put %d", code)
	}
	req, _ := http.NewRequest("POST", f.base+"/web/stage/"+pick.ID+"/done", strings.NewReader(`{"tops":["s.lua"]}`))
	req.Header.Set("X-Cbt-Key", p.Key)
	r, _ := f.hc.Do(req)
	r.Body.Close()
	v, x := c.callID(call)
	var cf ConsoleFile
	json.Unmarshal(v, &cf)
	if x != nil || cf.Name != "s.lua" || cf.Text != "print('hi')" {
		t.Fatalf("ConsoleOpen: %+v %v", cf, x)
	}

	// Cancelling the chooser.
	call = c.callAsync("ConsoleOpen")
	m = c.readUntil(func(m msg) bool { return m.E == "web:pick" })
	json.Unmarshal(m.D[0], &pick)
	c.send(map[string]string{"cancel": pick.ID})
	v, x = c.callID(call)
	json.Unmarshal(v, &cf)
	if x != nil || cf.Name != "" {
		t.Fatalf("cancelled: %+v %v", cf, x)
	}

	// Saving: the file comes as a download, once.
	call = c.callAsync("ConsoleSave", "console text")
	m = c.readUntil(func(m msg) bool { return m.E == "web:download" })
	var dl struct{ URL, Name string }
	json.Unmarshal(m.D[0], &dl)
	v, _ = c.callID(call)
	var name string
	json.Unmarshal(v, &name)
	if name != dl.Name || !strings.HasPrefix(name, "lua_console_") {
		t.Fatalf("names %q %q", name, dl.Name)
	}
	if code, body := f.get(dl.URL); code != 200 || body != "console text" {
		t.Fatalf("download %d %q", code, body)
	}
	if code, _ := f.get(dl.URL); code != http.StatusNotFound {
		t.Fatalf("second download %d", code)
	}
	// Folder choice isn't possible from another PC.
	if _, x := c.callID(c.callAsync("ChooseLogDir", "")); x == nil {
		t.Fatal("ChooseLogDir from a remote browser")
	}
}

// Files dropped on the page are uploaded to a stage, then FileUploadPaths
// sends them on and the stage is deleted.
func TestWebDropUpload(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	c := f.connect(p)
	id := p.Base + 1
	fs := newMemFS()
	tb := f.a.getTab(id)
	tb.files.fs, tb.files.proto = fs, "TEST"

	do := func(method, path, body string) *http.Response {
		req, _ := http.NewRequest(method, f.base+path, strings.NewReader(body))
		req.Header.Set("X-Cbt-Key", p.Key)
		r, err := f.hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := do("POST", "/web/stage", "")
	var st struct{ ID string }
	json.NewDecoder(r.Body).Decode(&st)
	r.Body.Close()
	do("PUT", "/web/stage/"+st.ID+"/file?p=d/a.txt", "A").Body.Close()
	do("PUT", "/web/stage/"+st.ID+"/file?p=d/sub/b.txt", "BB").Body.Close()
	do("PUT", "/web/stage/"+st.ID+"/dir?p=d/empty", "").Body.Close()
	r = do("POST", "/web/stage/"+st.ID+"/done", `{"tops":["d"]}`)
	var res struct{ Paths []string }
	json.NewDecoder(r.Body).Decode(&res)
	r.Body.Close()
	if len(res.Paths) != 1 {
		t.Fatalf("paths %v", res.Paths)
	}
	// Another page's key can't touch the stage.
	req, _ := http.NewRequest("PUT", f.base+"/web/stage/"+st.ID+"/file?p=x", strings.NewReader("x"))
	req.Header.Set("X-Cbt-Key", "other")
	if r, _ := f.hc.Do(req); r.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign key: %d", r.StatusCode)
	}

	var n int
	json.Unmarshal(c.callOK("FileUploadPaths", id, "/up", res.Paths), &n)
	if n != 2 || string(fs.files["/up/d/a.txt"]) != "A" || string(fs.files["/up/d/sub/b.txt"]) != "BB" {
		t.Fatalf("uploaded %d %v", n, fs.files)
	}
	if _, err := os.Stat(res.Paths[0]); !os.IsNotExist(err) {
		t.Fatal("stage not deleted")
	}
}

func TestStagePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "st")
	for rel, ok := range map[string]bool{
		"a.txt": true, "d/b.txt": true, `d\c.txt`: true, "/lead.txt": true,
		"": false, "..": false, "../x": false, "d/../../x": false, "a\x00b": false,
	} {
		p, got := stagePath(dir, rel)
		if got != ok {
			t.Errorf("%q: %v", rel, got)
		}
		if got && !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			t.Errorf("%q -> %q", rel, p)
		}
	}
}

func TestSafeFileNameAndZip(t *testing.T) {
	for in, want := range map[string]string{"a.txt": "a.txt", "../x": "x", `C:\a\b.txt`: "b.txt", "a:b?.txt": "a_b_.txt", "": "f", "..": "f"} {
		if got := safeFileName(in, "f"); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	src := t.TempDir()
	writeFiles(t, src, map[string]string{"한글.txt": "1", "d/x.txt": "22"})
	zp := filepath.Join(t.TempDir(), "o.zip")
	if err := zipFolder(src, zp); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(zp)
	if !bytes.Contains(b, []byte("한글.txt")) || !bytes.Contains(b, []byte("d/x.txt")) {
		t.Fatal("zip entries")
	}
}

// Output waits while the page is too far behind, and goes on once it acknowledges.
func TestWebOutputBackpressure(t *testing.T) {
	old := webMaxUnacked
	webMaxUnacked = 1000
	defer func() { webMaxUnacked = old }()
	f := startTestWeb(t, webOptions{})
	c, _ := f.w.newClient(false)
	chunk := make([]byte, 600)
	c.output(c.id<<tabBits+1, chunk) // 605 bytes queued
	c.output(c.id<<tabBits+1, chunk) // over the limit after this one
	done := make(chan struct{})
	go func() { c.output(c.id<<tabBits+1, chunk); close(done) }()
	select {
	case <-done:
		t.Fatal("output didn't wait")
	case <-time.After(100 * time.Millisecond):
	}
	c.mu.Lock()
	c.ackLocked(2)
	c.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("output still waiting after the ack")
	}
	// Events never wait.
	c.event("x", nil)
	c.close() // and closing releases any waiting writer
}

func TestWebStopClosesEverything(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	f.login()
	p := f.page()
	f.connect(p)
	sess := newPipeSession()
	f.a.getTab(p.Base + 1).sess = sess
	root := f.w.stageRoot
	f.w.stop()
	if f.a.web.Load() != nil || f.a.findTab(p.Base+1) != nil || !sess.isClosed() {
		t.Fatal("not stopped")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("temporary files left")
	}
	f.w.stop() // twice is fine
}

func TestWebTLSWhenOthersCanConnect(t *testing.T) {
	f := startTestWeb(t, webOptions{Addr: "0.0.0.0:0"})
	if !f.w.tls || !strings.HasPrefix(f.w.URL(true), "https://") {
		t.Fatal("no TLS on 0.0.0.0")
	}
	f.base = strings.Replace(f.base, f.base[strings.Index(f.base, "//")+2:strings.LastIndex(f.base, ":")], "127.0.0.1", 1)
	r, err := f.hc.Get(f.base + "/")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	sum := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
	if fp := fmt.Sprintf("%X", sum[:]); strings.ReplaceAll(f.w.fp, ":", "") != fp {
		t.Fatalf("fingerprint %s vs %s", f.w.fp, fp)
	}
	// The local URL points at the loopback address.
	if !strings.HasPrefix(f.w.localURL(), "https://127.0.0.1:") {
		t.Fatalf("local URL %s", f.w.localURL())
	}
	// --insecure: plain HTTP.
	g := startTestWeb(t, webOptions{Addr: "0.0.0.0:0", Insecure: true})
	if g.w.tls {
		t.Fatal("TLS despite Insecure")
	}
}

func TestWebStartErrors(t *testing.T) {
	isolateSettings(t)
	a := NewApp()
	if _, err := startWeb(a, testPage, webOptions{Addr: "nonsense"}); err == nil {
		t.Fatal("bad address accepted")
	}
	if _, err := startWeb(a, fstest.MapFS{}, webOptions{Addr: "127.0.0.1:0"}); err == nil {
		t.Fatal("no index.html accepted")
	}
	w, err := startWeb(a, testPage, webOptions{Addr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.stop()
	if _, err := startWeb(NewApp(), testPage, webOptions{Addr: w.ln.Addr().String()}); err == nil {
		t.Fatal("port in use accepted")
	}
}

// The window asks before closing only for its own connected tabs; without
// a Wails window there is nothing to hide.
func TestBeforeCloseIgnoresWebTabs(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	asked := 0
	f.a.hooks.emit = func(name string, _ ...interface{}) {
		if name == "app:close" {
			asked++
		}
	}
	f.a.getTab(3<<tabBits + 1).sess = newPipeSession()
	if f.a.beforeClose(nil) || asked != 0 {
		t.Fatalf("asked %d for a web tab", asked)
	}
	f.a.getTab(2).sess = newPipeSession()
	if !f.a.beforeClose(nil) || asked != 1 {
		t.Fatal("didn't ask for a window tab")
	}
}

// Closing the window while browsers use the web server asks (app:closeWeb,
// with how many pages are open) instead of quitting or hiding by itself;
// after Quit (the user chose "quit all") it closes.
func TestBeforeCloseAsksWhenBrowsersAreConnected(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	var got []interface{}
	f.a.hooks.emit = func(name string, data ...interface{}) {
		if name == "app:closeWeb" {
			got = data
		}
	}
	if f.a.beforeClose(nil) || got != nil {
		t.Fatal("asked with no browser")
	}
	f.login()
	f.connect(f.page())
	if !f.a.beforeClose(nil) || len(got) != 1 || got[0] != 1 {
		t.Fatalf("app:closeWeb %v", got)
	}
	// Even with a connected window tab, the browser question comes first
	// (it covers the window's tabs too).
	got = nil
	f.a.getTab(2).sess = newPipeSession()
	if !f.a.beforeClose(nil) || got == nil {
		t.Fatal("window tab hid the browser question")
	}
	f.a.quit.Store(true)
	if f.a.beforeClose(nil) {
		t.Fatal("didn't close after Quit")
	}
}

// A page coming and going is reported to the window (title), and a server
// that can't start says why in Settings.
func TestWebStatusEventsAndStartError(t *testing.T) {
	isolateSettings(t)
	old := webPage
	webPage = testPage
	defer func() { webPage = old }()
	a := NewApp()
	var mu sync.Mutex
	var status [][]interface{}
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "web:status" {
			mu.Lock()
			status = append(status, data)
			mu.Unlock()
		}
	}
	if _, err := a.WebStart("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(status) == 0 || status[len(status)-1][0] != true {
		t.Fatalf("status after start %v", status)
	}
	mu.Unlock()
	w := a.web.Load()
	f := fixtureFor(t, a, w)
	f.login()
	c := f.connect(f.page())
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		last := status[len(status)-1]
		mu.Unlock()
		if last[1] == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no status with 1 page: %v", last)
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.c.Close()

	// Another server on the same port: Settings shows why it's off.
	b := NewApp()
	b.hooks.emit = func(string, ...interface{}) {}
	if _, err := b.WebStart(w.ln.Addr().String()); err == nil {
		t.Fatal("started on a port in use")
	}
	if s := b.WebStatus(); s.Running || !strings.Contains(s.Error, "address already in use") && !strings.Contains(s.Error, "Only one usage") {
		t.Fatalf("status %+v", s)
	}
	if err := a.WebStop(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if last := status[len(status)-1]; last[0] != false {
		t.Fatalf("status after stop %v", last)
	}
}

// Starting choboterm again finds the running one (same token) and makes it
// show its window; a server of another token, or none, isn't taken for it.
func TestHandoffToRunning(t *testing.T) {
	f := startTestWeb(t, webOptions{})
	addr := f.w.ln.Addr().String()
	// No window in tests: the running one refuses (nothing to show).
	if handoffToRunning(webOptions{Addr: addr}) {
		t.Fatal("handoff without a window")
	}
	f.a.ctx = context.Background() // stands in for the window; showWindow is stubbed below
	defer func() { f.a.ctx = nil }()
	shown := make(chan struct{}, 1)
	oldShow := showWindowHook
	showWindowHook = func(*App) { shown <- struct{}{} }
	defer func() { showWindowHook = oldShow }()
	if !handoffToRunning(webOptions{Addr: addr}) {
		t.Fatal("no handoff to the running one")
	}
	select {
	case <-shown:
	case <-time.After(2 * time.Second):
		t.Fatal("window not shown")
	}
	// A server with another token (another user's) doesn't count.
	f.w.token = "someone-else-0123456789abcdef0123456789"
	if handoffToRunning(webOptions{Addr: addr}) {
		t.Fatal("handoff to a server of another token")
	}
	// Nothing listening.
	if handoffToRunning(webOptions{Addr: "127.0.0.1:1"}) {
		t.Fatal("handoff with nothing listening")
	}
	// Only this PC may ask, and only with POST and a nonce.
	if code, _ := f.get("/web/show?nonce=0123456789abcdef"); code != http.StatusNotFound {
		t.Fatalf("GET /web/show: %d", code)
	}
}

func TestWebStatusAndSettings(t *testing.T) {
	isolateSettings(t)
	old := webPage
	webPage = testPage
	defer func() { webPage = old }()
	a := NewApp()
	if s := a.WebStatus(); s.Running || s.Addr != webDefaultAddr || s.Remote {
		t.Fatalf("idle status %+v", s)
	}
	info, err := a.WebStart("127.0.0.1:0")
	if err != nil || !info.Running || !strings.Contains(info.URL, "token=") {
		t.Fatalf("start %+v %v", info, err)
	}
	if s := loadSettings().Web; s == nil || !s.Enabled || s.Addr != "127.0.0.1:0" {
		t.Fatalf("settings %+v", s)
	}
	// The page's own settings save keeps the web settings.
	if err := a.SaveSettings(defaultSettings()); err != nil {
		t.Fatal(err)
	}
	if s := loadSettings().Web; s == nil || !s.Enabled {
		t.Fatal("SaveSettings dropped the web settings")
	}
	oldTok := a.web.Load().token
	if info, err := a.WebNewToken(); err != nil || !info.Running || a.web.Load().token == oldTok {
		t.Fatalf("new token %+v %v", info, err)
	}
	if err := a.WebStop(); err != nil || a.web.Load() != nil || loadSettings().Web.Enabled {
		t.Fatal("stop")
	}
	if err := a.WebOpen(); err == nil {
		t.Fatal("open while stopped")
	}
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		args []string
		want cliOptions
		err  bool
	}{
		{nil, cliOptions{}, false},
		{[]string{"--web"}, cliOptions{web: true}, false},
		{[]string{"--web=0.0.0.0:8000", "--open"}, cliOptions{web: true, addr: "0.0.0.0:8000", open: true}, false},
		{[]string{"--headless", "--cert", "c.pem", "--key=k.pem"}, cliOptions{web: true, headless: true, cert: "c.pem", key: "k.pem"}, false},
		{[]string{"--web", "--grace", "2m", "--insecure"}, cliOptions{web: true, grace: 2 * time.Minute, insecure: true}, false},
		{[]string{"-h"}, cliOptions{help: true}, false},
		{[]string{"--cert", "c.pem"}, cliOptions{}, true},
		{[]string{"--grace"}, cliOptions{}, true},
		{[]string{"--grace", "-1s"}, cliOptions{}, true},
	}
	for _, c := range cases {
		got, err := parseArgs(c.args)
		if (err != nil) != c.err || (!c.err && fmt.Sprint(got) != fmt.Sprint(c.want)) {
			t.Errorf("%v: %+v %v", c.args, got, err)
		}
	}
	// Arguments that aren't ours are kept aside, not errors: the window
	// ignores them as before (wails dev passes -assetdir etc.).
	got, err := parseArgs([]string{"-assetdir", "x", "--web"})
	if err != nil || !got.web || fmt.Sprint(got.unknown) != "[-assetdir x]" {
		t.Fatalf("unknown args: %+v %v", got, err)
	}
	if o := (cliOptions{}).webOptions(); o != nil {
		t.Fatal("webOptions without --web")
	}
}

func TestWebInvokeRecoversPanics(t *testing.T) {
	a := NewApp()
	// FileList on a tab whose fs is nil returns an error, not a panic; a
	// nil *App would panic: the call fails instead of killing the server.
	if _, err := webInvoke(&App{}, "GetHistory", nil); err == nil {
		// GetHistory doesn't touch state; use a method that does.
		if _, err := webInvoke(&App{}, "CloseTab", []json.RawMessage{json.RawMessage("1")}); err == nil {
			t.Fatal("panic not turned into an error")
		}
	}
	if _, err := webInvoke(a, "Resize", []json.RawMessage{json.RawMessage(`"x"`)}); err == nil {
		t.Fatal("bad argument accepted")
	}
}
