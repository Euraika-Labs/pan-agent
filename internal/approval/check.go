package approval

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Level represents the approval level required for a command.
type Level int

const (
	Safe         Level = 0
	Dangerous    Level = 1
	Catastrophic Level = 2
)

// ApprovalCheck is the result of classifying a command.
type ApprovalCheck struct {
	Level       Level
	PatternKey  string
	Description string
}

// ansiEscape matches ANSI/VT escape sequences (ECMA-48: CSI, OSC, DCS, APC,
// PM, SOS, 8-bit C1 controls) so they can be stripped before pattern matching.
var ansiEscape = regexp.MustCompile(
	`\x1b(?:` +
		`[@-Z\\-_]` + // Fe: ESC + single byte
		`|\[[0-?]*[ -/]*[@-~]` + // CSI sequences
		`|\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC (BEL or ST terminated)
		`|\^[^\x1b]*\x1b\\` + // PM
		`|_[^\x1b]*\x1b\\` + // APC
		`|P[^\x1b]*\x1b\\` + // DCS
		`)` +
		// 8-bit C1 equivalents
		`|\x9b[0-?]*[ -/]*[@-~]` + // CSI (8-bit)
		`|\x9d[^\x07\x9c]*(?:\x07|\x9c)` + // OSC (8-bit)
		`|\x9e[^\x9c]*\x9c` + // PM (8-bit)
		`|\x9f[^\x9c]*\x9c` + // APC (8-bit)
		`|\x90[^\x9c]*\x9c`, // DCS (8-bit)
)

// nfkcNormalize performs a best-effort NFKC-style normalization over the
// runes that matter for command obfuscation: fullwidth Latin letters/digits
// (U+FF01–U+FF5E → U+0021–U+007E) and a handful of other lookalike ranges.
// This matches the intent of unicodedata.normalize('NFKC', ...) in the Python
// source without requiring golang.org/x/text.
func nfkcNormalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		b.WriteRune(normalizeRune(r))
	}
	return b.String()
}

// normalizeRune maps a single rune to its NFKC ASCII equivalent where the
// mapping is relevant to command-injection obfuscation detection.
func normalizeRune(r rune) rune {
	switch {
	// Fullwidth ASCII variants: U+FF01 (！) … U+FF5E (～) → U+0021 (!) … U+007E (~)
	case r >= 0xFF01 && r <= 0xFF5E:
		return r - 0xFEE0
	// Halfwidth katakana space U+FF61–U+FF9F — keep as-is (not command-relevant)
	// Ideographic space U+3000 → regular space U+0020
	case r == 0x3000:
		return ' '
	// Mathematical bold/italic/script Latin letters back to ASCII.
	// Block U+1D400–U+1D7FF contains styled Latin/digit forms.
	case r >= 0x1D400 && r <= 0x1D7FF:
		return mathLatinToASCII(r)
	default:
		return r
	}
}

// mathLatinToASCII maps Mathematical Alphanumeric Symbols (U+1D400–U+1D7FF)
// to their plain ASCII equivalents where possible.
func mathLatinToASCII(r rune) rune {
	// Mathematical Bold Capital A–Z: U+1D400–U+1D419 → A–Z
	if r >= 0x1D400 && r <= 0x1D419 {
		return rune('A') + (r - 0x1D400)
	}
	// Mathematical Bold Small a–z: U+1D41A–U+1D433 → a–z
	if r >= 0x1D41A && r <= 0x1D433 {
		return rune('a') + (r - 0x1D41A)
	}
	// Mathematical Italic Capital A–Z: U+1D434–U+1D44D → A–Z
	if r >= 0x1D434 && r <= 0x1D44D {
		return rune('A') + (r - 0x1D434)
	}
	// Mathematical Italic Small a–z: U+1D44E–U+1D467 → a–z (h is missing, U+210E)
	if r >= 0x1D44E && r <= 0x1D467 {
		return rune('a') + (r - 0x1D44E)
	}
	// For deeper coverage, keep original (patterns still have (?i) flag).
	return r
}

// normalize strips ANSI escape sequences, null bytes, and normalizes Unicode
// so that obfuscation cannot bypass pattern detection. Matches the Python
// _normalize_command_for_detection function.
func normalize(command string) string {
	// 1. Strip ANSI escape sequences (full ECMA-48)
	command = ansiEscape.ReplaceAllString(command, "")
	// 2. Strip null bytes
	command = strings.ReplaceAll(command, "\x00", "")
	// 3. Strip other control characters (belt-and-suspenders, keep \n and \t)
	command = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return -1
		}
		return r
	}, command)
	// 4. Normalize Unicode fullwidth / mathematical variants (NFKC-lite)
	command = nfkcNormalize(command)
	return command
}

// matchPattern reports whether command matches pat, respecting the optional
// NegativeRegex guard (used for RE2-incompatible negative lookaheads).
func matchPattern(pat Pattern, command string) bool {
	if !pat.Regex.MatchString(command) {
		return false
	}
	if pat.NegativeRegex != nil && pat.NegativeRegex.MatchString(command) {
		return false
	}
	return true
}

// Check classifies command into Safe / Dangerous / Catastrophic.
//
// Catastrophic patterns are evaluated first; Level 2 takes precedence over
// Level 1 when a command matches both (e.g. "del /s /q C:\\" matches both
// Windows recursive delete AND del /s /q against drive root).
// The command is normalized before matching.
func Check(command string) ApprovalCheck {
	normalized := normalize(command)

	for _, pat := range CatastrophicPatterns {
		if matchPattern(pat, normalized) {
			return ApprovalCheck{
				Level:       Catastrophic,
				PatternKey:  pat.Key,
				Description: pat.Description,
			}
		}
	}

	for _, pat := range DangerousPatterns {
		if matchPattern(pat, normalized) {
			return ApprovalCheck{
				Level:       Dangerous,
				PatternKey:  pat.Key,
				Description: pat.Description,
			}
		}
	}

	return ApprovalCheck{Level: Safe}
}
