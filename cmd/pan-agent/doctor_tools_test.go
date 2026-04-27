package main

import (
	"strings"
	"testing"
)

// Phase 13 WS#13.D — doctor tool-config probe tests. Verifies the
// labels + detail strings each env-var configuration shape produces,
// since the doctor's text output is operator-facing and a quiet
// drift would be confusing.

// findCheck returns the doctorCheck with the given label, or nil.
func findCheck(checks []doctorCheck, label string) *doctorCheck {
	for i := range checks {
		if checks[i].Label == label {
			return &checks[i]
		}
	}
	return nil
}

// clearAllToolEnv strips every env var the tool checks read so each
// test starts with a clean slate. t.Setenv("") sets to empty (which
// is the "unset" path the helpers test for via TrimSpace == "").
func clearAllToolEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"STRIPE_API_KEY", "STRIPE_TEST_API_KEY",
		"SLACK_BOT_TOKEN",
		"NOTION_API_KEY",
		"JIRA_HOST", "JIRA_BEARER", "JIRA_EMAIL", "JIRA_API_TOKEN",
		"PAN_AGENT_RAG_EMBEDDER_URL", "PAN_AGENT_RAG_EMBEDDER_MODEL",
	} {
		t.Setenv(k, "")
	}
}

func TestToolConfigChecks_AllEmpty(t *testing.T) {
	clearAllToolEnv(t)
	checks := toolConfigChecks()
	for _, c := range checks {
		if !c.Pass {
			t.Errorf("%s: Pass=false (tool checks must never fail)", c.Label)
		}
		if !strings.Contains(c.Detail, "not configured") &&
			!strings.Contains(c.Detail, "partially configured") {
			t.Errorf("%s: Detail = %q, expected 'not configured' message", c.Label, c.Detail)
		}
	}
	// All five tools should appear.
	for _, label := range []string{
		"Tool: stripe", "Tool: slack", "Tool: notion", "Tool: jira",
		"RAG embedder",
	} {
		if findCheck(checks, label) == nil {
			t.Errorf("missing check %q", label)
		}
	}
}

func TestToolConfigChecks_StripeLiveOnly(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("STRIPE_API_KEY", "sk_live_fake")
	c := findCheck(toolConfigChecks(), "Tool: stripe")
	if c == nil || !strings.Contains(c.Detail, "live only") {
		t.Errorf("got %+v, want 'live only'", c)
	}
}

func TestToolConfigChecks_StripeTestOnly(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("STRIPE_TEST_API_KEY", "sk_test_fake")
	c := findCheck(toolConfigChecks(), "Tool: stripe")
	if c == nil || !strings.Contains(c.Detail, "test only") {
		t.Errorf("got %+v, want 'test only'", c)
	}
}

func TestToolConfigChecks_StripeBoth(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("STRIPE_API_KEY", "sk_live_fake")
	t.Setenv("STRIPE_TEST_API_KEY", "sk_test_fake")
	c := findCheck(toolConfigChecks(), "Tool: stripe")
	if c == nil || !strings.Contains(c.Detail, "live + test keys") {
		t.Errorf("got %+v, want 'live + test keys'", c)
	}
}

func TestToolConfigChecks_SlackConfigured(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-fake")
	c := findCheck(toolConfigChecks(), "Tool: slack")
	if c == nil || c.Detail != "configured" {
		t.Errorf("got %+v, want 'configured'", c)
	}
}

func TestToolConfigChecks_NotionConfigured(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("NOTION_API_KEY", "secret_fake")
	c := findCheck(toolConfigChecks(), "Tool: notion")
	if c == nil || c.Detail != "configured" {
		t.Errorf("got %+v, want 'configured'", c)
	}
}

func TestToolConfigChecks_JiraNoHost(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("JIRA_BEARER", "tok") // bearer set but no host
	c := findCheck(toolConfigChecks(), "Tool: jira")
	if c == nil || !strings.Contains(c.Detail, "JIRA_HOST") {
		t.Errorf("got %+v, want host-missing message", c)
	}
}

func TestToolConfigChecks_JiraBearer(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("JIRA_HOST", "test.atlassian.net")
	t.Setenv("JIRA_BEARER", "tok")
	c := findCheck(toolConfigChecks(), "Tool: jira")
	if c == nil || !strings.Contains(c.Detail, "bearer auth") {
		t.Errorf("got %+v, want bearer auth", c)
	}
	if !strings.Contains(c.Detail, "test.atlassian.net") {
		t.Errorf("Detail should include host: %q", c.Detail)
	}
}

func TestToolConfigChecks_JiraBasic(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("JIRA_HOST", "test.atlassian.net")
	t.Setenv("JIRA_EMAIL", "a@b.c")
	t.Setenv("JIRA_API_TOKEN", "tok")
	c := findCheck(toolConfigChecks(), "Tool: jira")
	if c == nil || !strings.Contains(c.Detail, "basic auth") {
		t.Errorf("got %+v, want basic auth", c)
	}
}

func TestToolConfigChecks_JiraPartial(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("JIRA_HOST", "test.atlassian.net")
	t.Setenv("JIRA_EMAIL", "a@b.c") // token missing
	c := findCheck(toolConfigChecks(), "Tool: jira")
	if c == nil || !strings.Contains(c.Detail, "partially configured") {
		t.Errorf("got %+v, want partially-configured", c)
	}
}

func TestToolConfigChecks_RAGFullyConfigured(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("PAN_AGENT_RAG_EMBEDDER_URL", "http://x")
	t.Setenv("PAN_AGENT_RAG_EMBEDDER_MODEL", "bge-small")
	c := findCheck(toolConfigChecks(), "RAG embedder")
	if c == nil || !strings.Contains(c.Detail, "model=bge-small") {
		t.Errorf("got %+v, want configured w/ model name", c)
	}
}

func TestToolConfigChecks_RAGPartial(t *testing.T) {
	clearAllToolEnv(t)
	t.Setenv("PAN_AGENT_RAG_EMBEDDER_URL", "http://x") // model missing
	c := findCheck(toolConfigChecks(), "RAG embedder")
	if c == nil || !strings.Contains(c.Detail, "partially configured") {
		t.Errorf("got %+v, want partially-configured", c)
	}
}

func TestToolConfigChecks_AllPass(t *testing.T) {
	// Even with weird/wrong values, Pass should always be true —
	// tool checks are informational, not gating.
	clearAllToolEnv(t)
	t.Setenv("STRIPE_API_KEY", "weird")
	t.Setenv("SLACK_BOT_TOKEN", "weird")
	t.Setenv("NOTION_API_KEY", "weird")
	t.Setenv("JIRA_HOST", "weird")
	t.Setenv("JIRA_BEARER", "weird")
	checks := toolConfigChecks()
	for _, c := range checks {
		if !c.Pass {
			t.Errorf("%s: Pass=false (informational checks must always pass)", c.Label)
		}
	}
}
