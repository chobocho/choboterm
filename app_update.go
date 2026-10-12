package main

import (
	"fmt"
	"time"

	"choboterm/internal/config"
	"choboterm/internal/dbg"
	"choboterm/internal/update"
	"choboterm/internal/version"
)

// CheckUpdate looks for a newer release. manual is the "지금 확인" button:
// it always asks and always reports. Otherwise (at start) it asks only when
// the check is turned on and hasn't run today, and reports a new version once.
func (a *App) CheckUpdate(manual bool) (update.UpdateInfo, error) {
	info := update.UpdateInfo{Current: version.Version}
	s := config.Load()
	if !manual {
		if !s.UpdateCheck {
			return info, nil
		}
		if s.Update != nil && time.Since(s.Update.Last) < update.Every {
			return info, nil
		}
	}
	latest, url, err := update.LatestRelease()
	_ = config.Update(func(cur *config.Settings) {
		if cur.Update == nil {
			cur.Update = &config.UpdateState{}
		}
		cur.Update.Last = time.Now()
	})
	if err != nil {
		dbg.Printf("update check: %v", err)
		if manual {
			return info, fmt.Errorf("새 버전을 확인하지 못했습니다: %w", err)
		}
		return info, nil // a check at start fails quietly (offline, proxy...)
	}
	info.Checked, info.Latest, info.URL = true, latest, url
	info.Newer = update.Newer(latest, version.Version)
	dbg.Printf("update check: current %s, latest %s, newer %v", version.Version, latest, info.Newer)
	if info.Newer && !manual {
		if s.Update != nil && s.Update.Notified == latest {
			info.Newer = false // already told about this one
		} else {
			_ = config.Update(func(cur *config.Settings) { cur.Update.Notified = latest })
		}
	}
	return info, nil
}
