// Package skills: security guard for agent-authored skills.
//
// The guard scans SKILL.md content (and optionally supporting file contents)
// for patterns that indicate data exfiltration, prompt injection, destructive
// commands, credential leaks, or obfuscated payloads. The scan is advisory:
// the manager may decide to block on Severity="block" findings and warn on
// Severity="warn".
//
// Designed to match the upstream hermes-agent skills_guard.py risk model at a
// fraction of the complexity. Start with ~30 patterns; grow as needed.
package skills

import (
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// normalizeForGuard applies a defense-in-depth normalization pass before
// pattern matching: NFKC (collapses look-alike unicode + ligatures), zero-
// width and bidi-override control strip, and lowercase. This is the same
// class of normalization as approval/check.go's normalize() and closes the
// "use a different unicode variant to slip past a regex" family of bypasses.
func normalizeForGuard(s string) string {
	// NFKC compatibility decomposition + canonical composition.
	s = norm.NFKC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Strip zero-width + bidi-control chars that are a common injection trick.
		if r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF ||
			(r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			continue
		}
		// Strip other format characters (category Cf).
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// Finding is one matched pattern.
type Finding struct {
	Category string `json:"category"`
	Pattern  string `json:"pattern"`
	Severity string `json:"severity"`
	Line     int    `json:"line"`
	Excerpt  string `json:"excerpt"`
}

// ReviewResult summarises a scan.
type ReviewResult struct {
	Blocked    bool      `json:"blocked"`
	Findings   []Finding `json:"findings"`
	ScannedAt  int64     `json:"scanned_at"`
	DurationMs int64     `json:"duration_ms"`
}

// Guard runs pattern checks.
type Guard struct {
	// MaxFindings caps the number of findings returned per scan. 0 = default 50.
	MaxFindings int
}

// NewGuard returns a default guard.
func NewGuard() *Guard {
	return &Guard{MaxFindings: 50}
}

// Scan checks content for threat patterns. Returns the findings and whether
// the content should be blocked (any Severity="block" finding).
func (g *Guard) Scan(content string) ReviewResult {
	start := time.Now()
	max := g.MaxFindings
	if max <= 0 {
		max = 50
	}

	result := ReviewResult{
		ScannedAt: start.UnixMilli(),
		Findings:  make([]Finding, 0, 8),
	}

	// Scan two streams per line: (1) the raw line so the excerpt shown to
	// users reflects what they actually wrote, and (2) the normalized line
	// for pattern matching. This prevents trivial unicode/case bypasses
	// while preserving a human-readable report.
	rawLines := strings.Split(content, "\n")
	normLines := make([]string, len(rawLines))
	for i, ln := range rawLines {
		normLines[i] = normalizeForGuard(ln)
	}
	for _, p := range builtinPatterns {
		if len(result.Findings) >= max {
			break
		}
		// Credentials + zero-width injection patterns scan the RAW line so
		// casing and invisible chars remain visible; all other patterns scan
		// the normalized line (lowercase + NFKC + zero-width stripped).
		source := normLines
		if p.CaseSensitive {
			source = rawLines
		}
		// Scan line by line so we can report the line number.
		for i, scanLine := range source {
			if len(result.Findings) >= max {
				break
			}
			if !p.Re.MatchString(scanLine) {
				continue
			}
			line := rawLines[i]
			m := p.Re.FindString(scanLine)
			if m == "" {
				m = line
			}
			if len(m) > 120 {
				m = m[:120] + "…"
			}
			result.Findings = append(result.Findings, Finding{
				Category: p.Category,
				Pattern:  p.Label,
				Severity: p.Severity,
				Line:     i + 1,
				Excerpt:  m,
			})
			if p.Severity == "block" {
				result.Blocked = true
			}
		}
	}

	result.DurationMs = time.Since(start).Milliseconds()
	return result
}
