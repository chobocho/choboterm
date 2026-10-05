package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Debug log: set CHOBOTERM_DEBUG=1, or create %AppData%\choboterm\debug.on, to write
// frontend and backend diagnostics to %AppData%\choboterm\debug.log (overwritten on each start).
var debugLog struct {
	mu sync.Mutex
	f  *os.File
}

func init() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	dir = filepath.Join(dir, "choboterm")
	if _, err := os.Stat(filepath.Join(dir, "debug.on")); err != nil && os.Getenv("CHOBOTERM_DEBUG") == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	debugLog.f, _ = os.Create(filepath.Join(dir, "debug.log"))
	debugf("debug log started, version %s", AppVersion)
}

func debugf(format string, args ...interface{}) {
	debugLog.mu.Lock()
	defer debugLog.mu.Unlock()
	if debugLog.f == nil {
		return
	}
	fmt.Fprintf(debugLog.f, "%s [go] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// DebugEnabled tells the frontend whether to send diagnostics.
func (a *App) DebugEnabled() bool {
	debugLog.mu.Lock()
	defer debugLog.mu.Unlock()
	return debugLog.f != nil
}

// DebugLog writes a frontend diagnostic line.
func (a *App) DebugLog(msg string) {
	debugLog.mu.Lock()
	defer debugLog.mu.Unlock()
	if debugLog.f == nil {
		return
	}
	fmt.Fprintf(debugLog.f, "%s [js] %s\n", time.Now().Format("15:04:05.000"), msg)
}
