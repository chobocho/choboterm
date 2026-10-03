package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// scpFS is the fallback for SSH servers without the SFTP subsystem
// (e.g. Dropbear on routers or embedded boards). Listing runs `ls` over an SSH
// exec channel; files move with the classic scp protocol (`scp -f` / `scp -t`).
type scpFS struct {
	client *ssh.Client
	names  *codec
}

func newSCP(client *ssh.Client, names *codec) (*scpFS, error) {
	s := &scpFS{client: client, names: names}
	if _, err := s.run("command -v scp"); err != nil {
		return nil, errors.New("서버에 scp가 없습니다")
	}
	return s, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (s *scpFS) q(p string) string { return shellQuote(s.names.EncodeString(p)) }

// run executes a command and returns stdout. If the command fails but printed
// something (e.g. ls with one unreadable entry), the output is still returned.
func (s *scpFS) run(cmd string) ([]byte, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	var out, stderr bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &stderr
	if err := sess.Run(cmd); err != nil {
		if out.Len() > 0 {
			return out.Bytes(), nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(s.names.DecodeString(msg))
		}
		return nil, err
	}
	return out.Bytes(), nil
}

func (s *scpFS) Getwd() (string, error) {
	out, err := s.run("pwd")
	if err != nil {
		return "/", nil
	}
	return s.names.DecodeString(strings.TrimSpace(string(out))), nil
}

func (s *scpFS) List(dir string) ([]FileEntry, error) {
	// -L follows symlinks so linked folders show up as folders.
	out, err := s.run("LC_ALL=C ls -laL " + s.q(dir))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var list []FileEntry
	for _, line := range strings.Split(string(out), "\n") {
		e, ok := parseLsLine(strings.TrimRight(line, "\r"), now)
		if !ok || e.Name == "." || e.Name == ".." {
			continue
		}
		e.Name = s.names.DecodeString(e.Name)
		list = append(list, e)
	}
	return list, nil
}

// "drwxr-xr-x 2 user group 4096 Oct  3 01:02 name" (GNU and BusyBox ls -l).
var lsLine = regexp.MustCompile(`^([-dlbcps])\S*\s+\d+\s+\S+\s+\S+\s+(\d+)\s+([A-Z][a-z]{2})\s+(\d{1,2})\s+(\d{1,2}:\d{2}|\d{4})\s(.+)$`)

func parseLsLine(line string, now time.Time) (FileEntry, bool) {
	m := lsLine.FindStringSubmatch(line)
	if m == nil {
		return FileEntry{}, false
	}
	size, _ := strconv.ParseInt(m[2], 10, 64)
	e := FileEntry{Name: m[6], Size: size, IsDir: m[1] == "d"}

	day, _ := strconv.Atoi(m[4])
	if strings.Contains(m[5], ":") {
		// Recent files show the time instead of the year.
		if t, err := time.ParseInLocation("Jan 2 15:04 2006", fmt.Sprintf("%s %d %s %d", m[3], day, m[5], now.Year()), time.Local); err == nil {
			if t.After(now.Add(24 * time.Hour)) {
				t = t.AddDate(-1, 0, 0)
			}
			e.ModTime = t.Format("2006-01-02 15:04")
		}
	} else if t, err := time.Parse("Jan 2 2006", fmt.Sprintf("%s %d %s", m[3], day, m[5])); err == nil {
		e.ModTime = t.Format("2006-01-02")
	}
	return e, true
}

// readSCPAck reads one scp status: 0 = ok, 1 = warning, 2 = fatal (followed by a message line).
func (s *scpFS) readSCPAck(r *bufio.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	if b == 0 {
		return nil
	}
	msg, _ := r.ReadString('\n')
	msg = strings.TrimSpace(s.names.DecodeString(msg))
	if msg == "" {
		msg = fmt.Sprintf("scp 오류 (%d)", b)
	}
	return errors.New(msg)
}

func (s *scpFS) Download(remotePath string, w io.Writer) error {
	sess, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	if err := sess.Start("scp -f " + s.q(remotePath)); err != nil {
		return err
	}
	r := bufio.NewReader(stdout)

	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}
	header, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	switch {
	case header[0] == 1 || header[0] == 2:
		return errors.New(strings.TrimSpace(s.names.DecodeString(header[1:])))
	case header[0] == 'D':
		return errors.New("폴더는 받을 수 없습니다")
	case header[0] != 'C':
		return fmt.Errorf("알 수 없는 scp 응답: %q", strings.TrimSpace(header))
	}
	// "C0644 <size> <name>\n"
	fields := strings.SplitN(strings.TrimSpace(header[1:]), " ", 3)
	if len(fields) < 2 {
		return fmt.Errorf("알 수 없는 scp 응답: %q", strings.TrimSpace(header))
	}
	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return err
	}

	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}
	if _, err := io.CopyN(w, r, size); err != nil {
		return err
	}
	if err := s.readSCPAck(r); err != nil {
		return err
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}
	stdin.Close()
	return sess.Wait()
}

func (s *scpFS) Upload(remotePath string, r io.Reader, size int64) error {
	sess, err := s.client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	if err := sess.Start("scp -t " + s.q(path.Dir(remotePath))); err != nil {
		return err
	}
	ack := bufio.NewReader(stdout)

	if err := s.readSCPAck(ack); err != nil {
		return err
	}
	name := s.names.EncodeString(path.Base(remotePath))
	if _, err := fmt.Fprintf(stdin, "C0644 %d %s\n", size, name); err != nil {
		return err
	}
	if err := s.readSCPAck(ack); err != nil {
		return err
	}
	if _, err := io.CopyN(stdin, r, size); err != nil {
		return err // closing the session (deferred) aborts the remote scp
	}
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}
	if err := s.readSCPAck(ack); err != nil {
		return err
	}
	stdin.Close()
	return sess.Wait()
}

func (s *scpFS) Remove(p string, isDir bool) error {
	cmd := "rm -f -- "
	if isDir {
		cmd = "rm -rf -- "
	}
	_, err := s.run(cmd + s.q(p))
	return err
}

func (s *scpFS) Rename(from, to string) error {
	_, err := s.run("mv -- " + s.q(from) + " " + s.q(to))
	return err
}

func (s *scpFS) Mkdir(p string) error {
	_, err := s.run("mkdir -- " + s.q(p))
	return err
}

// Close does nothing: the SSH connection belongs to the terminal session.
func (s *scpFS) Close() error { return nil }
