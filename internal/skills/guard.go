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
)

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

	lines := strings.Split(content, "\n")
	for _, p := range builtinPatterns {
		if len(result.Findings) >= max {
			break
		}
		// Scan line by line so we can report the line number.
		for i, line := range lines {
			if len(result.Findings) >= max {
				break
			}
			m := p.Re.FindString(line)
			if m == "" {
				continue
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
