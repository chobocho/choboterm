package osutil

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// readMonoFont asks GNOME (gsettings), then KDE (kdeglobals).
func readMonoFont() MonoFont {
	if desc := gsetting("monospace-font-name"); desc != "" {
		family, points := parsePango(desc)
		scale, _ := strconv.ParseFloat(gsetting("text-scaling-factor"), 64)
		return MonoFont{Family: family, Size: toPixels(points, scale)}
	}
	if v := kdeFixed(); v != "" {
		family, points := parseKDE(v)
		return MonoFont{Family: family, Size: toPixels(points, 1)}
	}
	return MonoFont{}
}

// gsetting reads a key of org.gnome.desktop.interface, or "" without GNOME.
func gsetting(key string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gsettings", "get", "org.gnome.desktop.interface", key).Output()
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(string(out)), "'")
}

// kdeFixed is the "fixed" entry of kdeglobals' [General] group, or "".
func kdeFixed() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, "kdeglobals"))
	if err != nil {
		return ""
	}
	defer f.Close()
	general := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			general = line == "[General]"
		} else if v, ok := strings.CutPrefix(line, "fixed="); ok && general {
			return v
		}
	}
	return ""
}
