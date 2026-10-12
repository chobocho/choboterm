package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"choboterm/internal/testutil"
)

func TestSFTPRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello 한글"), 0o644); err != nil {
		t.Fatal(err)
	}

	client, err := ssh.Dial("tcp", testutil.StartSFTPServer(t, root), &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	fs, err := NewSFTP(client)
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
	SortEntries(list)
	if len(list) != 2 || list[0].Name != "sub" || !list[0].IsDir || list[1].Name != "hello.txt" || list[1].Size != int64(len("hello 한글")) {
		t.Fatalf("list = %+v", list)
	}

	var buf bytes.Buffer
	if err := fs.Download(wd+"/hello.txt", &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello 한글" {
		t.Fatalf("download = %q", buf.String())
	}

	payload := bytes.Repeat([]byte("0123456789"), 100_000) // 1MB, several SFTP packets
	if err := fs.Upload(wd+"/sub/up.bin", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "sub", "up.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("uploaded %d bytes, want %d", len(got), len(payload))
	}
}
