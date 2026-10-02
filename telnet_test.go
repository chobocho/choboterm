package main

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

func newPipeTelnet(login, pass string) (*telnetSession, net.Conn) {
	client, server := net.Pipe()
	return &telnetSession{
		conn:          client,
		cols:          80,
		rows:          24,
		login:         login,
		pass:          pass,
		sentLogin:     login == "",
		sentPass:      pass == "",
		loginDeadline: time.Now().Add(time.Minute),
	}, server
}

// readN reads exactly n bytes from the server side of the pipe.
func readN(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("read %d bytes: %v", n, err)
	}
	return buf
}

func TestTelnetNegotiation(t *testing.T) {
	ts, server := newPipeTelnet("", "")
	defer ts.Close()

	go func() {
		_, _ = server.Write([]byte{
			'h', 'i', tnIAC, tnIAC, // data "hi" + escaped 0xFF
			tnIAC, tnDO, optNAWS,
			tnIAC, tnDO, optTType,
			tnIAC, tnSB, optTType, ttypeSEND, tnIAC, tnSE,
			tnIAC, tnWILL, optEcho,
			tnIAC, tnDO, 99, // unknown option
			'!',
		})
	}()

	// Negotiation replies are written synchronously while parsing, so drain them concurrently.
	replies := make(chan []byte, 1)
	go func() {
		want := 3 + 9 + 3 + 4 + len(terminalType) + 2 + 3 + 3
		replies <- readN(t, server, want)
	}()

	got := make([]byte, 0, 8)
	buf := make([]byte, 64)
	for len(got) < 4 {
		n, err := ts.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, []byte{'h', 'i', 0xFF, '!'}) {
		t.Fatalf("data = %v", got)
	}

	want := []byte{tnIAC, tnWILL, optNAWS, tnIAC, tnSB, optNAWS, 0, 80, 0, 24, tnIAC, tnSE}
	want = append(want, tnIAC, tnWILL, optTType)
	want = append(want, tnIAC, tnSB, optTType, ttypeIS)
	want = append(want, terminalType...)
	want = append(want, tnIAC, tnSE)
	want = append(want, tnIAC, tnDO, optEcho)
	want = append(want, tnIAC, tnWONT, 99)
	if r := <-replies; !bytes.Equal(r, want) {
		t.Fatalf("replies =\n%v\nwant\n%v", r, want)
	}
}

func TestTelnetWriteEscaping(t *testing.T) {
	ts, server := newPipeTelnet("", "")
	defer ts.Close()

	go func() { _, _ = ts.Write([]byte{'a', 0xFF, '\r', 'b', '\r', '\n'}) }()
	got := readN(t, server, 8)
	want := []byte{'a', 0xFF, 0xFF, '\r', 0, 'b', '\r', '\n'}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestTelnetAutoLogin(t *testing.T) {
	ts, server := newPipeTelnet("guest", "secret")
	defer ts.Close()

	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := ts.Read(buf); err != nil {
				return
			}
		}
	}()

	_, _ = server.Write([]byte("Welcome\r\nlogin: "))
	if got := readN(t, server, 7); string(got) != "guest\r\x00" {
		t.Fatalf("login reply %q", got)
	}
	_, _ = server.Write([]byte("Password: "))
	if got := readN(t, server, 8); string(got) != "secret\r\x00" {
		t.Fatalf("password reply %q", got)
	}
}
