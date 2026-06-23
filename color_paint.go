package output

// Color is opt-in visual styling layered onto the family's structured output.
// It is purely cosmetic: when enabled, ANSI escapes are inserted around the
// tokens of the canonical JSON/NDJSON bytes; when disabled, output is
// byte-identical to the uncolored encoder. That invariant is what keeps colored
// output safe for machine consumers — stripping the escapes yields the exact
// original bytes, and the disabled path never runs the colorizer at all.
//
// Three concerns compose, separated so each can change without touching the
// others — and split across files accordingly: the decision (color_mode.go), the
// Painter and theme (this file), and the per-format colorizers (color_json.go,
// color_yaml.go).
//   - the decision — WHETHER to color a given stream: configured at runtime via
//     SetColorMode (the --color flag) and SetTerminalDetector (isatty injection).
//   - the Painter — HOW a token's Role is styled: a code-level seam. The
//     colorizer depends only on the Painter interface, so swapping the mechanism
//     (ANSI → a styling library, truecolor, HTML) is a one-type change with no
//     caller impact. There is deliberately no runtime SetPainter — there is one
//     painter today; add an injector when a second one actually exists.
//   - the theme — the Role→style map the ANSI painter consults.

// Role is the semantic class of a JSON token. It is contract-aware, not merely
// syntactic: values of the known envelope fields (error/fixable_by/hint/notice)
// get their own roles so they can be emphasised distinctly from ordinary data.
type Role int

const (
	RolePunct   Role = iota // structural punctuation: { } [ ] , :
	RoleKey                 // an object key
	RoleString              // an ordinary string value
	RoleNumber              // a numeric value
	RoleBool                // true / false
	RoleNull                // null
	RoleError               // the value of an "error" field
	RoleFixable             // the value of a "fixable_by" field
	RoleHint                // the value of a "hint" field
	RoleNotice              // the value of a "notice" field
)

// Painter turns a token and its Role into styled bytes. This is the swappable
// mechanism seam: the colorizer depends only on this interface and never on how
// the style is actually applied.
type Painter interface {
	Paint(role Role, s string) string
}

const (
	ansiReset   = "\x1b[0m"
	ansiDim     = "\x1b[2m"
	ansiBoldRed = "\x1b[1;31m"
	ansiYellow  = "\x1b[33m"
	ansiCyan    = "\x1b[36m"
	ansiMagenta = "\x1b[35m"
)

// defaultTheme dims the scaffolding (punctuation, keys) so ordinary string
// values — left unstyled, i.e. the terminal's default foreground — stand out as
// "the data". Numbers/bools/null get muted accents, and the known envelope
// fields get semantic emphasis (error in bold red, fixable_by/hint in yellow,
// notice in cyan). An empty style means "leave the token unstyled".
var defaultTheme = map[Role]string{
	RolePunct:   ansiDim,
	RoleKey:     ansiDim,
	RoleString:  "",
	RoleNumber:  ansiCyan,
	RoleBool:    ansiMagenta,
	RoleNull:    ansiDim,
	RoleError:   ansiBoldRed,
	RoleFixable: ansiYellow,
	RoleHint:    ansiYellow,
	RoleNotice:  ansiCyan,
}

type ansiPainter struct{ theme map[Role]string }

func (p ansiPainter) Paint(role Role, s string) string {
	style, ok := p.theme[role]
	if !ok || style == "" {
		return s
	}
	return style + s + ansiReset
}

// defaultPainter is the ANSI painter used when color is enabled.
var defaultPainter Painter = ansiPainter{theme: defaultTheme}

// valueRole maps a known envelope field name to its semantic Role, falling back
// to def for ordinary data. Shared by both the JSON and YAML colorizers.
func valueRole(key string, def Role) Role {
	switch key {
	case "error":
		return RoleError
	case "fixable_by":
		return RoleFixable
	case "hint":
		return RoleHint
	case "notice":
		return RoleNotice
	}
	return def
}
