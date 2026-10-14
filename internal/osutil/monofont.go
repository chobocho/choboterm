package osutil

import (
	"math"
	"strconv"
	"strings"
	"sync"
)

// MonoFont is the desktop's fixed-width font: what GNOME Terminal or Konsole
// draw with unless told otherwise. Size is in CSS pixels; both are empty
// where the desktop doesn't say.
type MonoFont struct {
	Family string `json:"family"`
	Size   int    `json:"size"`
}

var (
	monoOnce sync.Once
	monoFont MonoFont
)

// SystemMonoFont returns the desktop's fixed-width font, read once.
func SystemMonoFont() MonoFont {
	monoOnce.Do(func() { monoFont = readMonoFont() })
	return monoFont
}

// pxPerPoint converts points to CSS pixels (96 per inch).
const pxPerPoint = 96.0 / 72.0

// styleWords are the style and weight words a Pango font description may end
// with ("Monospace Bold 11"); they are not part of the family name.
var styleWords = map[string]bool{
	"regular": true, "book": true, "normal": true, "medium": true, "bold": true,
	"semi-bold": true, "semibold": true, "light": true, "thin": true, "heavy": true,
	"italic": true, "oblique": true, "condensed": true,
}

// parsePango reads a Pango font description such as "Ubuntu Sans Mono 13"
// (GNOME's monospace-font-name) into a family and a size in points.
func parsePango(desc string) (family string, points float64) {
	fields := strings.Fields(strings.Trim(strings.TrimSpace(desc), "'\""))
	if n := len(fields); n > 0 {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(fields[n-1], "px"), 64); err == nil && v > 0 {
			points = v
			if strings.HasSuffix(fields[n-1], "px") {
				points = v / pxPerPoint
			}
			fields = fields[:n-1]
		}
	}
	for len(fields) > 1 && styleWords[strings.ToLower(fields[len(fields)-1])] {
		fields = fields[:len(fields)-1]
	}
	return strings.TrimSuffix(strings.Join(fields, " "), ","), points
}

// parseKDE reads KDE's "fixed" font entry ("Hack,10,-1,5,50,0,0,0,0,0").
func parseKDE(value string) (family string, points float64) {
	parts := strings.Split(value, ",")
	family = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		if v, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil && v > 0 {
			points = v
		}
	}
	return family, points
}

// toPixels is points scaled by the desktop's text scaling, in whole pixels.
func toPixels(points, scale float64) int {
	if points <= 0 {
		return 0
	}
	if scale <= 0 {
		scale = 1
	}
	return int(math.Round(points * pxPerPoint * scale))
}
