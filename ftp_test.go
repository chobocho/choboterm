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

	fs, err := dialFTP(ConnectRequest{Host: host, Port: port, Login: "user", Pass: "pass", Encoding: os.Getenv("CHOBOTERM_FTP_ENCODING")}, nil)
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

	// Stopping a download early (the text viewer's size limit) must leave the
	// connection usable.
	lw := &limitWriter{max: 1000}
	if err := fs.Download(wd+"/sub/올림.bin", lw); !lw.full || !bytes.Equal(lw.buf, payload[:1000]) {
		t.Fatalf("early stop: full=%v len=%d, %v", lw.full, len(lw.buf), err)
	}
	if _, err := fs.List(wd); err != nil {
		t.Fatalf("list after early stop: %v", err)
	}

	// Resuming: the rest of a download, and the rest of an upload.
	buf.Reset()
	if err := fs.DownloadFrom(wd+"/sub/올림.bin", 500_000, &buf); err != nil || !bytes.Equal(buf.Bytes(), payload[500_000:]) {
		t.Fatalf("download from offset: %d bytes, %v", buf.Len(), err)
	}
	if err := fs.Upload(wd+"/sub/올림.bin", bytes.NewReader(payload[:400_000]), 400_000); err != nil {
		t.Fatal(err)
	}
	if err := fs.UploadFrom(wd+"/sub/올림.bin", bytes.NewReader(payload[400_000:]), 400_000); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := fs.Download(wd+"/sub/올림.bin", &buf); err != nil || !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("after resumed upload: %d bytes, %v", buf.Len(), err)
	}
	if err := fs.Remove(wd+"/sub/올림.bin", false); err != nil {
		t.Fatal(err)
	}
}
