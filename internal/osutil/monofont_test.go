package osutil

import "testing"

func TestParsePango(t *testing.T) {
	for _, c := range []struct {
		in     string
		family string
		points float64
	}{
		{"'Ubuntu Sans Mono 13'", "Ubuntu Sans Mono", 13},
		{"Monospace 11", "Monospace", 11},
		{"DejaVu Sans Mono Bold Italic 10.5", "DejaVu Sans Mono", 10.5},
		{"Source Code Pro, 12", "Source Code Pro", 12},
		{"Hack 16px", "Hack", 12},
		{"Fira Code", "Fira Code", 0},
		{"", "", 0},
	} {
		family, points := parsePango(c.in)
		if family != c.family || points != c.points {
			t.Errorf("parsePango(%q) = %q, %v; want %q, %v", c.in, family, points, c.family, c.points)
		}
	}
}

func TestParseKDE(t *testing.T) {
	if family, points := parseKDE("Hack,10,-1,5,50,0,0,0,0,0"); family != "Hack" || points != 10 {
		t.Errorf("parseKDE = %q, %v", family, points)
	}
	if family, points := parseKDE("Noto Sans Mono"); family != "Noto Sans Mono" || points != 0 {
		t.Errorf("parseKDE without size = %q, %v", family, points)
	}
}

func TestToPixels(t *testing.T) {
	for _, c := range []struct {
		points, scale float64
		want          int
	}{
		{13, 1, 17}, // GNOME Terminal's default on Ubuntu: 13pt is 17px
		{11, 1, 15},
		{10, 1.25, 17},
		{12, 0, 16}, // no scaling factor read
		{0, 1, 0},
	} {
		if got := toPixels(c.points, c.scale); got != c.want {
			t.Errorf("toPixels(%v, %v) = %d, want %d", c.points, c.scale, got, c.want)
		}
	}
}
