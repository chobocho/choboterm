package sessionlog

import (
	"testing"
	"time"
)

func TestPlainText(t *testing.T) {
	cases := []struct {
		name string
		in   []string // fed in pieces, to cross chunk boundaries
		want string
	}{
		{"lines", []string{"a\r\nb\n"}, "a\r\nb\r\n"},
		{"colors", []string{"\x1b[1;31mred\x1b[0m ok\r\n"}, "red ok\r\n"},
		{"split escape", []string{"x\x1b[3", "2my\r\n"}, "xy\r\n"},
		{"title", []string{"\x1b]0;user@host: ~\x07$ ls\r\n"}, "$ ls\r\n"},
		{"title ST", []string{"\x1b]2;t\x1b\\ok\r\n"}, "ok\r\n"},
		{"charset", []string{"\x1b(Bok\x1b=\r\n"}, "ok\r\n"},
		{"progress", []string{" 10%\r 50%\r100%\r\n"}, "100%\r\n"},
		{"backspace", []string{"lx\b \bs\r\n"}, "ls\r\n"},
		{"erase line", []string{"abcdef\r\x1b[Kxy\r\n"}, "xy\r\n"},
		{"korean split", []string{"한\xea", "\xb8\x80\r\n"}, "한글\r\n"},
		{"blank lines", []string{"a\r\n\r\nb\r\n"}, "a\r\n\r\nb\r\n"},
		{"bell", []string{"a\x07b\r\n"}, "ab\r\n"},
	}
	for _, c := range cases {
		p := &PlainText{}
		var got []byte
		for _, s := range c.in {
			got = append(got, p.Feed([]byte(s))...)
		}
		got = append(got, p.rest()...)
		if string(got) != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLogFileName(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 5, 7, 0, time.Local)
	if got := logFileName("example.com", 22, at); got != "example.com_22_20261003_090507.log" {
		t.Fatal(got)
	}
	if got := logFileName("::1", 2222, at); got != "__1_2222_20261003_090507.log" {
		t.Fatal(got)
	}
	if got := logFileName("", 0, at); got != "session_20261003_090507.log" {
		t.Fatal(got)
	}
}

// fakeClock makes logNow return the times given, one per call, then the last.
func fakeClock(t *testing.T, times ...time.Time) {
	old := logNow
	t.Cleanup(func() { logNow = old })
	logNow = func() time.Time {
		at := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return at
	}
}

func TestLogTimestamps(t *testing.T) {
	t1 := time.Date(2026, 10, 9, 21, 30, 15, 123e6, time.Local)
	t2 := t1.Add(5 * time.Second)
	t3 := t2.Add(time.Minute)
	s1, s2, s3 := "[2026-10-09 21:30:15.123] ", "[2026-10-09 21:30:20.123] ", "[2026-10-09 21:31:20.123] "

	// Plain text: a line gets the time its first character came, not when it ended.
	fakeClock(t, t1, t2, t3)
	p := &PlainText{stamp: true}
	got := string(p.Feed([]byte("$ make"))) // t1: the prompt line starts
	got += string(p.Feed([]byte("\r\n\r\nok\r\n")))
	if want := s1 + "$ make\r\n" + s2 + "\r\n" + s3 + "ok\r\n"; got != want {
		t.Errorf("plain:\n got %q\nwant %q", got, want)
	}

	// Raw output: the time goes in front of each line, also across chunks.
	fakeClock(t, t1, t2)
	l := &Log{stamp: true, bol: true}
	raw := string(l.stampRaw([]byte("\x1b[31mred\x1b[0m\r\nmo")))
	raw += string(l.stampRaw([]byte("re\r\n")))
	raw += string(l.stampRaw([]byte("last")))
	if want := s1 + "\x1b[31mred\x1b[0m\r\n" + s1 + "more\r\n" + s2 + "last"; raw != want {
		t.Errorf("raw:\n got %q\nwant %q", raw, want)
	}
}
