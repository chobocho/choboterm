// Package sshconn connects SSH sessions: authentication (keys, the agent,
// prompts), known hosts, jump hosts and ~/.ssh/config.
package sshconn

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
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"choboterm/internal/dbg"
	"choboterm/internal/terminal"
)

type Session struct {
	Client  *ssh.Client
	Jumps   []*ssh.Client // ProxyJump hosts the connection goes through, first to last
	Session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func (s *Session) Write(p []byte) (int, error) { return s.stdin.Write(p) }

// Read returns errConnLost when the output ends because the connection broke:
// the output also ends when the shell exits, but then the server reports an
// exit status first.
func (s *Session) Read(p []byte) (int, error) {
	n, err := s.stdout.Read(p)
	if err == io.EOF {
		done := make(chan error, 1)
		go func() { done <- s.Session.Wait() }()
		select {
		case werr := <-done:
			dbg.Printf("ssh output EOF; session.Wait: %v", werr)
			var missing *ssh.ExitMissingError
			if errors.As(werr, &missing) {
				err = terminal.ErrLost
			}
		case <-time.After(5 * time.Second):
			dbg.Printf("ssh output EOF; session.Wait timed out")
		}
	}
	return n, err
}

// KeepAlive asks the server for a reply, as OpenSSH's ServerAliveInterval does.
func (s *Session) KeepAlive() error {
	done := make(chan error, 1)
	go func() {
		// Servers answer even requests they don't know (with a failure), which is enough.
		_, _, err := s.Client.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(terminal.KeepAliveTimeout):
		return terminal.ErrNoReply
	}
}
func (s *Session) Resize(cols, rows int) error { return s.Session.WindowChange(rows, cols) }
func (s *Session) Close() error {
	_ = s.Session.Close()
	err := s.Client.Close()
	CloseClients(s.Jumps)
	return err
}

// CloseClients closes SSH connections, the last (innermost) one first.
func CloseClients(cs []*ssh.Client) {
	for i := len(cs) - 1; i >= 0; i-- {
		_ = cs[i].Close()
	}
}

// hostKeyConfirmer asks the user to trust an unknown host key.
type hostKeyConfirmer func(host, fingerprint string) bool

// sshTimeout bounds connecting and logging in to one SSH server.
const sshTimeout = 10 * time.Second

// sshLogin connects to addr and logs in, checking the host key against
// known_hosts. conn is an already connected socket (protocol detection, or a
// channel through a jump host) or nil to dial addr. keys are private key
// files (IdentityFile in ~/.ssh/config) to try before the default ones.
// ask prompts the user during login (key passphrases); nil cancels prompts.
func sshLogin(addr, login, pass string, confirm hostKeyConfirmer, ask terminal.Asker, conn net.Conn, keys []string) (*ssh.Client, error) {
	if login == "" {
		return nil, errors.New("SSH 접속에는 Login이 필요합니다")
	}
	kh, err := loadKnownHosts(confirm)
	if err != nil {
		return nil, err
	}

	auth := newSSHAuth(pass, keys, ask)
	defer auth.Close()
	cfg := &ssh.ClientConfig{
		User:            login,
		Auth:            auth.methods,
		HostKeyCallback: kh.check,
		Timeout:         sshTimeout,
	}
	if conn == nil {
		if conn, err = net.DialTimeout("tcp", addr, cfg.Timeout); err != nil {
			return nil, fmt.Errorf("SSH 접속 실패: %w", err)
		}
	}
	// Ask for the key type we already trust, as OpenSSH does. Otherwise the
	// server may offer e.g. its ECDSA key while known_hosts holds its Ed25519
	// key, which would look like a changed key.
	cfg.HostKeyAlgorithms = kh.algorithmsFor(addr, conn.RemoteAddr())
	host, port := splitAddr(addr)
	legacy := LegacyAllowed != nil && LegacyAllowed(host, port)
	if legacy {
		cfg.Config = legacyConfig()
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		if le := legacyNeeded(err, host, port); le != nil && !legacy {
			return nil, le
		}
		return nil, fmt.Errorf("SSH 접속 실패: %w", err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// LegacyAllowed reports whether old algorithms with known weaknesses
// (diffie-hellman-group1-sha1, CBC ciphers and the like) may be used with
// host:port, as OpenSSH's "KexAlgorithms +..." would. The app sets it from
// the server's prefs; nil allows them for no server.
var LegacyAllowed func(host string, port int) bool

// LegacyError is a server that only speaks such old algorithms: old
// switches, routers and embedded boards. Connecting again with
// LegacyAllowed true for Host:Port can work.
type LegacyError struct {
	Host  string
	Port  int
	What  string   // what couldn't be agreed on: "key exchange", "client to server cipher", ...
	Algos []string // the old algorithms the server offered for it
	err   error
}

func (e *LegacyError) Error() string {
	return fmt.Sprintf("SSH 접속 실패: %s 서버는 안전하지 않은 옛 알고리즘만 지원합니다 (%s)",
		net.JoinHostPort(e.Host, strconv.Itoa(e.Port)), strings.Join(e.Algos, ", "))
}

func (e *LegacyError) Unwrap() error { return e.err }

// legacyConfig offers the old algorithms after the usual ones, so a server
// that also has a good one still gets that.
func legacyConfig() ssh.Config {
	ok, old := ssh.SupportedAlgorithms(), ssh.InsecureAlgorithms()
	return ssh.Config{
		KeyExchanges: append(ok.KeyExchanges, old.KeyExchanges...),
		Ciphers:      append(ok.Ciphers, old.Ciphers...),
		MACs:         append(ok.MACs, old.MACs...),
	}
}

// legacyNeeded returns a LegacyError if err is a failed negotiation that the
// old algorithms would get past.
func legacyNeeded(err error, host string, port int) *LegacyError {
	var ne *ssh.AlgorithmNegotiationError
	if !errors.As(err, &ne) {
		return nil
	}
	old := ssh.InsecureAlgorithms()
	var known []string
	switch {
	case ne.What == "key exchange":
		known = old.KeyExchanges
	case strings.HasSuffix(ne.What, "cipher"):
		known = old.Ciphers
	case strings.HasSuffix(ne.What, "MAC"):
		known = old.MACs
	}
	var algos []string
	for _, a := range ne.RequestedAlgorithms {
		if slices.Contains(known, a) {
			algos = append(algos, a)
		}
	}
	if len(algos) == 0 {
		return nil
	}
	return &LegacyError{Host: host, Port: port, What: ne.What, Algos: algos, err: err}
}

// splitAddr splits "host:port"; the port is 0 if there is none.
func splitAddr(addr string) (string, int) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0
	}
	port, _ := strconv.Atoi(p)
	return host, port
}

// Dial opens an SSH shell (see sshLogin for conn, keys and ask).
func Dial(req terminal.ConnectRequest, confirm hostKeyConfirmer, ask terminal.Asker, conn net.Conn, keys []string) (terminal.Session, error) {
	addr := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	client, err := sshLogin(addr, req.Login, req.Pass, confirm, ask, conn, keys)
	if err != nil {
		return nil, err
	}

	sess, err := OpenPty(client, req.Cols, req.Rows, "")
	if err != nil {
		client.Close()
		return nil, err
	}
	go func() { dbg.Printf("ssh connection to %s ended: %v", req.Host, client.Wait()) }()
	return sess, nil
}

// OpenPty starts a session with a terminal on client: the login shell, or
// command when it isn't "". Closing it on failure is left to the caller.
func OpenPty(client *ssh.Client, cols, rows int, command string) (*Session, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("PTY 요청 실패: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	if command == "" {
		err = session.Shell()
	} else {
		err = session.Start(command)
	}
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("셸 시작 실패: %w", err)
	}
	return &Session{Client: client, Session: session, stdin: stdin, stdout: stdout}, nil
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
