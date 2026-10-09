package main

// English screen, Go side. Most text reaches the page and is translated
// there (see frontend/src/i18n.ts); this covers what doesn't: Windows file
// dialogs, the host key question, messages written into the terminal and the
// lines added to session logs. Both sides use the same dictionary.

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed frontend/src/i18n_en.json
var enJSON []byte

var hangul = regexp.MustCompile(`[\x{ac00}-\x{d7a3}]`)

type enPattern struct {
	re  *regexp.Regexp
	to  string
	lit int // length of the key without its {}
}

var enDict = sync.OnceValues(func() (map[string]string, []enPattern) {
	exact := map[string]string{}
	_ = json.Unmarshal(enJSON, &exact)
	var pats []enPattern
	for k, to := range exact {
		if !strings.Contains(k, "{}") {
			continue
		}
		parts := strings.Split(k, "{}")
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		re := regexp.MustCompile(`^` + strings.Join(parts, `([\s\S]*?)`) + `$`)
		pats = append(pats, enPattern{re, to, len(strings.ReplaceAll(k, "{}", ""))})
	}
	// The more literal text a key has, the earlier it is tried.
	sort.Slice(pats, func(i, j int) bool {
		if pats[i].lit != pats[j].lit {
			return pats[i].lit > pats[j].lit
		}
		return pats[i].re.String() < pats[j].re.String()
	})
	return exact, pats
})

var placeholder = regexp.MustCompile(`\{(\d*)\}`)

func enTranslate(s string, depth int) string {
	if !hangul.MatchString(s) {
		return s
	}
	core := strings.TrimSpace(s)
	lead := s[:strings.Index(s, core)]
	tail := s[len(lead)+len(core):]
	exact, pats := enDict()
	if out, ok := exact[core]; ok {
		return lead + out + tail
	}
	if depth <= 3 {
		for _, p := range pats {
			m := p.re.FindStringSubmatch(core)
			if m == nil {
				continue
			}
			parts := m[1:]
			next := 0
			out := placeholder.ReplaceAllStringFunc(p.to, func(ph string) string {
				i := next
				if n := ph[1 : len(ph)-1]; n != "" {
					i, _ = strconv.Atoi(n)
				} else {
					next++
				}
				if i < len(parts) {
					return enTranslate(parts[i], depth+1)
				}
				return ""
			})
			return lead + out + tail
		}
	}
	if strings.Contains(core, "\n") {
		lines := strings.Split(core, "\n")
		for i, l := range lines {
			lines[i] = enTranslate(l, depth)
		}
		return lead + strings.Join(lines, "\n") + tail
	}
	return s
}

// tr returns s in the screen language (unchanged in Korean).
func tr(s string) string {
	if appLang() != "en" {
		return s
	}
	return enTranslate(s, 0)
}

// appLang is the screen language: "ko" or "en". Settings.Language "" follows Windows.
func appLang() string {
	switch l := loadSettings().Language; l {
	case "ko", "en":
		return l
	}
	if systemKorean() {
		return "ko"
	}
	return "en"
}

// GetLanguage tells the page which language to show.
func (a *App) GetLanguage() string {
	return appLang()
}
