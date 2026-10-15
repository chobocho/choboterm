package app

// Opening a web page or a folder on request: Lua's open_url and open_folder
// (in a tab's script or console, and in the cat's runs) and the cat's plain
// messages ("https://...", "폴더 C:\work"; see OpenTarget). A script must not
// turn into a launcher, so only http(s) pages and folders that exist are
// opened (Explorer runs a file it is given), and only so often.

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"choboterm/internal/osutil"
	"choboterm/internal/web"
)

const (
	openURL    = "url"
	openFolder = "folder"
	openMaxLen = 4096
	openBurst  = 5                // opens allowed...
	openWindow = 10 * time.Second // ...within this long, for the whole app
)

var errFolderElsewhere = errors.New("폴더는 이 PC에서만 열 수 있습니다")

// Opened is what OpenTarget opened.
type Opened struct {
	Target string `json:"target"` // the address or folder, as opened
	InPage bool   `json:"inPage"` // the page must open the address itself (a browser on another PC)
}

// checkURL returns s as a web address to open; "github.com/x" gets https://.
func checkURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > openMaxLen || strings.ContainsAny(s, " \t\r\n") {
		return "", fmt.Errorf("웹 주소가 아닙니다: %s", s)
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	// Parse lowercases the scheme and refuses control characters.
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("http · https 주소만 열 수 있습니다: %s", s)
	}
	return u.String(), nil
}

// checkFolder returns s as a folder to open: a full path ("~" is the home
// folder) of a folder that exists. Quotes around it, as copied from
// Explorer's "Copy as path", are dropped.
func checkFolder(s string) (string, error) {
	p := strings.TrimSpace(s)
	if len(p) >= 2 && (p[0] == '"' && p[len(p)-1] == '"' || p[0] == '\'' && p[len(p)-1] == '\'') {
		p = p[1 : len(p)-1]
	}
	if p == "" || len(p) > openMaxLen {
		return "", errors.New("폴더 경로가 없습니다")
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("전체 경로를 써 주세요: %s", s)
	}
	p = filepath.Clean(p)
	fi, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("폴더가 없습니다: %s", p)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("폴더가 아닙니다: %s", p)
	}
	return p, nil
}

// opens holds when recent opens happened, for the limit of openBurst per openWindow.
var opens struct {
	sync.Mutex
	at []time.Time
}

func openAllowed(now time.Time) bool {
	opens.Lock()
	defer opens.Unlock()
	keep := opens.at[:0]
	for _, t := range opens.at {
		if now.Sub(t) < openWindow {
			keep = append(keep, t)
		}
	}
	opens.at = keep
	if len(opens.at) >= openBurst {
		return false
	}
	opens.at = append(opens.at, now)
	return true
}

// open opens a web page (kind openURL) or a folder (openFolder) for a page
// that is at this PC (local) or not. Here it is shown on this PC's screen;
// for a browser elsewhere a folder can't be shown, and an address is
// returned for that page to open itself.
func (a *App) open(local bool, kind, target string) (Opened, error) {
	var err error
	switch kind {
	case openURL:
		target, err = checkURL(target)
	case openFolder:
		if !local {
			return Opened{}, errFolderElsewhere
		}
		target, err = checkFolder(target)
	default:
		err = fmt.Errorf("열 수 없는 종류입니다: %s", kind)
	}
	if err != nil {
		return Opened{}, err
	}
	if !openAllowed(time.Now()) {
		return Opened{}, fmt.Errorf("너무 자주 엽니다 (%d초에 %d번까지)", int(openWindow.Seconds()), openBurst)
	}
	if !local {
		return Opened{Target: target, InPage: true}, nil
	}
	run := a.hooks.open
	if run == nil {
		run = func(kind, target string) error {
			if kind == openFolder {
				return osutil.OpenFolder(target)
			}
			return osutil.OpenURL(target)
		}
	}
	return Opened{Target: target}, run(kind, target)
}

// clientAtThisPC reports whether web client c (web.WindowClient: the
// window) is at this PC, as App.local does for the caller.
func (a *App) clientAtThisPC(c int) bool {
	if c == web.WindowClient {
		return true
	}
	if w := a.web.Load(); w != nil {
		if cl := w.Client(c); cl != nil {
			return cl.AtThisPC()
		}
	}
	return false
}

// OpenTarget opens a web page (kind "url") or a folder ("folder"): the
// cat's plain messages. A browser on another PC is told to open the
// address itself (InPage), which it does at once, still inside the click.
func (a *App) OpenTarget(kind, target string) (Opened, error) {
	return a.open(a.local(), kind, target)
}
