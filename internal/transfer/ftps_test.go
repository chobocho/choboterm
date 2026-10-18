package transfer

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"choboterm/internal/terminal"
)

// selfSigned makes a certificate for 127.0.0.1, like most FTP servers have.
func selfSigned(t *testing.T, name string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,

		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// fakeFTP is an FTP server with one file, "/hello.txt", that takes uploads.
type fakeFTP struct {
	cert     tls.Certificate
	noAuth   bool // refuses AUTH TLS, like a server without FTPS
	brokenTL bool // accepts AUTH TLS but then doesn't speak TLS
	implicit bool // TLS before the greeting (port 990 style)

	mu       sync.Mutex
	files    map[string][]byte
	commands []string // as seen, with "(tls)" after the ones received over TLS
	port     int
}

func (s *fakeFTP) start(t *testing.T) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	s.port = l.Addr().(*net.TCPAddr).Port
	if s.files == nil {
		s.files = map[string][]byte{"/hello.txt": []byte("hello")}
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
}

func (s *fakeFTP) seen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.commands, " ")
}

func (s *fakeFTP) serve(c net.Conn) {
	defer c.Close()
	cfg := &tls.Config{Certificates: []tls.Certificate{s.cert}}
	secure, prot := false, false
	if s.implicit {
		c = tls.Server(c, cfg)
		secure = true
	}
	r := bufio.NewReader(c)
	reply := func(format string, a ...interface{}) { fmt.Fprintf(c, format+"\r\n", a...) }
	var (
		data net.Listener
		user string
	)
	// Waits for the client on the data port, with TLS after PROT P.
	accept := func() (net.Conn, error) {
		if data == nil {
			return nil, errors.New("no EPSV")
		}
		defer func() { data.Close(); data = nil }()
		d, err := data.Accept()
		if err != nil {
			return nil, err
		}
		if prot {
			d = tls.Server(d, cfg)
		}
		return d, nil
	}
	reply("220 fake")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd, arg, _ := strings.Cut(line, " ")
		cmd = strings.ToUpper(cmd)
		s.mu.Lock()
		if secure {
			s.commands = append(s.commands, cmd+"(tls)")
		} else {
			s.commands = append(s.commands, cmd)
		}
		s.mu.Unlock()
		switch cmd {
		case "AUTH":
			if s.noAuth {
				reply("500 AUTH not understood")
				continue
			}
			reply("234 AUTH TLS ok")
			if s.brokenTL {
				reply("this is not TLS")
				return
			}
			c = tls.Server(c, cfg)
			r = bufio.NewReader(c)
			secure = true
		case "PBSZ":
			reply("200 PBSZ=0")
		case "PROT":
			prot = arg == "P"
			reply("200 PROT ok")
		case "USER":
			user = arg
			reply("331 password")
		case "PASS":
			if user == "nobody" {
				reply("530 login incorrect")
				continue
			}
			reply("230 logged in")
		case "FEAT":
			reply("211-Features:\r\n MLST type*;size*;\r\n UTF8\r\n211 End")
		case "TYPE", "OPTS", "NOOP":
			reply("200 ok")
		case "PWD":
			reply(`257 "/"`)
		case "EPSV":
			if data, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
				reply("425 no data port")
				continue
			}
			reply("229 Entering Extended Passive Mode (|||%d|)", data.Addr().(*net.TCPAddr).Port)
		case "MLSD", "RETR", "STOR":
			reply("150 data")
			d, err := accept()
			if err != nil {
				reply("425 %v", err)
				continue
			}
			s.mu.Lock()
			var derr error
			switch cmd {
			case "MLSD":
				for name, body := range s.files {
					fmt.Fprintf(d, "type=file;size=%d; %s\r\n", len(body), strings.TrimPrefix(name, "/"))
				}
			case "RETR":
				_, derr = d.Write(s.files[arg])
			}
			s.mu.Unlock()
			if cmd == "STOR" {
				var body []byte
				body, derr = io.ReadAll(d)
				s.mu.Lock()
				s.files[arg] = body
				s.mu.Unlock()
			}
			if tc, ok := d.(*tls.Conn); ok && derr == nil {
				// Wait for the client's close_notify, as servers doing a
				// TLS shutdown do.
				_ = tc.CloseWrite()
				_, derr = io.Copy(io.Discard, tc)
			}
			d.Close()
			if derr != nil {
				// Like pyftpdlib on Windows: a data connection that was
				// reset gets no reply at all.
				continue
			}
			reply("226 done")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 no")
		}
	}
}

func (s *fakeFTP) req() terminal.ConnectRequest {
	return terminal.ConnectRequest{Host: "127.0.0.1", Port: s.port, Login: "user", Pass: "secret"}
}

// within fails the test if f doesn't return in time: a server that never
// answers would otherwise hang the whole test run.
func within(t *testing.T, f func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(ftpsDrain + 5*time.Second):
		t.Fatal("the server never answered")
	}
}

// roundTrip lists, downloads and uploads through fs.
func roundTrip(t *testing.T, s *fakeFTP, fs *ftpFS) {
	t.Helper()
	within(t, func() error { return roundTripErr(s, fs) })
}

func roundTripErr(s *fakeFTP, fs *ftpFS) error {
	list, err := fs.List("/")
	if err != nil || len(list) != 1 || list[0].Name != "hello.txt" {
		return fmt.Errorf("list: %+v %v", list, err)
	}
	var buf bytes.Buffer
	if err := fs.Download("/hello.txt", &buf); err != nil || buf.String() != "hello" {
		return fmt.Errorf("download: %q %v", buf.String(), err)
	}
	if err := fs.Upload("/up.txt", strings.NewReader("uploaded"), 8); err != nil {
		return err
	}
	s.mu.Lock()
	got := string(s.files["/up.txt"])
	delete(s.files, "/up.txt") // the next round trip lists one file again
	s.mu.Unlock()
	if got != "uploaded" {
		return fmt.Errorf("uploaded %q", got)
	}
	return nil
}

func TestFTPSExplicitSelfSigned(t *testing.T) {
	s := &fakeFTP{cert: selfSigned(t, "fake ftp")}
	s.start(t)
	asked := 0
	trust := func(cert *x509.Certificate, why error) bool {
		asked++
		if CertName(cert) != "fake ftp" || !strings.Contains(CertProblem(why), "자체 서명") {
			t.Errorf("asked about %q: %v (%s)", CertName(cert), why, CertProblem(why))
		}
		return true
	}
	fs, err := DialFTP(s.req(), nil, trust)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if !fs.Secure() {
		t.Fatal("not FTPS")
	}
	roundTrip(t, s, fs)
	// Asked once: data connections with the same certificate are not asked about.
	if asked != 1 {
		t.Fatalf("asked %d times", asked)
	}
	// The password went over TLS, and data connections were protected.
	if seen := s.seen(); !strings.HasPrefix(seen, "AUTH USER(tls) PASS(tls)") || !strings.Contains(seen, "PROT(tls)") {
		t.Fatalf("commands: %s", seen)
	}
}

func TestFTPSDownloadStoppedEarly(t *testing.T) {
	// The viewer reads only the start of big files. The rest must not be left
	// unread on a closed connection: the reset makes some servers never
	// answer, and the next command would wait forever.
	big := bytes.Repeat([]byte("0123456789"), 400_000)
	s := &fakeFTP{cert: selfSigned(t, "fake ftp"), files: map[string][]byte{"/big.bin": big}}
	s.start(t)
	fs, err := DialFTP(s.req(), nil, func(*x509.Certificate, error) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	within(t, func() error {
		lw := &LimitWriter{Max: 1000}
		if err := fs.Download("/big.bin", lw); !lw.Full || !bytes.Equal(lw.Buf, big[:1000]) {
			return fmt.Errorf("early stop: full=%v len=%d, %v", lw.Full, len(lw.Buf), err)
		}
		_, err := fs.List("/")
		return err
	})
}

func TestFTPSRefusedCertificateSendsNothing(t *testing.T) {
	s := &fakeFTP{cert: selfSigned(t, "fake ftp")}
	s.start(t)
	_, err := DialFTP(s.req(), nil, func(*x509.Certificate, error) bool { return false })
	if !errors.Is(err, ErrCertRefused) {
		t.Fatalf("err = %v", err)
	}
	// No plain FTP afterwards: the user said no, so the password is never sent.
	time.Sleep(100 * time.Millisecond)
	if seen := s.seen(); strings.Contains(seen, "USER") || strings.Contains(seen, "PASS") {
		t.Fatalf("commands: %s", seen)
	}
}

func TestFTPSTrustedBySystemIsNotAsked(t *testing.T) {
	s := &fakeFTP{cert: selfSigned(t, "fake ftp")}
	s.start(t)
	ftpsRoots = x509.NewCertPool()
	ftpsRoots.AddCert(s.cert.Leaf)
	defer func() { ftpsRoots = nil }()
	fs, err := DialFTP(s.req(), nil, func(*x509.Certificate, error) bool {
		t.Error("asked about a trusted certificate")
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	roundTrip(t, s, fs)
}

func TestFTPSFallsBackWithoutTLS(t *testing.T) {
	for _, s := range []*fakeFTP{{noAuth: true}, {brokenTL: true}} {
		s.cert = selfSigned(t, "fake ftp")
		s.start(t)
		fs, err := DialFTP(s.req(), nil, func(*x509.Certificate, error) bool {
			t.Error("asked without a certificate")
			return false
		})
		if err != nil {
			t.Fatalf("noAuth %v brokenTLS %v: %v", s.noAuth, s.brokenTL, err)
		}
		if fs.Secure() {
			t.Fatal("FTPS without TLS")
		}
		roundTrip(t, s, fs)
		fs.Close()
		// AUTH TLS was tried first; the second connection is plain FTP from the start.
		if seen := s.seen(); !strings.HasPrefix(seen, "AUTH ") || !strings.Contains(seen, "USER PASS") ||
			strings.Contains(seen, "PROT") || strings.Contains(seen, "(tls)") {
			t.Fatalf("commands: %s", seen)
		}
	}
}

func TestFTPSImplicit(t *testing.T) {
	s := &fakeFTP{cert: selfSigned(t, "fake ftp"), implicit: true}
	s.start(t)
	fs, err := dialFTP(s.req(), nil, ftpImplicit, func(*x509.Certificate, error) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if !fs.Secure() {
		t.Fatal("not FTPS")
	}
	roundTrip(t, s, fs)
	if seen := s.seen(); strings.Contains(seen, "AUTH") || !strings.HasPrefix(seen, "USER(tls)") {
		t.Fatalf("commands: %s", seen)
	}
	// Refusing the certificate of an implicit server doesn't fall back either.
	_, err = dialFTP(s.req(), nil, ftpImplicit, func(*x509.Certificate, error) bool { return false })
	if !errors.Is(err, ErrCertRefused) {
		t.Fatalf("err = %v", err)
	}
}

func TestFTPSLoginErrorIsNotFallback(t *testing.T) {
	// A wrong password over TLS must not be tried again in the clear.
	s := &fakeFTP{cert: selfSigned(t, "fake ftp")}
	s.start(t)
	req := s.req()
	req.Login = "nobody"
	_, err := DialFTP(req, nil, func(*x509.Certificate, error) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "FTP 로그인 실패") {
		t.Fatalf("err = %v", err)
	}
	if n := strings.Count(s.seen(), "USER"); n != 1 {
		t.Fatalf("USER sent %d times: %s", n, s.seen())
	}
}
