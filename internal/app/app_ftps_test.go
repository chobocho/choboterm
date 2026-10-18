package app

import (
	"crypto/x509"
	"errors"
	"testing"

	"choboterm/internal/store"
	"choboterm/internal/transfer"
)

func TestFTPSCertificateRememberedPerServer(t *testing.T) {
	useTempHosts(t)
	a := NewApp()
	var asked []string
	answer := true
	a.hooks.confirmKey = func(host, fingerprint string) bool {
		asked = append(asked, host+" "+fingerprint)
		return answer
	}
	certA := &x509.Certificate{Raw: []byte("certificate A")}
	certB := &x509.Certificate{Raw: []byte("certificate B")}
	why := errors.New("x509: certificate signed by unknown authority")
	trust := a.certTrust(1, "ftp.example", 21)

	// Refused: not remembered, so asked again next time.
	answer = false
	if trust(certA, why) || trust(certA, why) || len(asked) != 2 {
		t.Fatalf("refused: asked %q", asked)
	}
	// Trusted once: the next connection isn't asked about it.
	answer = true
	if !trust(certA, why) || !trust(certA, why) || len(asked) != 3 {
		t.Fatalf("trusted: asked %q", asked)
	}
	if got := store.PrefsFor("ftp.example", 21).FTPSCert; got != transfer.CertFingerprint(certA) {
		t.Fatalf("saved %q", got)
	}
	if asked[2] != "ftp.example:21 "+transfer.CertFingerprint(certA) {
		t.Fatalf("question %q", asked[2])
	}
	// Another certificate is asked about; another port of the host is another server.
	answer = false
	if trust(certB, why) || len(asked) != 4 {
		t.Fatalf("changed: asked %q", asked)
	}
	if store.PrefsFor("ftp.example", 21).FTPSCert != transfer.CertFingerprint(certA) {
		t.Fatal("a refused certificate replaced the trusted one")
	}
	if a.certTrust(1, "ftp.example", 990)(certA, why) || len(asked) != 5 {
		t.Fatalf("other port: asked %q", asked)
	}
}
