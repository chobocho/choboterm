package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"choboterm/internal/localshell"
	"choboterm/internal/sshconn"
	"choboterm/internal/testutil"
)

func TestParseContainers(t *testing.T) {
	out := "0123abcd\tweb\tnginx:1.27\tUp 3 hours\t0.0.0.0:8080->80/tcp, :::8080->80/tcp, 443/tcp\t3 hours ago\r\n" +
		"4567ef01\tdb\tpostgres\tExited (0) 2 days ago\t\t2 days ago\n" +
		"89ab\tjup\tjupyter\tUp 5 minutes (Paused)\t127.0.0.1:8888->8888/tcp, [::1]:9000->9000/tcp, 0.0.0.0:53->53/udp\t5 minutes ago\n" +
		"\n"
	got := parseContainers(out)
	want := []Container{
		{ID: "0123abcd", Name: "web", Image: "nginx:1.27", Status: "Up 3 hours", Running: true,
			Ports: "0.0.0.0:8080->80/tcp, :::8080->80/tcp, 443/tcp", Created: "3 hours ago",
			Published: []DockerPort{{IP: "0.0.0.0", Port: 8080, To: "80/tcp"}}},
		{ID: "4567ef01", Name: "db", Image: "postgres", Status: "Exited (0) 2 days ago", Created: "2 days ago"},
		{ID: "89ab", Name: "jup", Image: "jupyter", Status: "Up 5 minutes (Paused)", Running: true,
			Ports: "127.0.0.1:8888->8888/tcp, [::1]:9000->9000/tcp, 0.0.0.0:53->53/udp", Created: "5 minutes ago",
			Published: []DockerPort{{IP: "127.0.0.1", Port: 8888, To: "8888/tcp"}, {IP: "::1", Port: 9000, To: "9000/tcp"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	if got := parseContainers(""); got == nil || len(got) != 0 {
		t.Errorf("empty output: %#v", got)
	}
}

func TestDockerArgs(t *testing.T) {
	if _, err := dockerArgs("web; rm -rf /", "shell"); err == nil {
		t.Error("bad container name accepted")
	}
	if _, err := dockerArgs("web", "rm"); err == nil {
		t.Error("unknown mode accepted")
	}
	got, _ := dockerArgs("web", "logs")
	if strings.Join(got, " ") != "logs -f --tail 200 web" {
		t.Errorf("logs: %q", got)
	}
}

func TestWinQuote(t *testing.T) {
	for in, want := range map[string]string{
		`docker`:                 `docker`,
		`C:\Program Files\d.exe`: `"C:\Program Files\d.exe"`,
		dockerShell:              `"` + dockerShell + `"`,
		`a"b`:                    `"a\"b"`,
		`end\ `:                  `"end\ "`,
		`x y\`:                   `"x y\\"`,
		``:                       `""`,
	} {
		if got := localshell.WinQuote(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDockerError(t *testing.T) {
	if err := dockerError("", "x\n"+noDocker+"\n", nil); err.Error() != errNoEngine("").Error() {
		t.Errorf("no docker: %v", err)
	}
	if err := dockerError("", "permission denied while trying to connect to the Docker daemon socket", nil); !strings.Contains(err.Error(), "usermod") {
		t.Errorf("permission: %v", err)
	}
	for _, msg := range []string{
		// Docker Desktop not running (docker CLI 29 on Windows)
		"failed to connect to the docker API at npipe:////./pipe/docker_engine; check if the path is correct and if the daemon is running: open //./pipe/docker_engine: The system cannot find the file specified.",
		"Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?",
		`error during connect: Get "http://127.0.0.1:1/v1.52/containers/json": dial tcp 127.0.0.1:1: connectex: refused`,
	} {
		if err := dockerError("", msg, nil); !strings.Contains(err.Error(), "실행 중이 아닙니다") {
			t.Errorf("%q: %v", msg, err)
		}
	}
	if err := dockerError("", "permission denied while trying to connect to the docker API at unix:///var/run/docker.sock", nil); !strings.Contains(err.Error(), "usermod") {
		t.Errorf("permission (docker 29): %v", err)
	}
	if err := dockerError("", "Error: No such container: x\nmore", nil); err.Error() != "Error: No such container: x" {
		t.Errorf("first line: %v", err)
	}
}

// startDockerSSHServer serves "exec" (with or without "pty-req") by running the
// command in WSL with a fake docker first in PATH. It prints its arguments one
// per line, and "ps" prints two containers.
func startDockerSSHServer(t *testing.T) string {
	t.Helper()
	// The script goes in on stdin: wsl.exe would expand the $ in an argument.
	testutil.WSLRun(t, "rm -rf /tmp/fakedocker && mkdir -p /tmp/fakedocker && cat > /tmp/fakedocker/docker && chmod +x /tmp/fakedocker/docker", []byte(`#!/bin/sh
if [ "$1" = ps ]; then
  printf 'c1\tweb\tnginx\tUp 1 hour\t0.0.0.0:8080->80/tcp\t1 hour ago\n'
  [ "$4" = -a ] && printf 'c2\told\tbusybox\tExited (1) 1 day ago\t\t1 day ago\n'
  exit 0
fi
if [ "$1" = stop ] && [ "$2" = gone ]; then echo "Error: No such container: gone" >&2; exit 1; fi
for a in "$@"; do echo "arg:$a"; done
`))

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
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
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, chReqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						for req := range chReqs {
							switch req.Type {
							case "pty-req", "window-change":
								req.Reply(true, nil)
								continue
							case "exec":
							default:
								req.Reply(false, nil)
								continue
							}
							req.Reply(true, nil)
							n := binary.BigEndian.Uint32(req.Payload)
							// The command goes in on stdin: wsl.exe would mangle quotes and $ in an argument.
							cmd := exec.Command("wsl.exe", "--", "sh")
							cmd.Stdin = strings.NewReader("PATH=/tmp/fakedocker:$PATH; " + string(req.Payload[4:4+n]) + "\n")
							cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
							status := uint32(0)
							if err := cmd.Run(); err != nil {
								status = 1
								if ee, ok := err.(*exec.ExitError); ok {
									status = uint32(ee.ExitCode())
								}
							}
							ch.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, status))
							ch.Close()
							return
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

// TestDockerRemote lists containers and opens a docker exec tab over a tab's
// SSH connection, then checks closing that tab leaves the connection open.
//
//	CHOBOTERM_WSL=1 go test -run DockerRemote -v
func TestDockerRemote(t *testing.T) {
	if os.Getenv("CHOBOTERM_WSL") == "" {
		t.Skip("CHOBOTERM_WSL not set")
	}
	client, err := ssh.Dial("tcp", startDockerSSHServer(t), &ssh.ClientConfig{
		User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	a := NewApp()
	data := make(chan string, 64)
	closed := make(chan string, 4)
	a.hooks.emit = func(name string, args ...interface{}) {
		switch name {
		case "term:data":
			if args[0] == 2 {
				data <- args[1].(string)
			}
		case "term:closed":
			if args[0] == 2 {
				closed <- args[1].(string)
			}
		}
	}
	a.getTab(1).sess = &sshconn.Session{Client: client}

	list, err := a.DockerList(1, false, "")
	if err != nil || len(list) != 1 || list[0].Name != "web" || !list[0].Running || list[0].Published[0].Port != 8080 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if list, err = a.DockerList(1, true, ""); err != nil || len(list) != 2 || list[1].Running {
		t.Fatalf("list -a: %+v %v", list, err)
	}
	if err := a.DockerAction(1, "c1", "restart", ""); err != nil {
		t.Errorf("restart: %v", err)
	}
	if err := a.DockerAction(1, "gone", "stop", ""); err == nil || err.Error() != "Error: No such container: gone" {
		t.Errorf("stop gone: %v", err)
	}
	if _, err := a.DockerList(9, false, ""); err == nil {
		t.Error("list on a tab that isn't connected")
	}

	if err := a.DockerOpen(2, 1, "c1", "shell", "web@test", 80, 24, ""); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	timeout := time.After(10 * time.Second)
wait:
	for {
		select {
		case d := <-data:
			b, _ := base64.StdEncoding.DecodeString(d)
			out.Write(b)
		case <-closed:
			break wait
		case <-timeout:
			t.Fatalf("no end; output %q", out.String())
		}
	}
	want := "arg:exec\narg:-it\narg:-e\narg:TERM=xterm-256color\narg:c1\narg:sh\narg:-c\narg:" + dockerShell + "\n"
	if out.String() != want {
		t.Errorf("exec output:\n%q\nwant\n%q", out.String(), want)
	}

	// The exec tab ended and was closed; the first tab's connection still works.
	a.CloseTab(2)
	if _, err := a.DockerList(1, false, ""); err != nil {
		t.Errorf("connection closed with the exec tab: %v", err)
	}
}

func TestEngineChoice(t *testing.T) {
	for engine, want := range map[string]string{"": "docker podman", "docker": "docker", "podman": "podman"} {
		names, err := engineNames(engine)
		if err != nil || strings.Join(names, " ") != want {
			t.Errorf("%q: %v %v", engine, names, err)
		}
	}
	if _, err := engineNames("lxc"); err == nil {
		t.Error("an unknown engine was taken")
	}
	// on a server: the commands looked for, in order
	if s := remoteDocker([]string{"podman"}, "ps"); !strings.Contains(s, "command -v podman)") || strings.Contains(s, "docker") {
		t.Errorf("podman only: %s", s)
	}
	if s := remoteDocker([]string{"docker", "podman"}, "ps"); !strings.Contains(s, "command -v docker || command -v podman)") {
		t.Errorf("auto: %s", s)
	}
	for engine, want := range map[string]string{"": "docker(또는 podman)를", "docker": "docker를", "podman": "podman을"} {
		if err := dockerError(engine, noDocker, nil); !strings.Contains(err.Error(), want) {
			t.Errorf("%q not found: %v", engine, err)
		}
	}
}

func TestPodmanErrors(t *testing.T) {
	for msg, want := range map[string]string{
		// podman on Windows / macOS with its VM stopped
		"Cannot connect to Podman. Please verify your connection to the Linux system using `podman system connection list`, or try `podman machine init` and `podman machine start` to manage a new Linux VM\nError: unable to connect to Podman socket": "podman machine start",
		"Error: unable to connect to Podman socket: Get \"http://d/v5.2.1/libpod/_ping\": dial unix /run/user/1000/podman/podman.sock: connect: no such file or directory":                                                                               "podman.socket",
	} {
		if err := dockerError("podman", msg, nil); !strings.Contains(err.Error(), want) {
			t.Errorf("%.40q: %v, want %q", msg, err, want)
		}
	}
}

// fakeEngines puts shell scripts named docker and/or podman alone on PATH.
// Each prints its name for ps and a version for "version"; podman's
// "version" may fail after printing, as with its VM stopped.
func fakeEngines(t *testing.T, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts as commands")
	}
	dir := t.TempDir()
	scripts := map[string]string{
		"docker": "#!/bin/sh\n" +
			"if [ \"$1\" = version ]; then echo 27.3.1; exit 0; fi\n" +
			"if [ \"$1\" = ps ]; then printf 'd1\\tfromdocker\\tnginx\\tUp 1 hour\\t\\t1 hour ago\\n'; exit 0; fi\n" +
			"echo \"docker $*\"\n",
		"podman": "#!/bin/sh\n" +
			"if [ \"$1\" = version ]; then echo 5.2.1; echo 'Cannot connect to Podman. try podman machine start' >&2; exit 125; fi\n" +
			"if [ \"$1\" = ps ]; then printf 'p1\\tfrompodman\\tbusybox\\tUp 2 minutes\\t0.0.0.0:8080->80/tcp\\t2 minutes ago\\n'; exit 0; fi\n" +
			"if [ \"$1\" = stop ]; then echo 'Cannot connect to Podman. Please verify your connection, or try podman machine init and podman machine start' >&2; exit 125; fi\n" +
			"echo \"podman $*\"\n",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(scripts[n]), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestLocalEngines(t *testing.T) {
	fakeEngines(t, "docker", "podman")
	a := NewApp()
	for engine, want := range map[string]DockerEngine{
		"":       {Name: "docker", Version: "27.3.1"},
		"docker": {Name: "docker", Version: "27.3.1"},
		"podman": {Name: "podman", Version: "5.2.1"}, // printed, then failed: still told
	} {
		if got, err := a.DockerEngineInfo(0, engine); err != nil || got != want {
			t.Errorf("%q: %+v %v, want %+v", engine, got, err, want)
		}
	}
	list, err := a.DockerList(0, false, "podman")
	if err != nil || len(list) != 1 || list[0].Name != "frompodman" || list[0].Published[0].Port != 8080 {
		t.Fatalf("podman list: %+v %v", list, err)
	}
	if list, err = a.DockerList(0, false, ""); err != nil || list[0].Name != "fromdocker" {
		t.Fatalf("auto list: %+v %v", list, err)
	}
	if err := a.DockerAction(0, "p1", "stop", "podman"); err == nil || !strings.Contains(err.Error(), "podman machine start") {
		t.Fatalf("podman with its VM stopped: %v", err)
	}
}

func TestLocalEngineFallback(t *testing.T) {
	fakeEngines(t, "podman")
	a := NewApp()
	if got, err := a.DockerEngineInfo(0, ""); err != nil || got.Name != "podman" {
		t.Fatalf("auto without docker: %+v %v", got, err)
	}
	if _, err := a.DockerEngineInfo(0, "docker"); err == nil || err.Error() != "docker를 찾을 수 없습니다" {
		t.Fatalf("docker chosen, not there: %v", err)
	}
	if _, err := a.DockerList(0, false, "docker"); err == nil || err.Error() != "docker를 찾을 수 없습니다" {
		t.Fatalf("docker chosen, not there: %v", err)
	}
}
