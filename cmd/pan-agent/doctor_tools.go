package main

import (
	"fmt"
	"os"
	"strings"
)

// Phase 13 WS#13.D — doctor sub-section: tool config probes.
//
// Each new read-only tool needs at least one env var to function.
// The doctor surfaces the configuration state so the operator knows
// at a glance which tools are usable on this host. None of the
// checks fail — they're informational. A missing env var means the
// tool returns a clear error at runtime; the doctor's job is to
// surface that BEFORE the user tries to use the tool.

// toolConfigChecks returns one doctorCheck per tool that ships
// with env-var configuration. Pass is always true (the absence of
// a tool's env var is documented as "optional"); the Detail string
// carries the actual state.
func toolConfigChecks() []doctorCheck {
	var out []doctorCheck

	// Stripe — STRIPE_API_KEY (live) or STRIPE_TEST_API_KEY (test).
	{
		live := strings.TrimSpace(os.Getenv("STRIPE_API_KEY"))
		test := strings.TrimSpace(os.Getenv("STRIPE_TEST_API_KEY"))
		switch {
		case live != "" && test != "":
			out = append(out, doctorCheck{
				Label: "Tool: stripe", Pass: true,
				Detail: "configured (live + test keys)",
			})
		case live != "":
			out = append(out, doctorCheck{
				Label: "Tool: stripe", Pass: true,
				Detail: "configured (live only)",
			})
		case test != "":
			out = append(out, doctorCheck{
				Label: "Tool: stripe", Pass: true,
				Detail: "configured (test only — set STRIPE_API_KEY for live mode)",
			})
		default:
			out = append(out, doctorCheck{
				Label: "Tool: stripe", Pass: true,
				Detail: "not configured (set STRIPE_API_KEY to enable)",
			})
		}
	}

	// Slack — SLACK_BOT_TOKEN.
	{
		tok := strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN"))
		if tok != "" {
			out = append(out, doctorCheck{
				Label: "Tool: slack", Pass: true, Detail: "configured",
			})
		} else {
			out = append(out, doctorCheck{
				Label: "Tool: slack", Pass: true,
				Detail: "not configured (set SLACK_BOT_TOKEN to enable)",
			})
		}
	}

	// Notion — NOTION_API_KEY.
	{
		tok := strings.TrimSpace(os.Getenv("NOTION_API_KEY"))
		if tok != "" {
			out = append(out, doctorCheck{
				Label: "Tool: notion", Pass: true, Detail: "configured",
			})
		} else {
			out = append(out, doctorCheck{
				Label: "Tool: notion", Pass: true,
				Detail: "not configured (set NOTION_API_KEY to enable)",
			})
		}
	}

	// Jira — JIRA_HOST + (JIRA_BEARER OR JIRA_EMAIL+JIRA_API_TOKEN).
	{
		host := strings.TrimSpace(os.Getenv("JIRA_HOST"))
		bearer := strings.TrimSpace(os.Getenv("JIRA_BEARER"))
		email := strings.TrimSpace(os.Getenv("JIRA_EMAIL"))
		tok := strings.TrimSpace(os.Getenv("JIRA_API_TOKEN"))
		switch {
		case host == "":
			out = append(out, doctorCheck{
				Label: "Tool: jira", Pass: true,
				Detail: "not configured (set JIRA_HOST + auth to enable)",
			})
		case bearer != "":
			out = append(out, doctorCheck{
				Label: "Tool: jira", Pass: true,
				Detail: fmt.Sprintf("configured (host=%s, bearer auth)", host),
			})
		case email != "" && tok != "":
			out = append(out, doctorCheck{
				Label: "Tool: jira", Pass: true,
				Detail: fmt.Sprintf("configured (host=%s, basic auth)", host),
			})
		default:
			out = append(out, doctorCheck{
				Label: "Tool: jira", Pass: true,
				Detail: fmt.Sprintf("partially configured (host=%s — missing JIRA_BEARER or JIRA_EMAIL+JIRA_API_TOKEN)", host),
			})
		}
	}

	// RAG embedder (Phase 13 WS#13.B) — surfaces the same env-var
	// pair the gateway lifecycle reads on startup. Reported here so
	// `doctor` users see whether semantic-recall context will be
	// available when they start the gateway.
	{
		url := strings.TrimSpace(os.Getenv("PAN_AGENT_RAG_EMBEDDER_URL"))
		model := strings.TrimSpace(os.Getenv("PAN_AGENT_RAG_EMBEDDER_MODEL"))
		switch {
		case url != "" && model != "":
			out = append(out, doctorCheck{
				Label: "RAG embedder", Pass: true,
				Detail: fmt.Sprintf("configured (model=%s)", model),
			})
		case url != "" || model != "":
			out = append(out, doctorCheck{
				Label: "RAG embedder", Pass: true,
				Detail: "partially configured (set BOTH PAN_AGENT_RAG_EMBEDDER_URL + _MODEL)",
			})
		default:
			out = append(out, doctorCheck{
				Label: "RAG embedder", Pass: true,
				Detail: "not configured (chat works; semantic recall disabled)",
			})
		}
	}

	return out
}
