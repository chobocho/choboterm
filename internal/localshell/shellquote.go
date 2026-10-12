package localshell

import (
	"strings"
)

// ShQuote quotes s as one word for a POSIX shell.
func ShQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// WinQuote quotes an argument for a Windows command line (CommandLineToArgvW rules).
func WinQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, c := range s {
		switch c {
		case '\\':
			slashes++
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*slashes+1))
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		b.WriteRune(c)
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}
