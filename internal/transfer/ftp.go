package transfer

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"

	"choboterm/internal/codec"
	"choboterm/internal/terminal"
)

// ftpFS is an FTP or FTPS connection. File names are converted with the
// selected encoding, so EUC-KR servers show Korean names correctly.
type ftpFS struct {
	mu     sync.Mutex // one command at a time on the control connection
	c      *ftp.ServerConn
	names  *codec.Codec
	stop   chan struct{}
	secure bool     // FTPS: the control and data connections use TLS
	ctrl   net.Conn // the control connection's socket, to unblock Close
}

// FTPSPort is the port of implicit FTPS, where TLS starts before the greeting.
const FTPSPort = 990

// CertTrust decides about an FTPS server certificate that the system does
// not trust (self-signed, expired, made for another name); why says what is
// wrong with it. It returns true to go on with the certificate.
type CertTrust func(cert *x509.Certificate, why error) bool

// ErrCertRefused is returned when the user did not trust the certificate.
var ErrCertRefused = errors.New("인증서를 신뢰하지 않아 접속하지 않았습니다")

// errNoTLS means the server can't do FTPS (AUTH TLS refused, or a TLS
// handshake that fails before a certificate is seen), so plain FTP is tried.
var errNoTLS = errors.New("no TLS")

// ftpsRoots replaces the system's certificate authorities in tests.
var ftpsRoots *x509.CertPool

type ftpMode int

const (
	ftpPlain    ftpMode = iota
	ftpExplicit         // AUTH TLS after the greeting (FTPES, usually port 21)
	ftpImplicit         // TLS from the start (port 990)
)

// DialFTP logs in to an FTP server. conn is an already connected control
// socket (from protocol detection) or nil to dial req.Host:req.Port.
//
// It asks for TLS first (AUTH TLS) and falls back to plain FTP only when the
// server can't do it, so passwords aren't sent in the clear to servers that
// support FTPS. Port 990 is implicit FTPS and never falls back.
func DialFTP(req terminal.ConnectRequest, conn net.Conn, trust CertTrust) (*ftpFS, error) {
	if req.Port == FTPSPort {
		return dialFTP(req, conn, ftpImplicit, trust)
	}
	f, err := dialFTP(req, conn, ftpExplicit, trust)
	if errors.Is(err, errNoTLS) {
		// conn was used up by the attempt; plain FTP dials again.
		return dialFTP(req, nil, ftpPlain, nil)
	}
	return f, err
}

// tlsState is what the certificate check of one FTP session found.
type tlsState struct {
	mu       sync.Mutex
	accepted *x509.Certificate // the server's certificate, once checked
	refused  bool              // the user did not trust it
}

func (st *tlsState) seen() (accepted, refused bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.accepted != nil, st.refused
}

// ftpsConfig checks the certificate itself instead of failing the handshake,
// so that a certificate the system doesn't trust can be shown to the user.
func ftpsConfig(host string, trust CertTrust, st *tlsState) *tls.Config {
	return &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, // checked in VerifyConnection below
		// Old FTP servers only speak TLS 1.0; that still beats the plain FTP
		// we'd fall back to.
		MinVersion: tls.VersionTLS10,
		// Many servers (vsftpd by default) refuse data connections that don't
		// resume the control connection's TLS session.
		ClientSessionCache: tls.NewLRUClientSessionCache(4),
		VerifyConnection: func(cs tls.ConnectionState) error {
			st.mu.Lock()
			defer st.mu.Unlock()
			if len(cs.PeerCertificates) == 0 {
				return errors.New("the server sent no certificate")
			}
			leaf := cs.PeerCertificates[0]
			if st.accepted != nil && leaf.Equal(st.accepted) {
				return nil // a data connection: checked (and maybe asked) already
			}
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			_, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: inter, Roots: ftpsRoots})
			if err != nil && (trust == nil || !trust(leaf, err)) {
				st.refused = true
				return ErrCertRefused
			}
			st.accepted = leaf
			return nil
		},
	}
}

func dialFTP(req terminal.ConnectRequest, conn net.Conn, mode ftpMode, trust CertTrust) (*ftpFS, error) {
	addr := net.JoinHostPort(req.Host, strconv.Itoa(req.Port))
	var (
		cfg *tls.Config
		st  tlsState
	)
	if mode != ftpPlain {
		cfg = ftpsConfig(req.Host, trust, &st)
	}
	// The library calls this for the control connection first, then for each
	// data connection. With a dial function of ours it leaves TLS to us,
	// except for upgrading the control connection after AUTH TLS.
	var ctrl net.Conn
	first, dialed := conn, false
	dial := func(network, address string) (net.Conn, error) {
		control := !dialed
		dialed = true
		c := first
		first = nil
		if c == nil {
			var err error
			if c, err = net.DialTimeout(network, address, 10*time.Second); err != nil {
				return nil, err
			}
		}
		if control {
			ctrl = c
		}
		switch {
		case cfg != nil && !control:
			return &ftpsData{Conn: tls.Client(c, cfg)}, nil
		case cfg != nil && mode == ftpImplicit:
			return tls.Client(c, cfg), nil
		}
		return c, nil
	}
	opts := []ftp.DialOption{ftp.DialWithTimeout(10 * time.Second), ftp.DialWithDialFunc(dial)}
	switch mode {
	case ftpExplicit:
		opts = append(opts, ftp.DialWithExplicitTLS(cfg))
	case ftpImplicit:
		opts = append(opts, ftp.DialWithTLS(cfg))
	}

	// What went wrong with TLS, if it did: the user's refusal, or (for AUTH
	// TLS) a server that can't do it, which plain FTP may still reach.
	tlsFailure := func(err error) error {
		if mode == ftpPlain {
			return nil
		}
		accepted, refused := st.seen()
		switch {
		case refused:
			return ErrCertRefused
		case !accepted && mode == ftpExplicit:
			return fmt.Errorf("%w: %v", errNoTLS, err)
		}
		return nil
	}

	c, err := ftp.Dial(addr, opts...)
	if err != nil {
		var te *textproto.Error
		if mode == ftpExplicit && errors.As(err, &te) && te.Code >= 500 {
			return nil, fmt.Errorf("%w: %v", errNoTLS, err) // AUTH TLS refused
		}
		if terr := tlsFailure(err); terr != nil {
			return nil, terr
		}
		return nil, fmt.Errorf("FTP 접속 실패: %w", err)
	}

	user, pass := req.Login, req.Pass
	if user == "" {
		user, pass = "anonymous", "anonymous@"
	}
	// With AUTH TLS the handshake happens here, before USER is sent.
	if err := c.Login(user, pass); err != nil {
		c.Quit()
		if terr := tlsFailure(err); terr != nil {
			return nil, terr
		}
		return nil, fmt.Errorf("FTP 로그인 실패: %w", err)
	}

	f := &ftpFS{c: c, names: codec.New(req.Encoding), stop: make(chan struct{}), secure: mode != ftpPlain, ctrl: ctrl}
	go f.keepAlive()
	return f, nil
}

// ftpsData is a TLS data connection, closed the way FTPS servers expect when
// it was used for an upload or left before the end (the viewer's size limit):
// close_notify, then reading until the server closes. Closing at once with
// data still unread (the rest of a file, or the session tickets a TLS 1.3
// server sends after the handshake) makes Windows reset the connection, and
// some servers then never answer the transfer, which leaves the control
// connection waiting for a reply.
type ftpsData struct {
	*tls.Conn
	sent bool // something was written: an upload
	eof  bool // everything was read: a finished download or listing
}

func (d *ftpsData) Write(p []byte) (int, error) {
	d.sent = true
	return d.Conn.Write(p)
}

func (d *ftpsData) Read(p []byte) (int, error) {
	n, err := d.Conn.Read(p)
	if err == io.EOF {
		d.eof = true
	}
	return n, err
}

// Handshake is how the library starts an empty upload.
func (d *ftpsData) Handshake() error {
	d.sent = true
	return d.Conn.Handshake()
}

// ftpsDrain is how long the rest of a data connection is read before closing.
const ftpsDrain = 10 * time.Second

func (d *ftpsData) Close() error {
	if d.sent || !d.eof {
		_ = d.Conn.CloseWrite()
		_ = d.Conn.SetReadDeadline(time.Now().Add(ftpsDrain))
		_, _ = io.Copy(io.Discard, d.Conn)
	}
	return d.Conn.Close()
}

// Secure reports whether the connection is FTPS.
func (f *ftpFS) Secure() bool { return f.secure }

// CertFingerprint is how certificates are shown and remembered: SHA256:base64,
// like OpenSSH's key fingerprints.
func CertFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// CertProblem says in words why the system doesn't trust a certificate.
func CertProblem(err error) string {
	var (
		unknown  x509.UnknownAuthorityError
		invalid  x509.CertificateInvalidError
		hostname x509.HostnameError
	)
	switch {
	case errors.As(err, &unknown):
		return "믿을 수 있는 기관이 발급하지 않았습니다 (자체 서명 등)"
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "유효 기간이 지났거나 아직 시작되지 않았습니다"
	case errors.As(err, &hostname):
		return "인증서의 이름이 접속한 주소와 다릅니다"
	}
	return err.Error()
}

// CertName is who a certificate was issued to: its common name, or else its
// first DNS name or IP address.
func CertName(cert *x509.Certificate) string {
	switch {
	case cert.Subject.CommonName != "":
		return cert.Subject.CommonName
	case len(cert.DNSNames) > 0:
		return strings.Join(cert.DNSNames, ", ")
	case len(cert.IPAddresses) > 0:
		return cert.IPAddresses[0].String()
	}
	return cert.Subject.String()
}

// keepAlive sends NOOP while idle so the server doesn't drop the session.
func (f *ftpFS) keepAlive() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			if f.mu.TryLock() { // skip while a transfer is running
				_ = f.c.NoOp()
				f.mu.Unlock()
			}
		}
	}
}

func (f *ftpFS) Getwd() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir, err := f.c.CurrentDir()
	if err != nil {
		return "/", nil
	}
	return f.names.DecodeString(dir), nil
}

func (f *ftpFS) List(dir string) ([]FileEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, err := f.c.List(f.names.EncodeString(dir))
	if err != nil {
		return nil, err
	}
	list := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		list = append(list, FileEntry{
			Name:    f.names.DecodeString(e.Name),
			Size:    int64(e.Size),
			IsDir:   e.Type == ftp.EntryTypeFolder || (e.Type == ftp.EntryTypeLink && e.Size == 0),
			ModTime: e.Time.Local().Format("2006-01-02 15:04"),
		})
	}
	return list, nil
}

func (f *ftpFS) Download(remotePath string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.c.Retr(f.names.EncodeString(remotePath))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	return err
}

func (f *ftpFS) Upload(remotePath string, r io.Reader, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.Stor(f.names.EncodeString(remotePath), r)
}

// DownloadFrom sends remotePath from offset on (REST + RETR).
func (f *ftpFS) DownloadFrom(remotePath string, offset int64, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, err := f.c.RetrFrom(f.names.EncodeString(remotePath), uint64(offset))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	if cerr := r.Close(); err == nil {
		err = cerr
	}
	return err
}

// UploadFrom writes r at offset of remotePath (REST + STOR).
func (f *ftpFS) UploadFrom(remotePath string, r io.Reader, offset int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.StorFrom(f.names.EncodeString(remotePath), r, uint64(offset))
}

func (f *ftpFS) Remove(p string, isDir bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if isDir {
		return f.c.RemoveDirRecur(f.names.EncodeString(p))
	}
	return f.c.Delete(f.names.EncodeString(p))
}

func (f *ftpFS) Rename(from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.Rename(f.names.EncodeString(from), f.names.EncodeString(to))
}

func (f *ftpFS) Mkdir(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.c.MakeDir(f.names.EncodeString(p))
}

func (f *ftpFS) Close() error {
	close(f.stop)
	if !f.mu.TryLock() {
		// A command is still running, perhaps waiting for a reply that never
		// comes; closing the socket ends it instead of waiting with it.
		return f.ctrl.Close()
	}
	defer f.mu.Unlock()
	return f.c.Quit()
}
