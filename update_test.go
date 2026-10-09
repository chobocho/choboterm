package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.14", "0.2.13", true},
		{"0.2.10", "0.2.9", true},
		{"0.3.0", "0.2.99", true},
		{"1.0", "0.9.9", true},
		{"0.2.13", "0.2.13", false},
		{"0.2.12", "0.2.13", false},
		{"0.2.13.1", "0.2.13", true},
		{"beta", "0.2.13", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// fakeGitHub serves a latest release with the given tag and counts requests.
func fakeGitHub(t *testing.T, tag string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://example.com/rel"}`))
	}))
	t.Cleanup(srv.Close)
	orig := latestReleaseURL
	latestReleaseURL = srv.URL
	t.Cleanup(func() { latestReleaseURL = orig })
	return &hits
}

func TestCheckUpdateOffByDefault(t *testing.T) {
	useTempSettings(t)
	hits := fakeGitHub(t, "v99.0.0")
	a := NewApp()
	info, err := a.CheckUpdate(false)
	if err != nil || info.Checked || info.Newer || hits.Load() != 0 {
		t.Fatalf("check at start with the setting off: %+v %v, %d requests", info, err, hits.Load())
	}
	// The button asks even when the check at start is off.
	info, err = a.CheckUpdate(true)
	if err != nil || !info.Checked || !info.Newer || info.Latest != "99.0.0" || info.URL != "https://example.com/rel" {
		t.Fatalf("manual check: %+v %v", info, err)
	}
}

func TestCheckUpdateOncePerDayAndVersion(t *testing.T) {
	useTempSettings(t)
	hits := fakeGitHub(t, "v99.0.0")
	a := NewApp()
	s := a.GetSettings()
	s.UpdateCheck = true
	if err := a.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	if info, _ := a.CheckUpdate(false); !info.Newer {
		t.Fatalf("first check: %+v", info)
	}
	// Saving from the page keeps what the Go side remembers.
	if err := a.SaveSettings(a.GetSettings()); err != nil {
		t.Fatal(err)
	}
	if info, _ := a.CheckUpdate(false); info.Checked || hits.Load() != 1 {
		t.Fatalf("second check the same day: %+v, %d requests", info, hits.Load())
	}
	// A day later the same version isn't announced again.
	_ = updateSettings(func(s *Settings) { s.Update.Last = time.Now().Add(-25 * time.Hour) })
	if info, _ := a.CheckUpdate(false); !info.Checked || info.Newer {
		t.Fatalf("next day, same version: %+v", info)
	}
	// The button still reports it.
	if info, _ := a.CheckUpdate(true); !info.Newer {
		t.Fatalf("manual check: %+v", info)
	}
}

func TestCheckUpdateCurrent(t *testing.T) {
	useTempSettings(t)
	fakeGitHub(t, "v"+AppVersion)
	if info, err := NewApp().CheckUpdate(true); err != nil || !info.Checked || info.Newer {
		t.Fatalf("same version: %+v %v", info, err)
	}
}
