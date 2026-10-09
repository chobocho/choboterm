package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The update check asks GitHub for the latest release and tells the user
// when it is newer than this build. It only runs when the user turned it on
// in the settings (or pressed "지금 확인"); nothing is downloaded or installed.

// UpdateState is what the Go side remembers between checks.
type UpdateState struct {
	Last     time.Time `json:"last"`     // when GitHub was last asked
	Notified string    `json:"notified"` // the newest version already shown to the user
}

// UpdateInfo is the result of a check.
type UpdateInfo struct {
	Checked bool   `json:"checked"` // false: skipped (turned off, or already checked today)
	Current string `json:"current"` // "0.2.13"
	Latest  string `json:"latest"`  // "0.2.14"
	Newer   bool   `json:"newer"`   // Latest is newer than Current
	URL     string `json:"url"`     // the release page
}

// latestReleaseURL is the GitHub API for the newest release; tests may replace it.
var latestReleaseURL = "https://api.github.com/repos/chobocho/choboterm/releases/latest"

// updateEvery is how often the check at start asks GitHub.
const updateEvery = 24 * time.Hour

// CheckUpdate looks for a newer release. manual is the "지금 확인" button:
// it always asks and always reports. Otherwise (at start) it asks only when
// the check is turned on and hasn't run today, and reports a new version once.
func (a *App) CheckUpdate(manual bool) (UpdateInfo, error) {
	info := UpdateInfo{Current: AppVersion}
	s := loadSettings()
	if !manual {
		if !s.UpdateCheck {
			return info, nil
		}
		if s.Update != nil && time.Since(s.Update.Last) < updateEvery {
			return info, nil
		}
	}
	latest, url, err := fetchLatestRelease()
	_ = updateSettings(func(cur *Settings) {
		if cur.Update == nil {
			cur.Update = &UpdateState{}
		}
		cur.Update.Last = time.Now()
	})
	if err != nil {
		debugf("update check: %v", err)
		if manual {
			return info, fmt.Errorf("새 버전을 확인하지 못했습니다: %w", err)
		}
		return info, nil // a check at start fails quietly (offline, proxy...)
	}
	info.Checked, info.Latest, info.URL = true, latest, url
	info.Newer = newerVersion(latest, AppVersion)
	debugf("update check: current %s, latest %s, newer %v", AppVersion, latest, info.Newer)
	if info.Newer && !manual {
		if s.Update != nil && s.Update.Notified == latest {
			info.Newer = false // already told about this one
		} else {
			_ = updateSettings(func(cur *Settings) { cur.Update.Notified = latest })
		}
	}
	return info, nil
}

// fetchLatestRelease returns the newest release's version ("0.2.14") and page.
func fetchLatestRelease() (version, page string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "choboterm/"+AppVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub 응답 %s", resp.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	v := strings.TrimLeft(r.TagName, "vV")
	if parseVersion(v) == nil {
		return "", "", errors.New("알 수 없는 버전: " + r.TagName)
	}
	return v, r.HTMLURL, nil
}

// parseVersion reads "0.2.13" as numbers, or nil if it isn't one.
func parseVersion(v string) []int {
	parts := strings.Split(v, ".")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		nums[i] = n
	}
	return nums
}

// newerVersion reports whether version a is newer than b ("0.2.10" > "0.2.9").
func newerVersion(a, b string) bool {
	x, y := parseVersion(a), parseVersion(b)
	if x == nil || y == nil {
		return false
	}
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p != q {
			return p > q
		}
	}
	return false
}
