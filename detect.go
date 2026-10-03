package main

import (
	"bytes"
	"io"
	"net"
	"regexp"
	"time"
)

// detectWait is how long to wait for a server greeting on a non-standard port.
// Telnet servers that stay silent until the client speaks cost this much delay.
var detectWait = 2 * time.Second

var ftpGreeting = regexp.MustCompile(`^220[ -]`)

// prefixConn replays the bytes read during protocol detection before
// continuing with the live connection.
type prefixConn struct {
	net.Conn
	r io.Reader
}

func (c *prefixConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// dialDetect connects to addr and guesses the protocol from the server's
// first bytes: SSH servers start with "SSH-", FTP servers with "220 ",
// anything else (IAC negotiation, a login banner, or silence) is Telnet.
func dialDetect(addr string) (net.Conn, string, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, "", err
	}

	var head []byte
	buf := make([]byte, 512)
	deadline := time.Now().Add(detectWait)
	_ = conn.SetReadDeadline(deadline)
	// Read until the greeting is long enough to decide (it may arrive in pieces).
	for len(head) < 4 && !bytes.ContainsAny(head, "\r\n") {
		n, err := conn.Read(buf)
		head = append(head, buf[:n]...)
		if err != nil {
			break
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	proto := "telnet"
	switch {
	case bytes.HasPrefix(head, []byte("SSH-")):
		proto = "ssh"
	case ftpGreeting.Match(head):
		proto = "ftp"
	}
	return &prefixConn{Conn: conn, r: io.MultiReader(bytes.NewReader(head), conn)}, proto, nil
}
