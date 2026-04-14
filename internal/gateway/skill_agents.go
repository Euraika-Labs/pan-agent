package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/skills"
	embedSkills "github.com/euraika-labs/pan-agent/internal/skills/embed"
	"github.com/euraika-labs/pan-agent/internal/tools"
)

// SkillAgentReport summarises one reviewer or curator run.
type SkillAgentReport struct {
	Agent      string `json:"agent"` // "reviewer" | "curator"
	Profile    string `json:"profile"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
	Turns      int    `json:"turns"`
	ToolCalls  int    `json:"tool_calls"`
	FinalReply string `json:"final_reply,omitempty"`
	Error      string `json:"error,omitempty"`
}

// runReviewerAgent drives one full reviewer cycle. It reads the proposal
// queue, hands it to the LLM with the reviewer persona + skill_review tool,
// and lets the model approve/reject/merge each entry.
//
// This is intentionally synchronous and bounded so it can be triggered from an
// HTTP endpoint or a cron job without spawning long-lived goroutines.
func (s *Server) runReviewerAgent(ctx context.Context, profile string) (SkillAgentReport, error) {
	rep := SkillAgentReport{
		Agent:     "reviewer",
		Profile:   profile,
		StartedAt: time.Now().UnixMilli(),
	}

	client := s.getLLMClient()
	if client == nil {
		rep.Error = "no LLM client configured"
		rep.FinishedAt = time.Now().UnixMilli()
		return rep, fmt.Errorf("%s", rep.Error)
	}

	mgr := skills.NewManager(profile)

	// If the queue is empty, short-circuit. Saves an LLM round-trip for the
	// common no-op cron tick.
	props, err := mgr.ListProposals()
	if err != nil {
		rep.Error = err.Error()
		rep.FinishedAt = time.Now().UnixMilli()
		return rep, err
	}
	if len(props) == 0 {
		rep.FinalReply = "no proposals queued"
		rep.FinishedAt = time.Now().UnixMilli()
		return rep, nil
	}

	// Render the inventory + proposal queue into the user message.
	inventory := renderActiveInventory(profile)
	queueSummary := renderProposalQueue(props)

	userMsg := fmt.Sprintf(
		"# Active skill inventory\n\n%s\n\n# Proposal queue (%d)\n\n%s\n\n"+
			"Process every proposal using the `skill_review` tool. "+
			"When done, reply with a single-line summary.",
		inventory, len(props), queueSummary,
	)

	tool := tools.SkillReviewTool{Profile: profile}
	toolDef := llm.ToolDef{
		Type: "function",
		Function: llm.ToolFnDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		},
	}

	rep = runSkillAgentLoop(ctx, client, embedSkills.ReviewerMD, userMsg, []llm.ToolDef{toolDef},
		func(ctx context.Context, tc llm.ToolCall) string {
			if tc.Function.Name != tool.Name() {
				return fmt.Sprintf(`{"error": "reviewer can only call %s, got %s"}`, tool.Name(), tc.Function.Name)
			}
			res, err := tool.Execute(ctx, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				return fmt.Sprintf(`{"error": %q}`, err.Error())
			}
			if res.Error != "" {
				return fmt.Sprintf(`{"error": %q}`, res.Error)
			}
			return res.Output
		},
		rep,
	)
	rep.FinishedAt = time.Now().UnixMilli()
	return rep, nil
}

// runCuratorAgent drives one full curator cycle.
func (s *Server) runCuratorAgent(ctx context.Context, profile string) (SkillAgentReport, error) {
	rep := SkillAgentReport{
		Agent:     "curator",
		Profile:   profile,
		StartedAt: time.Now().UnixMilli(),
	}

	client := s.getLLMClient()
	if client == nil {
		rep.Error = "no LLM client configured"
		rep.FinishedAt = time.Now().UnixMilli()
		return rep, fmt.Errorf("%s", rep.Error)
	}

	tool := tools.SkillCuratorTool{Profile: profile, DB: s.db}
	toolDef := llm.ToolDef{
		Type: "function",
		Function: llm.ToolFnDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		},
	}

	// The curator decides for itself which skills to inspect; we just hand it
	// the inventory and let it call list_active_with_usage to see fresh
	// numbers.
	inventory := renderActiveInventory(profile)
	userMsg := fmt.Sprintf(
		"# Active skill inventory (snapshot)\n\n%s\n\n"+
			"Use `skill_curator` with action=list_active_with_usage to see "+
			"current usage stats, then propose any refinements/merges/splits/"+
			"archives/recategorisations the data justifies. When done, reply "+
			"with a single-line summary.",
		inventory,
	)

	rep = runSkillAgentLoop(ctx, client, embedSkills.CuratorMD, userMsg, []llm.ToolDef{toolDef},
		func(ctx context.Context, tc llm.ToolCall) string {
			log.Printf("[curator] → %s(%s)", tc.Function.Name, truncate(tc.Function.Arguments, 180))
			if tc.Function.Name != tool.Name() {
				return fmt.Sprintf(`{"error": "curator can only call %s, got %s"}`, tool.Name(), tc.Function.Name)
			}
			res, err := tool.Execute(ctx, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				log.Printf("[curator]   ← err: %v", err)
				return fmt.Sprintf(`{"error": %q}`, err.Error())
			}
			if res.Error != "" {
				log.Printf("[curator]   ← tool err: %s", res.Error)
				return fmt.Sprintf(`{"error": %q}`, res.Error)
			}
			log.Printf("[curator]   ← ok: %s", truncate(res.Output, 180))
			return res.Output
		},
		rep,
	)
	rep.FinishedAt = time.Now().UnixMilli()
	return rep, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// runSkillAgentLoop is the shared LLM agent loop used by both reviewer and
// curator. It runs up to maxAgentTurns iterations and dispatches tool calls
// through dispatch. The loop terminates when the model stops emitting
// tool calls (final reply) or hits the turn cap.
const maxAgentTurns = 10

func runSkillAgentLoop(
	ctx context.Context,
	client *llm.Client,
	personaMD string,
	userMsg string,
	toolDefs []llm.ToolDef,
	dispatch func(context.Context, llm.ToolCall) string,
	rep SkillAgentReport,
) SkillAgentReport {
	msgs := []llm.Message{
		{Role: "system", Content: personaMD},
		{Role: "user", Content: userMsg},
	}

	for turn := 0; turn < maxAgentTurns; turn++ {
		rep.Turns = turn + 1

		ch, err := client.ChatStream(ctx, msgs, toolDefs)
		if err != nil {
			rep.Error = "LLM error: " + err.Error()
			return rep
		}

		var (
			assistantContent string
			toolCalls        []llm.ToolCall
		)
		for ev := range ch {
			switch ev.Type {
			case "chunk":
				assistantContent += ev.Content
			case "tool_call":
				if ev.ToolCall != nil {
					toolCalls = append(toolCalls, *ev.ToolCall)
				}
			case "error":
				rep.Error = "LLM stream error: " + ev.Error
				return rep
			}
		}
		if ctx.Err() != nil {
			rep.Error = "context cancelled"
			return rep
		}

		msgs = append(msgs, llm.Message{
			Role:      "assistant",
			Content:   assistantContent,
			ToolCalls: toolCalls,
		})

		if len(toolCalls) == 0 {
			rep.FinalReply = strings.TrimSpace(assistantContent)
			return rep
		}

		for _, tc := range toolCalls {
			rep.ToolCalls++
			result := dispatch(ctx, tc)
			msgs = append(msgs, llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
		}
	}
	rep.Error = fmt.Sprintf("hit %d-turn cap without termination", maxAgentTurns)
	return rep
}

// renderActiveInventory returns a markdown bullet list of every active
// installed skill, plus its description. Used as the snapshot in the system
// prompt for both agents.
func renderActiveInventory(profile string) string {
	installed, err := skills.ListInstalled(profile)
	if err != nil || len(installed) == 0 {
		return "_(no active skills)_"
	}
	var b strings.Builder
	for _, s := range installed {
		fmt.Fprintf(&b, "- `%s/%s` — %s\n", s.Category, s.Name, s.Description)
	}
	return b.String()
}

// renderProposalQueue returns a markdown rendering of the queue. Each entry
// includes the metadata the reviewer needs at a glance — including curator
// intent so the reviewer knows when a proposal is more than a "create".
func renderProposalQueue(props []skills.Proposal) string {
	var b strings.Builder
	for _, p := range props {
		fmt.Fprintf(&b, "### %s — `%s/%s`\n", p.Metadata.ID, p.Metadata.Category, p.Metadata.Name)
		fmt.Fprintf(&b, "- source: `%s`\n", p.Metadata.Source)
		if p.Metadata.Intent != "" {
			fmt.Fprintf(&b, "- intent: `%s`\n", p.Metadata.Intent)
			if len(p.Metadata.IntentTargets) > 0 {
				fmt.Fprintf(&b, "- intent_targets: %v\n", p.Metadata.IntentTargets)
			}
			if p.Metadata.IntentNewCategory != "" {
				fmt.Fprintf(&b, "- intent_new_category: `%s`\n", p.Metadata.IntentNewCategory)
			}
			if p.Metadata.IntentReason != "" {
				fmt.Fprintf(&b, "- intent_reason: %s\n", p.Metadata.IntentReason)
			}
		}
		fmt.Fprintf(&b, "- description: %s\n", p.Metadata.Description)
		if p.GuardResult.Blocked {
			fmt.Fprintf(&b, "- **guard blocked** (%d findings)\n", len(p.GuardResult.Findings))
		} else if len(p.GuardResult.Findings) > 0 {
			fmt.Fprintf(&b, "- guard warnings: %d\n", len(p.GuardResult.Findings))
		}
		b.WriteString("\n")
	}
	return b.String()
}
