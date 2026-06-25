package output

import (
	"bytes"
	"regexp"
	"strings"
)

// urlRE matches an http(s) URL within an already-tokenized string content run.
// The content the colorizers hand it is a single string value's bytes (the
// surrounding quotes and any escapes are handled by the caller), so a URL runs
// until whitespace or a delimiter that cannot appear in one. Trailing sentence
// punctuation is trimmed separately — see paintContent.
var urlRE = regexp.MustCompile(`https?://[^\s"'<>` + "`" + `]+`)

// paintContent paints one string-value content run, diverting any http(s) URLs to
// RoleURL (cyan + underline) while the rest stays under role. The "://" gate keeps
// the common no-URL case to a single Paint call. Every byte is emitted exactly, so
// stripping the escapes still reproduces the run — the colorizers' core invariant.
func paintContent(out *bytes.Buffer, s string, role Role, p Painter) {
	if !strings.Contains(s, "://") {
		out.WriteString(p.Paint(role, s))
		return
	}
	last := 0
	for _, m := range urlRE.FindAllStringIndex(s, -1) {
		start, end := m[0], m[1]
		// Trim trailing punctuation that's more likely sentence/structure than part
		// of the URL ("see https://x.test." → the period stays content). The trimmed
		// bytes fall into the role-painted gap below, so nothing is dropped.
		for end > start && isURLTrailingPunct(s[end-1]) {
			end--
		}
		if end <= start {
			continue
		}
		if start > last {
			out.WriteString(p.Paint(role, s[last:start]))
		}
		out.WriteString(p.Paint(RoleURL, s[start:end]))
		last = end
	}
	if last < len(s) {
		out.WriteString(p.Paint(role, s[last:]))
	}
}

func isURLTrailingPunct(c byte) bool {
	switch c {
	case '.', ',', ';', ':', '!', '?', ')', ']', '}', '\'', '"', '>':
		return true
	}
	return false
}
