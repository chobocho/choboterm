package main

// Docker containers: the container window lists the containers of a tab's SSH
// server, or of this PC (Docker Desktop), and opens "docker exec" / "docker
// logs" in new tabs. On a server those tabs are extra sessions on the tab's
// SSH connection, so they need no login and end with it. Podman is used when
// docker isn't installed.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// DockerPort is a container port published on the host.
type DockerPort struct {
	IP   string `json:"ip"`   // host address it listens on ("0.0.0.0", "::", "127.0.0.1"...)
	Port int    `json:"port"` // host port
	To   string `json:"to"`   // container side, e.g. "8888/tcp"
}

// Container is one line of "docker ps".
type Container struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Image     string       `json:"image"`
	Status    string       `json:"status"` // "Up 3 hours", "Exited (0) 2 days ago"...
	Running   bool         `json:"running"`
	Ports     string       `json:"ports"`
	Published []DockerPort `json:"published"` // TCP ports on the host, one per port number
	Created   string       `json:"created"`   // "3 hours ago"
}

const dockerTimeout = 20 * time.Second

// One container per line; the fields exist in old docker versions and podman.
const dockerPsFormat = "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}\t{{.RunningFor}}"

// The shell inside a container: bash when it has one.
const dockerShell = "command -v bash >/dev/null 2>&1 && exec bash || exec sh"

// noDocker is printed by the server-side script when neither docker nor podman exists.
const noDocker = "__CHOBOTERM_NO_DOCKER__"

var errNoDocker = errors.New("docker(또는 podman)를 찾을 수 없습니다")

var containerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// shQuote quotes s as one word for a POSIX shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// remoteDocker is a shell command running docker (or podman) with args on a server.
func remoteDocker(args ...string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shQuote(a)
	}
	script := `d=$(command -v docker || command -v podman) || { echo ` + noDocker + ` >&2; exit 127; }; exec "$d" ` + strings.Join(q, " ")
	return "sh -c " + shQuote(script)
}

// localDocker finds docker (or podman) on this PC.
func localDocker() (string, error) {
	for _, name := range []string{"docker", "podman"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errNoDocker
}

// winQuote quotes an argument for a Windows command line (CommandLineToArgvW rules).
func winQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, c := range s {
		switch c {
		case '\\':
			slashes++
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*slashes+1))
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		b.WriteRune(c)
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}

// dockerError turns docker's error output into a message for the user.
func dockerError(stderr string, err error) error {
	msg := strings.TrimSpace(stderr)
	switch low := strings.ToLower(msg); {
	case strings.Contains(msg, noDocker):
		return errNoDocker
	case strings.Contains(low, "permission denied") && strings.Contains(low, "docker"):
		return errors.New("docker를 쓸 권한이 없습니다. 서버에서 sudo usermod -aG docker $USER 후 다시 로그인하세요")
	case strings.Contains(low, "cannot connect to the docker daemon"),
		strings.Contains(low, "error during connect"),
		strings.Contains(low, "docker daemon is not running"):
		return errors.New("Docker가 실행 중이 아닙니다 (PC에서는 Docker Desktop을 켜세요)")
	}
	if msg == "" {
		return err
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return errors.New(msg)
}

// runDocker runs docker with args on the server of SSH tab src, or on this PC
// when src is 0, and returns its output.
func (a *App) runDocker(src int, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if src == 0 {
		exe, err := localDocker()
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		hideWindow(cmd)
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return "", errors.New("docker가 응답하지 않습니다")
			}
			return "", dockerError(stderr.String(), err)
		}
		return stdout.String(), nil
	}

	client, err := a.dockerClient(src)
	if err != nil {
		return "", err
	}
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	session.Stdout, session.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(remoteDocker(args...)) }()
	select {
	case err = <-done:
	case <-time.After(dockerTimeout):
		return "", errors.New("docker가 응답하지 않습니다")
	}
	if err != nil {
		return "", dockerError(stderr.String(), err)
	}
	return stdout.String(), nil
}

// dockerClient is the SSH connection of tab src.
func (a *App) dockerClient(src int) (*ssh.Client, error) {
	t := a.findTab(src)
	if t == nil {
		return nil, errors.New("연결되어 있지 않습니다")
	}
	sess, _ := t.session().(*sshSession)
	if sess == nil {
		return nil, errors.New("서버의 컨테이너는 SSH 접속에서만 볼 수 있습니다")
	}
	return sess.client, nil
}

// parsePorts reads docker's port list, e.g.
// "0.0.0.0:8888->8888/tcp, :::8888->8888/tcp, 5432/tcp".
func parsePorts(s string) []DockerPort {
	var out []DockerPort
	seen := map[int]bool{}
	for _, p := range strings.Split(s, ",") {
		host, to, ok := strings.Cut(strings.TrimSpace(p), "->")
		if !ok || !strings.HasSuffix(to, "/tcp") {
			continue
		}
		i := strings.LastIndexByte(host, ':')
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(host[i+1:])
		if err != nil || port <= 0 || port > 65535 || seen[port] {
			continue
		}
		ip := strings.Trim(host[:i], "[]")
		if ip == "" || ip == "::" {
			ip = "0.0.0.0"
		}
		seen[port] = true
		out = append(out, DockerPort{IP: ip, Port: port, To: to})
	}
	return out
}

// parseContainers reads "docker ps" output in dockerPsFormat.
func parseContainers(out string) []Container {
	list := []Container{}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 6 || f[0] == "" {
			continue
		}
		c := Container{ID: f[0], Name: f[1], Image: f[2], Status: f[3], Ports: f[4], Created: f[5]}
		c.Running = strings.HasPrefix(c.Status, "Up")
		c.Published = parsePorts(c.Ports)
		list = append(list, c)
	}
	return list
}

// DockerList lists the containers on the server of SSH tab src, or on this PC
// when src is 0; all includes stopped ones.
func (a *App) DockerList(src int, all bool) ([]Container, error) {
	args := []string{"ps", "--format", dockerPsFormat}
	if all {
		args = append(args, "-a")
	}
	out, err := a.runDocker(src, args...)
	if err != nil {
		return nil, err
	}
	return parseContainers(out), nil
}

// DockerAction starts, stops or restarts a container.
func (a *App) DockerAction(src int, id, action string) error {
	switch action {
	case "start", "stop", "restart":
	default:
		return fmt.Errorf("알 수 없는 동작: %s", action)
	}
	if !containerID.MatchString(id) {
		return fmt.Errorf("컨테이너 이름이 올바르지 않습니다: %s", id)
	}
	_, err := a.runDocker(src, action, id)
	return err
}

// dockerArgs is the docker command line for mode: "shell" (docker exec) or "logs".
func dockerArgs(id, mode string) ([]string, error) {
	if !containerID.MatchString(id) {
		return nil, fmt.Errorf("컨테이너 이름이 올바르지 않습니다: %s", id)
	}
	switch mode {
	case "shell":
		return []string{"exec", "-it", "-e", "TERM=xterm-256color", id, "sh", "-c", dockerShell}, nil
	case "logs":
		return []string{"logs", "-f", "--tail", "200", id}, nil
	}
	return nil, fmt.Errorf("알 수 없는 동작: %s", mode)
}

// dockerExec is a "docker exec" session sharing another tab's SSH connection:
// closing it must leave that connection open.
type dockerExec struct {
	*sshSession
}

func (d dockerExec) Close() error { return d.session.Close() }

// DockerOpen runs a shell in container id ("shell") or follows its log
// ("logs") in tab tabID: on the server of SSH tab src, or on this PC when src
// is 0. label names the tab's session in its log.
func (a *App) DockerOpen(tabID, src int, id, mode, label string, cols, rows int) error {
	args, err := dockerArgs(id, mode)
	if err != nil {
		return err
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	var sess Session
	if src == 0 {
		exe, err := localDocker()
		if err != nil {
			return err
		}
		cmd := winQuote(exe)
		for _, a := range args {
			cmd += " " + winQuote(a)
		}
		if sess, err = startPty(cmd, localHome(), cols, rows); err != nil {
			return err
		}
	} else {
		client, err := a.dockerClient(src)
		if err != nil {
			return err
		}
		s, err := openPty(client, cols, rows, remoteDocker(args...))
		if err != nil {
			return err
		}
		sess = dockerExec{s}
	}
	t := a.getTab(tabID)
	t.disconnect()
	t.codec.Set(EncodingUTF8)
	t.start(sess, "docker", ConnectRequest{Host: label})
	return nil
}
