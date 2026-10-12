package main

// One web page (webClient) and its WebSocket.
//
// Server to page: every message gets the next sequence number (counted by
// the page, not sent). Calls' results and events are JSON text messages;
// terminal output is binary: [1][tab id, uint32 LE][bytes]. Messages stay
// queued until the page acknowledges them ({"ack": seq}), so a page that
// reconnects after a network drop (?seq= what it has) gets exactly what it
// missed. While too much output is unacknowledged, the tab's reader waits,
// which slows the remote down like a slow terminal would.
//
// Page to server: {"c": id, "m": "Method", "a": [args]} calls a method of
// App (as Wails would) and gets {"r": id, "v": result} or {"r": id, "x":
// "error"}. A call resent after reconnecting runs only once.

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"choboterm/internal/dbg"
)

// webMaxUnacked is how much output may wait for the page's
// acknowledgement before the tab's reader waits (tests lower it).
var webMaxUnacked = 8 << 20

const (
	// webSeenWindow is how many recent call ids are remembered to drop resends.
	webSeenWindow = 4096
	webPing       = 25 * time.Second
	webReadWait   = 70 * time.Second // > webPing plus a slow pong
	webWriteWait  = 30 * time.Second
	webMaxMessage = 64 << 20 // a call's arguments (a big paste, a file saved in the viewer)
)

// webOpOutput marks a binary message carrying terminal output.
const webOpOutput = 1

type webMsg struct {
	seq  uint64
	bin  bool
	data []byte
}

// webClient is one loaded page.
type webClient struct {
	id    int
	key   string // proves a WebSocket or upload belongs to this page
	local bool   // the browser is on this PC (see webServer.isLocal)
	srv   *webServer
	view  *App // calls from this page run as this App (caller = this client)
	done  chan struct{}

	mu       sync.Mutex
	cond     *sync.Cond // queue changes, acks, (de)attaching, closing
	out      []webMsg   // unacknowledged messages; their seqs are consecutive
	outBytes int
	nextSeq  uint64
	conn     *websocket.Conn // nil while disconnected
	gen      int             // counts connections; an old writer stops when it changes
	closed   bool
	timer    *time.Timer // closes the client while it stays disconnected
	seen     map[uint64]bool
	maxSeen  uint64
	picks    map[string]chan []string // stage id -> picker waiting for the upload
}

func newWebClient(w *webServer, id int, local bool) *webClient {
	c := &webClient{
		id: id, key: randomHex(16), local: local, srv: w,
		done: make(chan struct{}), nextSeq: 1,
		seen: map[uint64]bool{}, picks: map[string]chan []string{},
	}
	c.cond = sync.NewCond(&c.mu)
	c.view = &App{appState: w.app.appState, caller: c}
	// A page that never connects (closed right away) is dropped too.
	c.mu.Lock()
	c.timer = time.AfterFunc(w.opts.Grace, c.expire)
	c.mu.Unlock()
	return c
}

// expire closes the client if it is still disconnected (the timer may fire
// just as the page reconnects).
func (c *webClient) expire() {
	c.mu.Lock()
	connected := c.conn != nil
	c.mu.Unlock()
	if !connected {
		c.close()
	}
}

// push queues a message. With wait, it first waits while the page is far
// behind (terminal output only: results and events must never wait).
func (c *webClient) push(bin bool, data []byte, wait bool) {
	c.mu.Lock()
	for wait && !c.closed && c.outBytes >= webMaxUnacked {
		c.cond.Wait()
	}
	if !c.closed {
		c.out = append(c.out, webMsg{c.nextSeq, bin, data})
		c.nextSeq++
		c.outBytes += len(data)
		c.cond.Broadcast()
	}
	c.mu.Unlock()
}

// event queues an event for the page's listeners (EventsOn).
func (c *webClient) event(name string, data []interface{}) {
	if data == nil {
		data = []interface{}{}
	}
	b, err := json.Marshal(struct {
		E string        `json:"e"`
		D []interface{} `json:"d"`
	}{name, data})
	if err != nil {
		dbg.Printf("web: event %s: %v", name, err)
		return
	}
	c.push(false, b, false)
}

// output queues terminal output of a tab.
func (c *webClient) output(tabID int, out []byte) {
	b := make([]byte, 5+len(out))
	b[0] = webOpOutput
	binary.LittleEndian.PutUint32(b[1:], uint32(tabID))
	copy(b[5:], out)
	c.push(true, b, true)
}

// ackLocked forgets messages the page has received.
func (c *webClient) ackLocked(seq uint64) {
	n := 0
	for n < len(c.out) && c.out[n].seq <= seq {
		c.outBytes -= len(c.out[n].data)
		n++
	}
	if n > 0 {
		// Copy down so the backing array doesn't grow forever.
		c.out = append(c.out[:0], c.out[n:]...)
		c.cond.Broadcast()
	}
}

// close ends the client: its tabs, uploads and downloads go away.
func (c *webClient) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	if c.timer != nil {
		c.timer.Stop()
	}
	picks := c.picks
	c.picks = nil
	c.out, c.outBytes = nil, 0
	close(c.done)
	c.cond.Broadcast()
	c.mu.Unlock()
	dbg.Printf("web: client %d closed", c.id)

	if conn != nil {
		// 1001 "closed" tells the page not to reconnect (see web_shim.js).
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, "closed"), time.Now().Add(time.Second))
		_ = conn.Close()
	}
	for _, ch := range picks {
		close(ch) // the waiting picker sees a cancel
	}
	cancelPrompts(func(tab int) bool { return clientOf(tab) == c.id })
	a := c.srv.app
	a.mu.Lock()
	var ids []int
	for id := range a.tabs {
		if clientOf(id) == c.id {
			ids = append(ids, id)
		}
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.CloseTab(id)
	}
	c.srv.dropFiles(c)
	c.srv.removeClient(c)
}

// ---- WebSocket ----

var webUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 64 << 10,
	// Only this server's own page: a page on another site must not be able
	// to drive the terminal with the user's cookie.
	CheckOrigin: sameOrigin,
}

// sameOrigin reports whether the request's Origin is the host it was sent to.
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

// handleWS connects (or reconnects) a page: /web/ws?c=id&k=key&seq=n.
func (w *webServer) handleWS(rw http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, _ := strconv.Atoi(q.Get("c"))
	seq, _ := strconv.ParseUint(q.Get("seq"), 10, 64)
	c := w.client(id)
	conn, err := webUpgrader.Upgrade(rw, r, nil)
	if err != nil {
		return // Upgrade already answered
	}
	if c == nil || q.Get("k") != c.key || (c.local && !w.isLocal(r)) {
		// The page is gone (closed after a long disconnect, or the server
		// restarted): tell it to reload instead of reconnecting forever.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"gone":true}`))
		_ = conn.Close()
		return
	}
	c.serve(conn, seq)
}

func (c *webClient) serve(conn *websocket.Conn, seq uint64) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"gone":true}`))
		_ = conn.Close()
		return
	}
	if c.conn != nil {
		_ = c.conn.Close() // the same page reconnected; drop the old socket
	}
	c.gen++
	gen := c.gen
	c.conn = conn
	c.timer.Stop()
	if last := c.nextSeq - 1; seq > last {
		seq = last
	}
	c.ackLocked(seq) // the page has these
	c.cond.Broadcast()
	c.mu.Unlock()
	dbg.Printf("web: client %d connected (gen %d, seq %d)", c.id, gen, seq)
	c.srv.changed()

	stop := make(chan struct{})
	go c.writeLoop(conn, gen, seq)
	go c.pingLoop(conn, stop)
	c.readLoop(conn)
	close(stop)

	c.mu.Lock()
	if c.gen == gen && !c.closed {
		c.conn = nil
		c.timer.Reset(c.srv.opts.Grace)
		dbg.Printf("web: client %d disconnected; closing in %v", c.id, c.srv.opts.Grace)
	}
	c.cond.Broadcast()
	c.mu.Unlock()
	_ = conn.Close()
	c.srv.changed()
}

// writeLoop sends queued messages after sent until the connection changes.
func (c *webClient) writeLoop(conn *websocket.Conn, gen int, sent uint64) {
	for {
		c.mu.Lock()
		for !c.closed && c.gen == gen && c.nextSeq-1 <= sent {
			c.cond.Wait()
		}
		if c.closed || c.gen != gen {
			c.mu.Unlock()
			return
		}
		if len(c.out) == 0 || c.out[0].seq > sent+1 {
			// Acknowledged past what this writer sent (can't really happen).
			if len(c.out) == 0 {
				sent = c.nextSeq - 1
				c.mu.Unlock()
				continue
			}
			sent = c.out[0].seq - 1
		}
		batch := append([]webMsg(nil), c.out[sent+1-c.out[0].seq:]...)
		c.mu.Unlock()

		for _, m := range batch {
			typ := websocket.TextMessage
			if m.bin {
				typ = websocket.BinaryMessage
			}
			_ = conn.SetWriteDeadline(time.Now().Add(webWriteWait))
			if err := conn.WriteMessage(typ, m.data); err != nil {
				_ = conn.Close() // readLoop ends and the client waits for a reconnect
				return
			}
			sent = m.seq
		}
	}
}

func (c *webClient) pingLoop(conn *websocket.Conn, stop <-chan struct{}) {
	t := time.NewTicker(webPing)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

// webIn is a message from the page.
type webIn struct {
	Call   uint64            `json:"c"`
	Method string            `json:"m"`
	Args   []json.RawMessage `json:"a"`
	Ack    uint64            `json:"ack"`
	// Pick cancels the file picker of a stage (see web_files.go).
	Cancel string `json:"cancel"`
}

func (c *webClient) readLoop(conn *websocket.Conn) {
	conn.SetReadLimit(webMaxMessage)
	_ = conn.SetReadDeadline(time.Now().Add(webReadWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(webReadWait))
	})
	for {
		typ, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(webReadWait))
		if typ != websocket.TextMessage {
			continue
		}
		var m webIn
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Ack > 0 {
			c.mu.Lock()
			if m.Ack < c.nextSeq {
				c.ackLocked(m.Ack)
			}
			c.mu.Unlock()
		}
		if m.Cancel != "" {
			c.answerPick(m.Cancel, nil)
		}
		if m.Method != "" && c.firstTime(m.Call) {
			go c.call(m.Call, m.Method, m.Args)
		}
	}
}

// firstTime reports whether call id hasn't run yet (a page resends the
// calls still unanswered when it reconnects).
func (c *webClient) firstTime(id uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[id] || id+webSeenWindow <= c.maxSeen {
		return false
	}
	c.seen[id] = true
	if id > c.maxSeen {
		c.maxSeen = id
	}
	if len(c.seen) > 2*webSeenWindow {
		for k := range c.seen {
			if k+webSeenWindow <= c.maxSeen {
				delete(c.seen, k)
			}
		}
	}
	return true
}

// call runs one method for the page and queues its result.
func (c *webClient) call(id uint64, method string, args []json.RawMessage) {
	v, err := webInvoke(c.view, method, args)
	var b []byte
	if err != nil {
		b, _ = json.Marshal(struct {
			R uint64 `json:"r"`
			X string `json:"x"`
		}{id, err.Error()})
	} else {
		b, err = json.Marshal(struct {
			R uint64      `json:"r"`
			V interface{} `json:"v"`
		}{id, v})
		if err != nil {
			b, _ = json.Marshal(struct {
				R uint64 `json:"r"`
				X string `json:"x"`
			}{id, err.Error()})
		}
	}
	c.push(false, b, false)
}

// ---- Calling App methods ----

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

// ---- Files of a client ----

// tempDir makes a folder for one upload or download of the client.
func (c *webClient) tempDir() (string, error) {
	dir := filepath.Join(c.srv.stageRoot, strconv.Itoa(c.id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, "x")
}
