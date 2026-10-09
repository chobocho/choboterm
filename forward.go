package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Forward is one SSH port forwarding rule.
//
//	L: this PC listens on Bind, and connections go from the server to Host:Port (ssh -L).
//	R: the server listens on Bind, and connections go from this PC to Host:Port (ssh -R).
//	D: this PC listens on Bind as a SOCKS5 proxy that connects from the server (ssh -D).
type Forward struct {
	Type     string `json:"type"`
	BindAddr string `json:"bindAddr"`
	BindPort int    `json:"bindPort"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

// ForwardStatus is a rule of a connected tab and how it is doing.
type ForwardStatus struct {
	Forward
	ID      int    `json:"id"`
	Error   string `json:"error"`   // why it isn't listening, or ""
	Conns   int    `json:"conns"`   // open connections through it
	Standby bool   `json:"standby"` // another tab to the same server listens for it
}

// normalize checks f and fills in default bind addresses.
func (f Forward) normalize() (Forward, error) {
	switch f.Type {
	case "L", "D":
		if f.BindAddr == "" {
			f.BindAddr = "127.0.0.1"
		}
	case "R":
		if f.BindAddr == "" {
			f.BindAddr = "localhost"
		}
	default:
		return f, fmt.Errorf("알 수 없는 포워딩 종류: %q", f.Type)
	}
	if f.BindPort <= 0 || f.BindPort > 65535 {
		return f, errors.New("수신 포트가 올바르지 않습니다")
	}
	if f.Type == "D" {
		f.Host, f.Port = "", 0
		return f, nil
	}
	if f.Host == "" {
		return f, errors.New("대상 호스트를 입력하세요")
	}
	if f.Port <= 0 || f.Port > 65535 {
		return f, errors.New("대상 포트가 올바르지 않습니다")
	}
	return f, nil
}

func (f Forward) bind() string   { return net.JoinHostPort(f.BindAddr, strconv.Itoa(f.BindPort)) }
func (f Forward) target() string { return net.JoinHostPort(f.Host, strconv.Itoa(f.Port)) }

func (f Forward) same(o Forward) bool {
	return f.Type == o.Type && f.BindAddr == o.BindAddr && f.BindPort == o.BindPort
}

// forwardRule is a running (or failed) rule. Of the tabs connected to the
// same server only one runs a rule; the others keep it on standby and one of
// them takes it over when that tab's connection ends.
type forwardRule struct {
	id  int
	f   Forward
	ln  net.Listener // nil if it failed to start or is on standby
	err string

	standby bool

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (r *forwardRule) track(c net.Conn, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if on {
		r.conns[c] = struct{}{}
	} else {
		delete(r.conns, c)
	}
}

func (r *forwardRule) status() ForwardStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return ForwardStatus{Forward: r.f, ID: r.id, Error: r.err, Conns: len(r.conns), Standby: r.standby}
}

func (r *forwardRule) running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ln != nil
}

func (r *forwardRule) stop() {
	r.mu.Lock()
	ln := r.ln
	r.ln = nil
	r.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	r.mu.Lock()
	conns := r.conns
	r.conns = map[net.Conn]struct{}{}
	r.mu.Unlock()
	for c := range conns {
		_ = c.Close()
	}
}

// forwards holds a tab's rules for its current SSH connection.
type forwards struct {
	mu     sync.Mutex
	nextID int
	rules  []*forwardRule
}

// startForward adds f to the tab's rules and runs it over client, or keeps
// it on standby. A rule that can't listen (e.g. the port is in use) is still
// kept, with its error, so the user sees why.
func (t *tab) startForward(client *ssh.Client, f Forward, standby bool) *forwardRule {
	t.fwd.mu.Lock()
	t.fwd.nextID++
	r := &forwardRule{id: t.fwd.nextID, f: f, conns: map[net.Conn]struct{}{}, standby: standby}
	t.fwd.rules = append(t.fwd.rules, r)
	t.fwd.mu.Unlock()
	if !standby {
		t.listenForward(client, r)
	}
	return r
}

// listenForward starts listening for r.
func (t *tab) listenForward(client *ssh.Client, r *forwardRule) {
	var (
		ln  net.Listener
		err error
	)
	if r.f.Type == "R" {
		ln, err = client.Listen("tcp", r.f.bind())
	} else {
		ln, err = net.Listen("tcp", r.f.bind())
	}
	r.mu.Lock()
	r.standby = false
	if err != nil {
		r.err = err.Error()
		r.mu.Unlock()
		return
	}
	r.err = ""
	r.ln = ln
	r.mu.Unlock()
	go t.acceptForward(client, r, ln)
}

func (t *tab) acceptForward(client *ssh.Client, r *forwardRule, ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			r.track(c, true)
			t.emit("fwd:changed")
			defer func() {
				c.Close()
				r.track(c, false)
				t.emit("fwd:changed")
			}()
			var (
				peer net.Conn
				err  error
			)
			switch r.f.Type {
			case "L":
				peer, err = client.Dial("tcp", r.f.target())
			case "R":
				peer, err = net.Dial("tcp", r.f.target())
			case "D":
				peer, err = socks5Connect(c, func(addr string) (net.Conn, error) {
					return client.Dial("tcp", addr)
				})
			}
			if err != nil {
				return
			}
			r.track(peer, true)
			defer r.track(peer, false)
			pipe(c, peer)
		}()
	}
}

// pipe copies both ways until either side is done, then closes both.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	a.Close()
	b.Close()
	<-done
}

// stopForwards closes all rules (the SSH connection is going away). Another
// tab connected to the same server takes over the ones that were running.
func (t *tab) stopForwards() {
	t.app.fwdMu.Lock()
	defer t.app.fwdMu.Unlock()
	t.fwd.mu.Lock()
	rules := t.fwd.rules
	t.fwd.rules = nil
	t.fwd.mu.Unlock()
	var handOver []Forward
	for _, r := range rules {
		if r.running() {
			handOver = append(handOver, r.f)
		}
		r.stop()
	}
	if len(handOver) == 0 {
		return
	}
	for _, f := range handOver {
	next:
		for _, o := range t.app.sameServerTabs(t) {
			for _, r := range o.tabRules() {
				if c := o.sshClient(); c != nil && r.f.same(f) && r.status().Standby {
					o.listenForward(c, r)
					o.emit("fwd:changed")
					break next
				}
			}
		}
	}
}

func (t *tab) tabRules() []*forwardRule {
	t.fwd.mu.Lock()
	defer t.fwd.mu.Unlock()
	return append([]*forwardRule(nil), t.fwd.rules...)
}

// sshClient is the tab's SSH connection, or nil.
func (t *tab) sshClient() *ssh.Client {
	if s, _ := t.session().(*sshSession); s != nil {
		return s.client
	}
	return nil
}

// sameServerTabs are the other tabs with an SSH connection to t's server.
func (a *App) sameServerTabs(t *tab) []*tab {
	t.mu.Lock()
	host, port := t.host, t.port
	t.mu.Unlock()
	a.mu.Lock()
	all := make([]*tab, 0, len(a.tabs))
	for _, o := range a.tabs {
		all = append(all, o)
	}
	a.mu.Unlock()
	var out []*tab
	for _, o := range all {
		if o == t || o.sshClient() == nil {
			continue
		}
		o.mu.Lock()
		same := o.host == host && o.port == port
		o.mu.Unlock()
		if same {
			out = append(out, o)
		}
	}
	return out
}

// runningElsewhere tells whether another tab to t's server runs a rule like f.
func (a *App) runningElsewhere(t *tab, f Forward) bool {
	for _, o := range a.sameServerTabs(t) {
		for _, r := range o.tabRules() {
			if r.f.same(f) && r.running() {
				return true
			}
		}
	}
	return false
}

func (t *tab) forwardList() []ForwardStatus {
	t.fwd.mu.Lock()
	defer t.fwd.mu.Unlock()
	list := make([]ForwardStatus, 0, len(t.fwd.rules))
	for _, r := range t.fwd.rules {
		list = append(list, r.status())
	}
	return list
}

// forwardConfigs returns the rules as saved in the host history.
func (t *tab) forwardConfigs() []Forward {
	t.fwd.mu.Lock()
	defer t.fwd.mu.Unlock()
	list := make([]Forward, 0, len(t.fwd.rules))
	for _, r := range t.fwd.rules {
		list = append(list, r.f)
	}
	return list
}

// ---- SOCKS5 (RFC 1928), CONNECT without authentication ----

// socks5Connect answers a SOCKS5 client on c and connects to the requested
// address with dial. Returns the connection to the target.
func socks5Connect(c net.Conn, dial func(addr string) (net.Conn, error)) (net.Conn, error) {
	buf := make([]byte, 262)
	// Greeting: VER NMETHODS METHODS...
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return nil, err
	}
	if buf[0] != 5 {
		return nil, errors.New("SOCKS5가 아닙니다")
	}
	if _, err := io.ReadFull(c, buf[:buf[1]]); err != nil {
		return nil, err
	}
	if _, err := c.Write([]byte{5, 0}); err != nil { // no authentication
		return nil, err
	}

	// Request: VER CMD RSV ATYP ADDR PORT
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return nil, err
	}
	reply := func(code byte) {
		_, _ = c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
	}
	if buf[1] != 1 { // only CONNECT
		reply(7)
		return nil, errors.New("지원하지 않는 SOCKS 명령")
	}
	var host string
	switch buf[3] {
	case 1: // IPv4
		if _, err := io.ReadFull(c, buf[:4]); err != nil {
			return nil, err
		}
		host = net.IP(buf[:4]).String()
	case 3: // domain name
		if _, err := io.ReadFull(c, buf[:1]); err != nil {
			return nil, err
		}
		n := int(buf[0])
		if _, err := io.ReadFull(c, buf[:n]); err != nil {
			return nil, err
		}
		host = string(buf[:n])
	case 4: // IPv6
		if _, err := io.ReadFull(c, buf[:16]); err != nil {
			return nil, err
		}
		host = net.IP(buf[:16]).String()
	default:
		reply(8)
		return nil, errors.New("지원하지 않는 주소 형식")
	}
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return nil, err
	}
	port := binary.BigEndian.Uint16(buf[:2])

	peer, err := dial(net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		reply(5) // connection refused
		return nil, err
	}
	reply(0)
	return peer, nil
}

// ---- App bindings ----

func (a *App) sshTab(tabID int) (*tab, *ssh.Client, error) {
	t := a.findTab(tabID)
	if t == nil {
		return nil, nil, errors.New("연결되어 있지 않습니다")
	}
	sess, _ := t.session().(*sshSession)
	if sess == nil {
		return nil, nil, errors.New("포트 포워딩은 SSH 접속에서만 사용할 수 있습니다")
	}
	return t, sess.client, nil
}

// ForwardList returns the forwarding rules of a tab's SSH connection.
func (a *App) ForwardList(tabID int) ([]ForwardStatus, error) {
	t, _, err := a.sshTab(tabID)
	if err != nil {
		return nil, err
	}
	return t.forwardList(), nil
}

// ForwardAdd starts a rule on a tab's SSH connection and remembers it for the
// host, so it starts again on the next connection.
func (a *App) ForwardAdd(tabID int, f Forward) (ForwardStatus, error) {
	t, client, err := a.sshTab(tabID)
	if err != nil {
		return ForwardStatus{}, err
	}
	if f, err = f.normalize(); err != nil {
		return ForwardStatus{}, err
	}
	a.fwdMu.Lock()
	defer a.fwdMu.Unlock()
	for _, o := range t.forwardConfigs() {
		if o.same(f) {
			return ForwardStatus{}, fmt.Errorf("이미 같은 수신 주소(%s)의 규칙이 있습니다", f.bind())
		}
	}
	r := t.startForward(client, f, false)
	t.saveForwards()
	// The other tabs to this server keep it on standby.
	for _, o := range a.sameServerTabs(t) {
		dup := false
		for _, c := range o.forwardConfigs() {
			dup = dup || c.same(f)
		}
		if !dup {
			o.startForward(nil, f, true)
			o.emit("fwd:changed")
		}
	}
	return r.status(), nil
}

// ForwardRemove stops a rule and forgets it for the host.
func (a *App) ForwardRemove(tabID, id int) error {
	t, _, err := a.sshTab(tabID)
	if err != nil {
		return err
	}
	a.fwdMu.Lock()
	defer a.fwdMu.Unlock()
	removed := t.removeForward(func(r *forwardRule) bool { return r.id == id })
	if removed != nil {
		t.saveForwards()
		// It is gone for the server: also from the other tabs to it.
		for _, o := range a.sameServerTabs(t) {
			if o.removeForward(func(r *forwardRule) bool { return r.f.same(removed.f) }) != nil {
				o.emit("fwd:changed")
			}
		}
	}
	return nil
}

// removeForward stops and drops the first rule match accepts.
func (t *tab) removeForward(match func(*forwardRule) bool) *forwardRule {
	t.fwd.mu.Lock()
	var removed *forwardRule
	for i, r := range t.fwd.rules {
		if match(r) {
			removed = r
			t.fwd.rules = append(t.fwd.rules[:i], t.fwd.rules[i+1:]...)
			break
		}
	}
	t.fwd.mu.Unlock()
	if removed != nil {
		removed.stop()
	}
	return removed
}

func (t *tab) saveForwards() {
	t.mu.Lock()
	host, port := t.host, t.port
	t.mu.Unlock()
	_ = setHistoryForwards(host, port, t.forwardConfigs())
}

// restoreForwards starts the rules saved for the host of a new SSH
// connection; those another tab to the server already runs wait on standby.
func (t *tab) restoreForwards(sess *sshSession, host string, port int) {
	t.app.fwdMu.Lock()
	defer t.app.fwdMu.Unlock()
	for _, f := range historyForwards(host, port) {
		if f, err := f.normalize(); err == nil {
			t.startForward(sess.client, f, t.app.runningElsewhere(t, f))
		}
	}
	t.emit("fwd:changed")
}
