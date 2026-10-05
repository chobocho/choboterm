package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Debug log: set CHOBOTERM_DEBUG=1, or put a file named debug.on next to the exe, to write
// frontend and backend diagnostics to debug.log next to the exe (overwritten on each start).
var debugLog struct {
	mu sync.Mutex
	f  *os.File
}

func init() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, "debug.on")); err != nil && os.Getenv("CHOBOTERM_DEBUG") == "" {
		return
	}
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
