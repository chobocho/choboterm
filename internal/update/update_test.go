package update

import (
	"testing"
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
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
