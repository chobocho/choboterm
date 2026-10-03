package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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

func (s *sshSession) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *sshSession) Write(p []byte) (int, error) { return s.stdin.Write(p) }
func (s *sshSession) Resize(cols, rows int) error { return s.session.WindowChange(rows, cols) }
func (s *sshSession) Close() error {
	_ = s.session.Close()
	return s.client.Close()
}

// hostKeyConfirmer asks the user to trust an unknown host key.
type hostKeyConfirmer func(host, fingerprint string) bool

// dialSSH opens an SSH shell. conn is an already connected socket (from
// protocol detection) or nil to dial req.Host:req.Port.
func dialSSH(req ConnectRequest, confirm hostKeyConfirmer, conn net.Conn) (Session, error) {
	if req.Login == "" {
		return nil, errors.New("SSH 접속에는 Login이 필요합니다")
	}

	hostKeyCallback, err := knownHostsCallback(confirm)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            req.Login,
		Auth:            authMethods(req.Pass),
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	if conn == nil {
		if conn, err = net.DialTimeout("tcp", addr, cfg.Timeout); err != nil {
			return nil, fmt.Errorf("SSH 접속 실패: %w", err)
		}
	}
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

	return &sshSession{client: client, session: session, stdin: stdin, stdout: stdout}, nil
}

// authMethods tries unencrypted private keys in ~/.ssh first, then the password
// (both as plain password and keyboard-interactive).
func authMethods(pass string) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	if home, err := os.UserHomeDir(); err == nil {
		var signers []ssh.Signer
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			data, err := os.ReadFile(filepath.Join(home, ".ssh", name))
			if err != nil {
				continue
			}
			if signer, err := ssh.ParsePrivateKey(data); err == nil {
				signers = append(signers, signer)
			}
		}
		if len(signers) > 0 {
			methods = append(methods, ssh.PublicKeys(signers...))
		}
	}

	if pass != "" {
		methods = append(methods,
			ssh.Password(pass),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					if !echos[i] {
						answers[i] = pass
					}
				}
				return answers, nil
			}),
		)
	}
	return methods
}

// knownHostsCallback verifies host keys against ~/.ssh/known_hosts.
// Unknown hosts are confirmed by the user and appended; changed keys are rejected.
func knownHostsCallback(confirm hostKeyConfirmer) (ssh.HostKeyCallback, error) {
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

	check, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := check(hostname, remote, key)
		var keyErr *knownhosts.KeyError
		if err == nil || !errors.As(err, &keyErr) {
			return err
		}
		if len(keyErr.Want) > 0 {
			return fmt.Errorf("호스트 키가 known_hosts와 다릅니다 (중간자 공격 가능성): %s", path)
		}
		if !confirm(hostname, ssh.FingerprintSHA256(key)) {
			return errors.New("사용자가 호스트 키를 거부했습니다")
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = fmt.Fprintln(f, knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key))
		return err
	}, nil
}
