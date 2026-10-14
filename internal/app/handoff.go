package app

// Starting choboterm again while it runs with the web server (its window
// maybe hidden, see WebHideWindow) shows that window instead of starting a
// second choboterm, which couldn't use the same port anyway.
//
// The new process asks POST /web/show?nonce=N on the server's port. The
// running one shows its window and answers HMAC-SHA256(token, nonce): only
// a choboterm of the same user (same web-token file) can, so the new process
// quits only for its own app, and the token itself never leaves the process.
// This works however the running one turned the server on (--web or later
// in Settings), unlike a single-instance lock taken at start.

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"choboterm/internal/config"
	"choboterm/internal/dbg"
)

// showProof is the answer only the holder of token can give for nonce.
func showProof(token, nonce string) []byte {
	m := hmac.New(sha256.New, []byte(token))
	m.Write([]byte("choboterm-show:" + nonce))
	return m.Sum(nil)
}

// handleShow brings the window to the front for a choboterm started again.
// It needs no login: showing the window is harmless, and only this PC may ask.
func (w *webServer) handleShow(rw http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("nonce")
	if r.Method != http.MethodPost || !loopbackRemote(r.RemoteAddr) || !w.host.HasWindow() ||
		len(nonce) < 16 || len(nonce) > 128 {
		http.NotFound(rw, r)
		return
	}
	go w.host.ShowWindow()
	writeJSON(rw, map[string]string{"proof": hex.EncodeToString(showProof(w.token, nonce))})
}

// readWebToken returns the access token if there is one (never makes one).
func readWebToken() string {
	p, err := config.File()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(p), "web-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// handoffToRunning asks a choboterm already serving o.Addr to show its
// window, and reports whether it did (then this process should quit).
func handoffToRunning(o webOptions) bool {
	token := readWebToken()
	if token == "" {
		return false // no server has ever run for this user
	}
	addr := o.Addr
	if addr == "" {
		addr = webDefaultAddr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	// A server on every address (or this PC's) answers on the loopback
	// address; one on a specific address only there.
	if ip := net.ParseIP(host); host == "" || host == "localhost" || (ip != nil && (ip.IsUnspecified() || ip.IsLoopback())) {
		host = "127.0.0.1"
	}
	scheme := "http"
	if o.CertFile != "" || (!loopbackHost(strings.Trim(addrHost(addr), "[]")) && !o.Insecure) {
		scheme = "https"
	}
	nonce := randomHex(16)
	u := scheme + "://" + net.JoinHostPort(host, port) + "/web/show?nonce=" + url.QueryEscape(nonce)
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			Proxy: nil, // straight to this PC, never through HTTPS_PROXY
			// The certificate is often self-signed; no secret is sent, and
			// the answer itself proves who is there.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := client.Post(u, "text/plain", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var ans struct {
		Proof string `json:"proof"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&ans) != nil {
		return false
	}
	got, err := hex.DecodeString(ans.Proof)
	ok := err == nil && hmac.Equal(got, showProof(token, nonce))
	dbg.Printf("web: handoff to %s: %v", addr, ok)
	return ok
}

// addrHost is the host part of host:port ("" if it has none).
func addrHost(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	return h
}
