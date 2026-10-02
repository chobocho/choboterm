package main

import (
	"bytes"
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func testPayload(n int, seed int64) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

func noProgress(XferProgress) {}

// pipePeers connects two zmPeers back to back.
func pipePeers(ctx context.Context, escAll bool) (*zmPeer, *zmPeer) {
	ab := make(chan []byte, 4096)
	ba := make(chan []byte, 4096)
	send := func(ch chan []byte) func([]byte) error {
		return func(b []byte) error {
			ch <- append([]byte(nil), b...)
			return nil
		}
	}
	a := &zmPeer{ctx: ctx, in: ba, write: send(ab), escAll: escAll, timeout: 5 * time.Second}
	b := &zmPeer{ctx: ctx, in: ab, write: send(ba), escAll: escAll, timeout: 5 * time.Second}
	return a, b
}

func TestZmodemLoopback(t *testing.T) {
	for _, escAll := range []bool{false, true} {
		src := t.TempDir()
		dst := t.TempDir()
		files := map[string][]byte{
			"big.bin":   testPayload(300_000, 1), // several windows
			"empty.txt": {},
			"한글.txt":    []byte("안녕\r\n\x18\x11\x13\xff\x7f"),
		}
		var paths []string
		for name, data := range files {
			p := filepath.Join(src, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, p)
		}

		ctx, cancel := context.WithCancel(context.Background())
		sender, receiver := pipePeers(ctx, escAll)
		errc := make(chan error, 1)
		go func() { errc <- zmSend(sender, paths, zmOptions{Progress: noProgress}) }()

		saved, err := zmReceive(receiver, zmOptions{Dir: dst, Progress: noProgress})
		if err != nil {
			t.Fatalf("escAll=%v receive: %v", escAll, err)
		}
		if err := <-errc; err != nil {
			t.Fatalf("escAll=%v send: %v", escAll, err)
		}
		cancel()

		if len(saved) != len(files) {
			t.Fatalf("saved %v", saved)
		}
		for name, want := range files {
			got, err := os.ReadFile(filepath.Join(dst, name))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("escAll=%v %s: %d bytes, want %d", escAll, name, len(got), len(want))
			}
		}
	}
}

func TestZmodemDetection(t *testing.T) {
	i, recv := findZmodemStart([]byte("rz\r**\x18B00000000000000\r\x8a\x11"))
	if i != 3 || !recv {
		t.Fatalf("sz start: %d %v", i, recv)
	}
	i, recv = findZmodemStart([]byte("rz waiting to receive.**\x18B0100000023be50\r\x8a\x11"))
	if i != 22 || recv {
		t.Fatalf("rz start: %d %v", i, recv)
	}
	if n := partialSigSuffix([]byte("abc**\x18")); n != 3 {
		t.Fatalf("partial = %d", n)
	}
	if n := partialSigSuffix([]byte("abc")); n != 0 {
		t.Fatalf("partial = %d", n)
	}
}

func TestSanitizeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd",
		`C:\x\evil.txt`:    "evil.txt",
		"a:b?.txt":         "a_b_.txt",
		"..":               "zmodem.bin",
		"보고서.hwp":          "보고서.hwp",
	} {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// ---- interoperability with real lrzsz through WSL ----
//
//	CHOBOTERM_LRZSZ=/tmp/lrzsz/x/usr/bin go test -run ZmodemLrzsz -v
//
// (In Git Bash also set MSYS2_ENV_CONV_EXCL=CHOBOTERM_LRZSZ so the path isn't rewritten.)
// The directory must contain sz and rz (e.g. from `apt-get download lrzsz && dpkg -x`).

func wslPeer(t *testing.T, ctx context.Context, script string, escAll bool) (*zmPeer, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command("wsl.exe", "--", "sh", "-c", script)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	in := make(chan []byte, 1024)
	go func() {
		defer close(in)
		buf := make([]byte, 32*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				in <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	p := &zmPeer{ctx: ctx, in: in, escAll: escAll, timeout: 10 * time.Second,
		write: func(b []byte) error { _, err := stdin.Write(b); return err }}
	return p, cmd
}

func wslRun(t *testing.T, script string, stdin []byte) []byte {
	t.Helper()
	cmd := exec.Command("wsl.exe", "--", "sh", "-c", script)
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: %v", script, err)
	}
	return out
}

func TestZmodemLrzsz(t *testing.T) {
	bin := os.Getenv("CHOBOTERM_LRZSZ")
	if bin == "" {
		t.Skip("CHOBOTERM_LRZSZ not set")
	}
	big := testPayload(500_000, 7)
	small := []byte("hello\r\n\x18\x11\x13\xff")

	wslRun(t, "rm -rf /tmp/zt && mkdir -p /tmp/zt/out /tmp/zt/in", nil)
	wslRun(t, "cat > /tmp/zt/out/big.bin", big)
	wslRun(t, "cat > /tmp/zt/out/한글.txt", small)

	for _, escAll := range []bool{false, true} {
		// Remote sz -> us.
		ctx := context.Background()
		dst := t.TempDir()
		p, cmd := wslPeer(t, ctx, bin+"/sz -q /tmp/zt/out/big.bin /tmp/zt/out/한글.txt", escAll)
		saved, err := zmReceive(p, zmOptions{Dir: dst, Progress: noProgress})
		if err != nil {
			t.Fatalf("escAll=%v receive from sz: %v", escAll, err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("sz exit: %v", err)
		}
		if len(saved) != 2 {
			t.Fatalf("saved %v", saved)
		}
		for name, want := range map[string][]byte{"big.bin": big, "한글.txt": small} {
			got, err := os.ReadFile(filepath.Join(dst, name))
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("escAll=%v from sz %s: %d bytes, err %v", escAll, name, len(got), err)
			}
		}

		// Us -> remote rz.
		src := t.TempDir()
		up1 := filepath.Join(src, "up.bin")
		up2 := filepath.Join(src, "올림.txt")
		os.WriteFile(up1, big, 0o644)
		os.WriteFile(up2, small, 0o644)
		wslRun(t, "rm -f /tmp/zt/in/*", nil)
		p, cmd = wslPeer(t, ctx, "cd /tmp/zt/in && "+bin+"/rz -q", escAll)
		if err := zmSend(p, []string{up1, up2}, zmOptions{Progress: noProgress}); err != nil {
			t.Fatalf("escAll=%v send to rz: %v", escAll, err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("rz exit: %v", err)
		}
		if got := wslRun(t, "cat /tmp/zt/in/up.bin", nil); !bytes.Equal(got, big) {
			t.Fatalf("escAll=%v rz up.bin: %d bytes", escAll, len(got))
		}
		if got := wslRun(t, "cat /tmp/zt/in/올림.txt", nil); !bytes.Equal(got, small) {
			t.Fatalf("escAll=%v rz 올림.txt: %q", escAll, got)
		}
	}
}
