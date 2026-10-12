package transfer

import (
	"testing"
	"time"
)

func TestParseLsLine(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.Local)
	cases := []struct {
		line string
		want FileEntry
		ok   bool
	}{
		{"drwxr-xr-x  2 root root    4096 Oct  3 01:02 bin", FileEntry{Name: "bin", Size: 4096, IsDir: true, ModTime: "2026-10-03 01:02"}, true},
		{"-rw-r--r--  1 user users 123456 Dec 31 23:59 my file.txt", FileEntry{Name: "my file.txt", Size: 123456, ModTime: "2025-12-31 23:59"}, true},
		{"-rw-r--r--    1 0        0              17 Jan  5  2021 busybox.cfg", FileEntry{Name: "busybox.cfg", Size: 17, ModTime: "2021-01-05"}, true},
		{"-rw-r--r--. 1 a b 5 Oct  2 22:41 한글.txt", FileEntry{Name: "한글.txt", Size: 5, ModTime: "2026-10-02 22:41"}, true},
		{"crw-rw-rw-  1 root root 1, 3 Oct  3 01:02 null", FileEntry{}, false},
		{"total 48", FileEntry{}, false},
	}
	for _, c := range cases {
		got, ok := parseLsLine(c.line, now)
		if ok != c.ok || got != c.want {
			t.Errorf("%q:\n got %+v %v\nwant %+v %v", c.line, got, ok, c.want, c.ok)
		}
	}
}
