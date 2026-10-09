package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// tabOutput collects what the app sends to each tab.
type tabOutput struct {
	mu     sync.Mutex
	out    map[int]*strings.Builder
	closed map[int]string
}

func newTabOutput(a *App) *tabOutput {
	o := &tabOutput{out: map[int]*strings.Builder{}, closed: map[int]string{}}
	a.hooks.emit = func(name string, args ...interface{}) {
		if len(args) < 2 {
			return
		}
		id, ok := args[0].(int)
		if !ok {
			return
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		switch name {
		case "term:data":
			b, _ := base64.StdEncoding.DecodeString(args[1].(string))
			if o.out[id] == nil {
				o.out[id] = &strings.Builder{}
			}
			o.out[id].Write(b)
		case "term:closed":
			o.closed[id] = args[1].(string)
		}
	}
	return o
}

func (o *tabOutput) text(id int) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.out[id] == nil {
		return ""
	}
	return o.out[id].String()
}

func (o *tabOutput) isClosed(id int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.closed[id]
	return ok
}

// waitFor waits until cond holds, or fails with the tab's output.
func (o *tabOutput) waitFor(t *testing.T, id int, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("tab %d: no %s; output:\n%s", id, what, o.text(id))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (o *tabOutput) waitText(t *testing.T, id int, s string) {
	t.Helper()
	o.waitFor(t, id, strconv.Quote(s), func() bool { return strings.Contains(o.text(id), s) })
}

// TestDockerLive runs the container window's calls against a real SSH server
// with a real Docker, the way the app uses them. The containers are made by:
//
//	docker run -d --name ct-web -p 8080:80 nginx:alpine
//	docker run -d --name ct-ubu ubuntu:24.04 sleep infinity
//	docker run -d --name ct-log busybox sh -c 'i=0; while true; do echo log-line-$i; i=$((i+1)); sleep 1; done'
//	docker run --name ct-done alpine true
//
// and the test runs with the server, a login in the docker group and its key:
//
//	CHOBOTERM_DOCKER_SSH=localhost:22 CHOBOTERM_DOCKER_USER=me CHOBOTERM_DOCKER_KEY=path/to/key go test -run DockerLive -v
func TestDockerLive(t *testing.T) {
	addr := os.Getenv("CHOBOTERM_DOCKER_SSH")
	if addr == "" {
		t.Skip("CHOBOTERM_DOCKER_SSH not set")
	}
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	// A home of its own: an ssh config with the key, and a fresh known_hosts.
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(home, ".ssh"), 0o700)
	conf := fmt.Sprintf("Host dockertest\n  HostName %s\n  Port %d\n  User %s\n  IdentityFile %s\n",
		host, port, os.Getenv("CHOBOTERM_DOCKER_USER"), os.Getenv("CHOBOTERM_DOCKER_KEY"))
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	hist := filepath.Join(home, "hosts.json")
	origHist := historyFile
	historyFile = func() (string, error) { return hist, nil }
	defer func() { historyFile = origHist }()
	useTempSettings(t)

	a := NewApp()
	o := newTabOutput(a)
	a.hooks.confirmKey = func(string, string) bool { return true }
	if p, err := a.Connect(1, ConnectRequest{Host: "dockertest", Port: 22, Cols: 80, Rows: 24}); err != nil || p != "ssh" {
		t.Fatalf("connect: %q %v", p, err)
	}
	defer a.CloseTab(1)

	byName := func(all bool) map[string]Container {
		t.Helper()
		list, err := a.DockerList(1, all)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		m := map[string]Container{}
		for _, c := range list {
			m[c.Name] = c
		}
		return m
	}

	// The list: running ones only, then with stopped ones.
	m := byName(false)
	for _, n := range []string{"ct-web", "ct-ubu", "ct-log"} {
		if !m[n].Running {
			t.Errorf("%s not listed as running: %+v", n, m[n])
		}
	}
	if _, ok := m["ct-done"]; ok {
		t.Error("stopped container listed without all")
	}
	if p := m["ct-web"].Published; len(p) != 1 || p[0].Port != 8080 || p[0].To != "80/tcp" {
		t.Errorf("ct-web ports %q -> %+v", m["ct-web"].Ports, p)
	}
	if c, ok := byName(true)["ct-done"]; !ok || c.Running || !strings.HasPrefix(c.Status, "Exited") {
		t.Errorf("ct-done with all: %+v %v", c, ok)
	}

	// A shell where bash exists: bash, with the tab's size, then exit ends the tab.
	if err := a.DockerOpen(2, 1, m["ct-ubu"].ID, "shell", "ct-ubu@test", 80, 24); err != nil {
		t.Fatal(err)
	}
	a.Resize(2, 101, 33)
	a.Send(2, "echo M-$((6*7)) $0; stty size\r")
	o.waitText(t, 2, "M-42 bash")
	o.waitText(t, 2, "33 101")
	a.Send(2, "exit\r")
	o.waitFor(t, 2, "end after exit", func() bool { return o.isClosed(2) })
	a.CloseTab(2)

	// No bash (alpine): sh.
	if err := a.DockerOpen(3, 1, "ct-web", "shell", "ct-web@test", 80, 24); err != nil {
		t.Fatal(err)
	}
	a.Send(3, "echo M-$((6*7)) $0 $(readlink /proc/$$/exe)\r")
	o.waitText(t, 3, "M-42 sh /bin/busybox")
	// Closing the tab while the shell runs leaves the connection open.
	a.CloseTab(3)

	// The log follows new lines.
	if err := a.DockerOpen(4, 1, "ct-log", "logs", "ct-log@test", 80, 24); err != nil {
		t.Fatal(err)
	}
	o.waitText(t, 4, "log-line-")
	before := strings.Count(o.text(4), "log-line-")
	o.waitFor(t, 4, "a new log line", func() bool { return strings.Count(o.text(4), "log-line-") > before })
	a.CloseTab(4)

	if _, err := a.DockerList(1, false); err != nil {
		t.Fatalf("connection gone after the container tabs: %v", err)
	}

	// Stop / start / restart.
	if err := a.DockerAction(1, "ct-log", "stop"); err != nil {
		t.Fatal(err)
	}
	if byName(false)["ct-log"].Running {
		t.Error("ct-log still running after stop")
	}
	if err := a.DockerAction(1, "ct-log", "start"); err != nil {
		t.Fatal(err)
	}
	if !byName(false)["ct-log"].Running {
		t.Error("ct-log not running after start")
	}
	if err := a.DockerAction(1, "ct-web", "restart"); err != nil {
		t.Fatal(err)
	}
	if err := a.DockerAction(1, "ct-nope", "stop"); err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Errorf("stop of a missing container: %v", err)
	}

	// "Open port": a local forward to the published port reaches nginx.
	lPort := freePort(t)
	if st, err := a.ForwardAdd(1, Forward{Type: "L", BindPort: lPort, Host: "127.0.0.1", Port: 8080}); err != nil || st.Error != "" {
		t.Fatalf("forward: %+v %v", st, err)
	}
	var page string
	for i := 0; i < 50; i++ { // nginx may still be restarting
		if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", lPort)); err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if page = string(b); strings.Contains(page, "nginx") {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(page, "Welcome to nginx") {
		t.Errorf("page through the forward: %.200q", page)
	}

	// The SSH tab ends: a container tab on it ends too.
	if err := a.DockerOpen(5, 1, "ct-ubu", "shell", "ct-ubu@test", 80, 24); err != nil {
		t.Fatal(err)
	}
	a.Send(5, "echo M-$((6*7))\r")
	o.waitText(t, 5, "M-42")
	a.Disconnect(1)
	o.waitFor(t, 5, "end with the SSH tab", func() bool { return o.isClosed(5) })
	a.CloseTab(5)
}

// TestDockerLocalLive runs the "this PC" side: docker on PATH (Docker Desktop
// or a docker CLI pointed at a daemon with DOCKER_HOST), with the containers
// of TestDockerLive.
//
//	CHOBOTERM_DOCKER_LOCAL=1 go test -run DockerLocalLive -v
func TestDockerLocalLive(t *testing.T) {
	if os.Getenv("CHOBOTERM_DOCKER_LOCAL") == "" {
		t.Skip("CHOBOTERM_DOCKER_LOCAL not set")
	}
	a := NewApp()
	o := newTabOutput(a)

	list, err := a.DockerList(0, true)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]Container{}
	for _, c := range list {
		m[c.Name] = c
	}
	if !m["ct-ubu"].Running || m["ct-done"].Running || len(m["ct-web"].Published) != 1 {
		t.Fatalf("list: %+v", list)
	}

	// The shell runs in a local pseudo console.
	if err := a.DockerOpen(1, 0, "ct-ubu", "shell", "ct-ubu@PC", 80, 24); err != nil {
		t.Fatal(err)
	}
	a.Resize(1, 101, 33)
	a.Send(1, "echo M-$((6*7)) $0; stty size\r")
	o.waitText(t, 1, "M-42 bash")
	o.waitText(t, 1, "33 101")
	a.Send(1, "exit\r")
	o.waitFor(t, 1, "end after exit", func() bool { return o.isClosed(1) })
	a.CloseTab(1)

	if err := a.DockerOpen(2, 0, "ct-web", "shell", "ct-web@PC", 80, 24); err != nil {
		t.Fatal(err)
	}
	a.Send(2, "echo M-$((6*7)) $0\r")
	o.waitText(t, 2, "M-42 sh")
	a.CloseTab(2)

	if err := a.DockerOpen(3, 0, "ct-log", "logs", "ct-log@PC", 80, 24); err != nil {
		t.Fatal(err)
	}
	o.waitText(t, 3, "log-line-")
	a.CloseTab(3)

	if err := a.DockerAction(0, "ct-log", "restart"); err != nil {
		t.Fatal(err)
	}
	if err := a.DockerAction(0, "ct-nope", "stop"); err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Errorf("stop of a missing container: %v", err)
	}
}
