package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type sshSession struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func (s *sshSession) Write(p []byte) (int, error) { return s.stdin.Write(p) }

// Read returns errConnLost when the output ends because the connection broke:
// the output also ends when the shell exits, but then the server reports an
// exit status first.
func (s *sshSession) Read(p []byte) (int, error) {
	n, err := s.stdout.Read(p)
	if err == io.EOF {
		done := make(chan error, 1)
		go func() { done <- s.session.Wait() }()
		select {
		case werr := <-done:
			debugf("ssh output EOF; session.Wait: %v", werr)
			var missing *ssh.ExitMissingError
			if errors.As(werr, &missing) {
				err = errConnLost
			}
		case <-time.After(5 * time.Second):
			debugf("ssh output EOF; session.Wait timed out")
		}
	}
	return n, err
}

// keepAlive asks the server for a reply, as OpenSSH's ServerAliveInterval does.
func (s *sshSession) keepAlive() error {
	done := make(chan error, 1)
	go func() {
		// Servers answer even requests they don't know (with a failure), which is enough.
		_, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(keepAliveTimeout):
		return errNoReply
	}
}
func (s *sshSession) Resize(cols, rows int) error { return s.session.WindowChange(rows, cols) }
func (s *sshSession) Close() error {
	_ = s.session.Close()
	return s.client.Close()
}

// hostKeyConfirmer asks the user to trust an unknown host key.
type hostKeyConfirmer func(host, fingerprint string) bool

// dialSSH opens an SSH shell. conn is an already connected socket (from
// protocol detection) or nil to dial req.Host:req.Port. keys are private key
// files (IdentityFile in ~/.ssh/config) to try before the default ones.
// ask prompts the user during login (key passphrases); nil cancels prompts.
func dialSSH(req ConnectRequest, confirm hostKeyConfirmer, ask asker, conn net.Conn, keys []string) (Session, error) {
	if req.Login == "" {
		return nil, errors.New("SSH 접속에는 Login이 필요합니다")
	}

	kh, err := loadKnownHosts(confirm)
	if err != nil {
		return nil, err
	}

	auth := newSSHAuth(req.Pass, keys, ask)
	defer auth.Close()
	cfg := &ssh.ClientConfig{
		User:            req.Login,
		Auth:            auth.methods,
		HostKeyCallback: kh.check,
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	if conn == nil {
		if conn, err = net.DialTimeout("tcp", addr, cfg.Timeout); err != nil {
			return nil, fmt.Errorf("SSH 접속 실패: %w", err)
		}
	}
	// Ask for the key type we already trust, as OpenSSH does. Otherwise the
	// server may offer e.g. its ECDSA key while known_hosts holds its Ed25519
	// key, which would look like a changed key.
	cfg.HostKeyAlgorithms = kh.algorithmsFor(addr, conn.RemoteAddr())
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SSH 접속 실패: %w", err)
	}
	client := ssh.NewClient(c, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}
	if err := session.RequestPty("xterm-256color", req.Rows, req.Cols, modes); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("PTY 요청 실패: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("셸 시작 실패: %w", err)
	}

	go func() { debugf("ssh connection to %s ended: %v", req.Host, client.Wait()) }()
	return &sshSession{client: client, session: session, stdin: stdin, stdout: stdout}, nil
}

// knownHosts verifies host keys against ~/.ssh/known_hosts. Unknown hosts are
// confirmed by the user and appended; changed keys are rejected.
type knownHosts struct {
	path    string
	lookup  ssh.HostKeyCallback
	confirm hostKeyConfirmer
}

func loadKnownHosts(confirm hostKeyConfirmer) (*knownHosts, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(path, os.O_CREATE, 0o600); err == nil {
		f.Close()
	} else {
		return nil, err
	}
	lookup, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	return &knownHosts{path: path, lookup: lookup, confirm: confirm}, nil
}

// probeKey is a throwaway key used to ask known_hosts which keys it holds for a host.
var probeKey = func() ssh.PublicKey {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, _ := ssh.NewPublicKey(pub)
	return k
}()

// algorithmsFor returns the host key algorithms matching the keys known for
// addr, or nil (library defaults) for an unknown host.
func (k *knownHosts) algorithmsFor(addr string, remote net.Addr) []string {
	var keyErr *knownhosts.KeyError
	if err := k.lookup(addr, remote, probeKey); !errors.As(err, &keyErr) {
		return nil
	}
	var algos []string
	add := func(names ...string) {
		for _, n := range names {
			if !slices.Contains(algos, n) {
				algos = append(algos, n)
			}
		}
	}
	for _, w := range keyErr.Want {
		if t := w.Key.Type(); t == ssh.KeyAlgoRSA {
			add(ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		} else {
			add(t)
		}
	}
	return algos
}

func (k *knownHosts) check(hostname string, remote net.Addr, key ssh.PublicKey) error {
	err := k.lookup(hostname, remote, key)
	var keyErr *knownhosts.KeyError
	if err == nil || !errors.As(err, &keyErr) {
		return err
	}
	if len(keyErr.Want) > 0 {
		w := keyErr.Want[0]
		return fmt.Errorf("호스트 키가 known_hosts에 저장된 키와 다릅니다. 서버를 다시 설치했거나 중간자 공격일 수 있습니다.\n"+
			"받은 키: %s %s\n저장된 키: %s %d번째 줄 (%s)\n"+
			"서버를 신뢰할 수 있으면 그 줄을 지우고 다시 접속하세요.",
			key.Type(), ssh.FingerprintSHA256(key), w.Filename, w.Line, w.Key.Type())
	}
	if !k.confirm(hostname, ssh.FingerprintSHA256(key)) {
		return errors.New("사용자가 호스트 키를 거부했습니다")
	}
	f, err := os.OpenFile(k.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key))
	return err
}
