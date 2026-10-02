package main

import (
	"bytes"
	"net"
	"os"
	"strconv"
	"testing"
)

// TestFTPRoundTrip runs against a real FTP server whose root contains
// "한글파일.txt" ("hello") and an empty "sub" folder, e.g.:
//
//	CHOBOTERM_FTP_TEST=127.0.0.1:2121 CHOBOTERM_FTP_ENCODING=EUC-KR go test -run FTP
//
// Login is user / pass. Skipped when the variable isn't set.
func TestFTPRoundTrip(t *testing.T) {
	addr := os.Getenv("CHOBOTERM_FTP_TEST")
	if addr == "" {
		t.Skip("CHOBOTERM_FTP_TEST not set")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)

	fs, err := dialFTP(ConnectRequest{Host: host, Port: port, Login: "user", Pass: "pass", Encoding: os.Getenv("CHOBOTERM_FTP_ENCODING")})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	wd, err := fs.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	list, err := fs.List(wd)
	if err != nil {
		t.Fatal(err)
	}
	sortEntries(list)
	if len(list) < 2 || list[0].Name != "sub" || !list[0].IsDir {
		t.Fatalf("list = %+v", list)
	}
	found := false
	for _, e := range list {
		found = found || (e.Name == "한글파일.txt" && e.Size == 5)
	}
	if !found {
		t.Fatalf("한글파일.txt missing: %+v", list)
	}

	var buf bytes.Buffer
	if err := fs.Download(wd+"/한글파일.txt", &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello" {
		t.Fatalf("download = %q", buf.String())
	}

	payload := bytes.Repeat([]byte("abc"), 300_000)
	if err := fs.Upload(wd+"/sub/올림.bin", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	sub, err := fs.List(wd + "/sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 1 || sub[0].Name != "올림.bin" || sub[0].Size != int64(len(payload)) {
		t.Fatalf("sub = %+v", sub)
	}
	buf.Reset()
	if err := fs.Download(wd+"/sub/올림.bin", &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("round trip mismatch: %d bytes", buf.Len())
	}
}
