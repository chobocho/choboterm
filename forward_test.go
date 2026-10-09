package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startForwardingServer runs an SSH server that, like OpenSSH, connects
// direct-tcpip channels (-L, -D) and listens for tcpip-forward requests (-R).
func startForwardingServer(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	key := ed25519Signer(t)
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
	cfg.AddHostKey(key)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveForwarding(nc, cfg)
		}
	}()
	return ln.Addr().String(), key.PublicKey()
}

func serveForwarding(nc net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	var (
		mu        sync.Mutex
		listeners = map[string]net.Listener{}
	)
	defer func() {
		mu.Lock()
		for _, l := range listeners {
			l.Close()
		}
		mu.Unlock()
	}()
	go func() {
		for r := range reqs {
			var p struct {
				Addr string
				Port uint32
			}
			switch r.Type {
			case "tcpip-forward":
				ssh.Unmarshal(r.Payload, &p)
				l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(p.Port))))
				if err != nil {
					r.Reply(false, nil)
					continue
				}
				port := uint32(l.Addr().(*net.TCPAddr).Port)
				mu.Lock()
				listeners[strconv.Itoa(int(port))] = l
				mu.Unlock()
				r.Reply(true, ssh.Marshal(struct{ Port uint32 }{port}))
				go func() {
					for {
						c, err := l.Accept()
						if err != nil {
							return
						}
						ch, chReqs, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(struct {
							Addr     string
							Port     uint32
							OrigAddr string
							OrigPort uint32
						}{p.Addr, port, "127.0.0.1", 1}))
						if err != nil {
							c.Close()
							continue
						}
						go ssh.DiscardRequests(chReqs)
						go pipeChannel(c, ch)
					}
				}()
			case "cancel-tcpip-forward":
				ssh.Unmarshal(r.Payload, &p)
				mu.Lock()
				if l := listeners[strconv.Itoa(int(p.Port))]; l != nil {
					l.Close()
				}
				mu.Unlock()
				r.Reply(true, nil)
			default:
				r.Reply(false, nil)
			}
		}
	}()
	for nch := range chans {
		switch nch.ChannelType() {
		case "session":
			ch, chReqs, err := nch.Accept()
			if err != nil {
				continue
			}
			go func() {
				for req := range chReqs {
					req.Reply(req.Type == "pty-req" || req.Type == "shell", nil)
					if req.Type == "shell" {
						ch.Write([]byte("$ "))
					}
				}
			}()
		case "direct-tcpip":
			var p struct {
				Host     string
				Port     uint32
				OrigHost string
				OrigPort uint32
			}
			ssh.Unmarshal(nch.ExtraData(), &p)
			c, err := net.Dial("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))))
			if err != nil {
				nch.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			ch, chReqs, err := nch.Accept()
			if err != nil {
				c.Close()
				continue
			}
			go ssh.DiscardRequests(chReqs)
			go pipeChannel(c, ch)
		default:
			nch.Reject(ssh.UnknownChannelType, "")
		}
	}
}

func pipeChannel(c net.Conn, ch ssh.Channel) {
	go func() {
		io.Copy(ch, c)
		ch.CloseWrite()
	}()
	io.Copy(c, ch)
	c.Close()
	ch.Close()
}

// startEcho answers every line with "echo: " + line.
func startEcho(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					c.Write([]byte("echo: " + line))
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// roundTrip sends a line over c (already connected to the echo server) and checks the answer.
func roundTrip(t *testing.T, c net.Conn, what string) {
	t.Helper()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("hi " + what + "\n")); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || line != "echo: hi "+what+"\n" {
		t.Fatalf("%s: got %q, %v", what, line, err)
	}
}

func dialThrough(t *testing.T, port int, what string) {
	t.Helper()
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	defer c.Close()
	roundTrip(t, c, what)
}

func socksThrough(t *testing.T, port int, target string) {
	t.Helper()
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	host, portStr, _ := net.SplitHostPort(target)
	p, _ := strconv.Atoi(portStr)
	req := []byte{5, 1, 0, 5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(p))
	c.Write(req)
	resp := make([]byte, 12)
	if _, err := io.ReadFull(c, resp); err != nil || resp[0] != 5 || resp[1] != 0 || resp[3] != 0 {
		t.Fatalf("socks reply % X, %v", resp, err)
	}
	roundTrip(t, c, "socks")
}

func TestPortForwarding(t *testing.T) {
	addr, key := startForwardingServer(t)
	useKnownHosts(t, addr, key)
	hist := filepath.Join(t.TempDir(), "hosts.json")
	origHist := historyFile
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = origHist }()
	useTempSettings(t)

	echo := startEcho(t)
	echoHost, echoPortStr, _ := net.SplitHostPort(echo)
	echoPort, _ := strconv.Atoi(echoPortStr)

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	a.hooks.confirmKey = func(string, string) bool { t.Fatal("unexpected host key prompt"); return false }
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	req := ConnectRequest{Host: host, Port: port, Login: "u", Pass: "p", Cols: 80, Rows: 24}
	if p, err := a.Connect(1, req); err != nil || p != "ssh" {
		t.Fatalf("connect: %q, %v", p, err)
	}
	defer a.CloseTab(1)

	lPort, dPort, rPort := freePort(t), freePort(t), freePort(t)
	for _, f := range []Forward{
		{Type: "L", BindPort: lPort, Host: echoHost, Port: echoPort},
		{Type: "D", BindPort: dPort},
		{Type: "R", BindPort: rPort, Host: echoHost, Port: echoPort},
	} {
		if st, err := a.ForwardAdd(1, f); err != nil || st.Error != "" {
			t.Fatalf("add %s: %+v, %v", f.Type, st, err)
		}
	}
	dialThrough(t, lPort, "local")
	socksThrough(t, dPort, echo)
	dialThrough(t, rPort, "remote") // the server listens there and connects back through us

	// The same listening address twice is refused.
	if _, err := a.ForwardAdd(1, Forward{Type: "L", BindPort: lPort, Host: "x", Port: 1}); err == nil {
		t.Fatal("duplicate rule accepted")
	}
	// A port in use is kept with its error, so the user sees why.
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port
	st, err := a.ForwardAdd(1, Forward{Type: "L", BindPort: busyPort, Host: echoHost, Port: echoPort})
	if err != nil || st.Error == "" {
		t.Fatalf("busy port: %+v, %v", st, err)
	}
	if _, err := a.ForwardAdd(1, Forward{Type: "L", BindPort: 0, Host: "x", Port: 1}); err == nil {
		t.Fatal("bad port accepted")
	}

	list, _ := a.ForwardList(1)
	if len(list) != 4 || len(historyForwards(host, port)) != 4 {
		t.Fatalf("list %+v, saved %+v", list, historyForwards(host, port))
	}
	if err := a.ForwardRemove(1, st.ID); err != nil {
		t.Fatal(err)
	}
	if got := historyForwards(host, port); len(got) != 3 {
		t.Fatalf("saved after remove: %+v", got)
	}

	// Disconnecting stops the listeners...
	a.Disconnect(1)
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(lPort), time.Second); err == nil {
		c.Close()
		t.Fatal("local forward still listening after disconnect")
	}
	// ...and the next connection to the host starts the saved rules again.
	if _, err := a.Connect(1, req); err != nil {
		t.Fatal(err)
	}
	dialThrough(t, lPort, "local again")
	socksThrough(t, dPort, echo)
	if list, _ := a.ForwardList(1); len(list) != 3 {
		t.Fatalf("restored %+v", list)
	}
	// Reconnecting must not lose the saved rules.
	if e := loadHistory(); len(e) != 1 || !strings.Contains(e[0].Host, "127.0.0.1") || len(historyForwards(e[0].Host, e[0].Port)) != 3 {
		t.Fatalf("history %+v, rules %+v", e, historyForwards(host, port))
	}
}

func TestForwardOnlyOverSSH(t *testing.T) {
	a := NewApp()
	tb := a.getTab(1)
	tb.sess = newPipeSession()
	if _, err := a.ForwardAdd(1, Forward{Type: "D", BindPort: 1080}); err == nil {
		t.Fatal("forwarding allowed without SSH")
	}
}

// TestForwardOneTabPerHost opens two tabs to the same server: only the first
// listens, the second waits, takes over when the first ends, and rules added
// or removed in one tab reach the other.
func TestForwardOneTabPerHost(t *testing.T) {
	addr, key := startForwardingServer(t)
	useKnownHosts(t, addr, key)
	hist := filepath.Join(t.TempDir(), "hosts.json")
	origHist := historyFile
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = origHist }()
	useTempSettings(t)

	echo := startEcho(t)
	echoHost, echoPortStr, _ := net.SplitHostPort(echo)
	echoPort, _ := strconv.Atoi(echoPortStr)

	a := NewApp()
	a.hooks.emit = func(string, ...interface{}) {}
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	req := ConnectRequest{Host: host, Port: port, Login: "u", Pass: "p", Cols: 80, Rows: 24}
	for _, id := range []int{1, 2} {
		if _, err := a.Connect(id, req); err != nil {
			t.Fatal(err)
		}
	}
	defer a.CloseTab(2)

	lPort := freePort(t)
	if st, err := a.ForwardAdd(1, Forward{Type: "L", BindPort: lPort, Host: echoHost, Port: echoPort}); err != nil || st.Error != "" || st.Standby {
		t.Fatalf("add: %+v, %v", st, err)
	}
	state := func(id int) string {
		list, _ := a.ForwardList(id)
		if len(list) != 1 {
			return fmt.Sprintf("%d rules", len(list))
		}
		if list[0].Error != "" {
			return "error " + list[0].Error
		}
		if list[0].Standby {
			return "standby"
		}
		return "listening"
	}
	if s1, s2 := state(1), state(2); s1 != "listening" || s2 != "standby" {
		t.Fatalf("after add: tab1 %s, tab2 %s", s1, s2)
	}
	dialThrough(t, lPort, "tab 1")

	// A third tab connecting later waits too, instead of failing on the port.
	if _, err := a.Connect(3, req); err != nil {
		t.Fatal(err)
	}
	if s := state(3); s != "standby" {
		t.Fatalf("tab 3 restored: %s", s)
	}

	// The listening tab ends: another one takes the rule over.
	a.CloseTab(1)
	if s2, s3 := state(2), state(3); !(s2 == "listening" && s3 == "standby" || s2 == "standby" && s3 == "listening") {
		t.Fatalf("after close: tab2 %s, tab3 %s", s2, s3)
	}
	dialThrough(t, lPort, "taken over")

	// Removing it in one tab removes it everywhere.
	list, _ := a.ForwardList(3)
	if err := a.ForwardRemove(3, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if s2, s3 := state(2), state(3); s2 != "0 rules" || s3 != "0 rules" {
		t.Fatalf("after remove: tab2 %s, tab3 %s", s2, s3)
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(lPort), time.Second); err == nil {
		c.Close()
		t.Fatal("still listening after remove")
	}
	a.CloseTab(3)
}
