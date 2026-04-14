package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/approval"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/persona"
	"github.com/euraika-labs/pan-agent/internal/skills"
	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/euraika-labs/pan-agent/internal/tools"
)

// =============================================================================
// Request / response types
// =============================================================================

// chatRequest is the JSON body accepted by POST /v1/chat/completions.
type chatRequest struct {
	// Messages is the conversation history. Required.
	Messages []llm.Message `json:"messages"`
	// Model overrides the server default when non-empty.
	Model string `json:"model,omitempty"`
	// Stream must be true; non-streaming is not supported.
	Stream bool `json:"stream"`
	// Tools is the set of function tools the model may call.
	Tools []llm.ToolDef `json:"tools,omitempty"`
	// SessionID resumes an existing session when provided.
	SessionID string `json:"session_id,omitempty"`
}

// sseEvent is a single server-sent event.
type sseEvent struct {
	// Type is one of: "chunk", "tool_call", "approval_required",
	// "tool_result", "usage", "error", "done".
	Type string `json:"type"`

	// chunk
	Content string `json:"content,omitempty"`

	// tool_call / approval_required
	ToolCall *llm.ToolCall `json:"tool_call,omitempty"`

	// approval_required
	ApprovalID string `json:"approval_id,omitempty"`

	// tool_result
	ToolCallID string `json:"tool_call_id,omitempty"`
	Result     string `json:"result,omitempty"`

	// usage
	Usage *llm.Usage `json:"usage,omitempty"`

	// done
	SessionID string `json:"session_id,omitempty"`

	// error
	Error string `json:"error,omitempty"`
}

// =============================================================================
// Abort registry
// =============================================================================

// abortRegistry maps sessionID → cancel function so that POST /v1/chat/abort
// can cancel an in-flight generation.
var abortRegistry struct {
	sync.Mutex
	m map[string]context.CancelFunc
}

func init() {
	abortRegistry.m = make(map[string]context.CancelFunc)
}

func registerAbort(sessionID string, cancel context.CancelFunc) {
	abortRegistry.Lock()
	abortRegistry.m[sessionID] = cancel
	abortRegistry.Unlock()
}

func unregisterAbort(sessionID string) {
	abortRegistry.Lock()
	delete(abortRegistry.m, sessionID)
	abortRegistry.Unlock()
}

// =============================================================================
// Handlers
// =============================================================================

// handleChatCompletions is the main agent loop endpoint.
//
// Flow:
//  1. Parse request.
//  2. Create / resume storage session.
//  3. Prepend the persona system prompt.
//  4. Loop: call LLM → stream chunks → on tool_call: execute tool (with
//     optional approval gate) → append tool result → repeat.
//  5. On finish: persist messages, emit "done" SSE.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// ---------------------------------------------------------------- parse
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages is required")
		return
	}

	// --------------------------------------------------------- SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush(w)

	// -------------------------------------------------------- LLM client
	client := s.getLLMClient()
	if req.Model != "" && client != nil {
		// Create a one-off client for the requested model using the same base URL.
		client = llm.NewClient(client.BaseURL, client.APIKey, req.Model)
	}
	if client == nil {
		sendSSE(w, sseEvent{Type: "error", Error: "no LLM client configured; set model via PUT /v1/config"})
		sendDone(w)
		return
	}

	// ------------------------------------------------------- session setup
	sessionID := req.SessionID
	if sessionID == "" {
		sess, err := s.db.CreateSession(client.Model)
		if err != nil {
			sendSSE(w, sseEvent{Type: "error", Error: "failed to create session: " + err.Error()})
			sendDone(w)
			return
		}
		sessionID = sess.ID
	}

	// ------------------------------------------ cancellable context
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	registerAbort(sessionID, cancel)
	defer unregisterAbort(sessionID)

	// -------------------------------------------------------- persist user messages
	// Store the user's messages before the agent loop so they are in the DB
	// even if the loop is aborted or the connection drops mid-generation.
	for _, m := range req.Messages {
		if m.Role == "user" {
			if err := s.db.AddMessage(sessionID, "user", m.Content); err != nil {
				log.Printf("[chat] AddMessage error: %v", err)
			}
		}
	}

	// ------------------------------------------- persona system prompt
	systemPrompt, err := persona.Read(s.profile)
	if err != nil {
		systemPrompt = ""
	}

	// Build the working message slice: system + skills inventory + history.
	// The skills inventory is injected as a *user* message (not in the system
	// prompt) to preserve the LLM provider's prompt cache — the system prompt
	// stays stable across requests, and the inventory message sits at a
	// predictable position regardless of how many turns have happened.
	msgs := buildMessagesWithSkills(systemPrompt, skillsInventoryMessage(s.profile), req.Messages)

	// ============================================================ agent loop
	const maxTurns = 20 // safety cap to prevent runaway loops

	for turn := 0; turn < maxTurns; turn++ {
		// --------------------------------------------------- call LLM
		ch, err := client.ChatStream(ctx, msgs, req.Tools)
		if err != nil {
			sendSSE(w, sseEvent{Type: "error", Error: "LLM error: " + err.Error()})
			sendDone(w)
			return
		}

		// Collect the assistant reply so we can append it to msgs.
		var (
			assistantContent string
			toolCalls        []llm.ToolCall
			gotDone          bool
		)

		for ev := range ch {
			switch ev.Type {

			case "chunk":
				assistantContent += ev.Content
				sendSSE(w, sseEvent{Type: "chunk", Content: ev.Content})

			case "tool_call":
				if ev.ToolCall != nil {
					toolCalls = append(toolCalls, *ev.ToolCall)
					sendSSE(w, sseEvent{Type: "tool_call", ToolCall: ev.ToolCall})
				}

			case "usage":
				sendSSE(w, sseEvent{Type: "usage", Usage: ev.Usage})

			case "done":
				gotDone = true

			case "error":
				sendSSE(w, sseEvent{Type: "error", Error: ev.Error})
				sendDone(w)
				return
			}
		}

		if ctx.Err() != nil {
			// Aborted by client.
			sendSSE(w, sseEvent{Type: "error", Error: "generation aborted"})
			sendDone(w)
			return
		}

		_ = gotDone // loop termination is driven by toolCalls being empty

		// Append the assistant turn to the working history.
		assistantMsg := llm.Message{
			Role:      "assistant",
			Content:   assistantContent,
			ToolCalls: toolCalls,
		}
		msgs = append(msgs, assistantMsg)

		// Persist the assistant message.
		if assistantContent != "" {
			if err := s.db.AddMessage(sessionID, "assistant", assistantContent); err != nil {
				log.Printf("[chat] AddMessage error: %v", err)
			}
		}

		// ------------------------------------------ no tool calls → done
		if len(toolCalls) == 0 {
			break
		}

		// ------------------------------------------ execute tool calls
		for _, tc := range toolCalls {
			result, executeErr := s.executeToolCall(ctx, w, sessionID, tc)
			if executeErr != nil {
				// executeToolCall already sent an SSE error when appropriate.
				sendDone(w)
				return
			}

			// Log skill-related tool usage so the curator agent has data to
			// work with. Best-effort: failure here must not break the chat.
			logSkillToolUsage(s.db, sessionID, tc, result)

			// Append tool result message so the model can see the outcome.
			toolResultMsg := llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			}
			msgs = append(msgs, toolResultMsg)

			sendSSE(w, sseEvent{
				Type:       "tool_result",
				ToolCallID: tc.ID,
				Result:     result,
			})
		}
		// Loop: re-call LLM with the tool results appended.
	}

	// ---------------------------------------------------------------- done
	sendSSE(w, sseEvent{Type: "done", SessionID: sessionID})
	sendDone(w)
}

// handleChatAbort cancels an in-flight generation for the given session.
// Body: {"session_id": "..."}
func (s *Server) handleChatAbort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	abortRegistry.Lock()
	cancel, ok := abortRegistry.m[body.SessionID]
	abortRegistry.Unlock()

	if !ok {
		writeError(w, http.StatusNotFound, "no active generation for session")
		return
	}
	cancel()
	writeJSON(w, http.StatusOK, map[string]string{"status": "aborted"})
}

// =============================================================================
// Tool execution
// =============================================================================

// dangerousTools is the set of tool names that require user approval before
// execution. This list should grow as more tools are added.
var dangerousTools = map[string]bool{
	"terminal":       true,
	"filesystem":     true,
	"code_execution": true,
	"browser":        true,
}

// executeToolCall dispatches a single tool call and returns the result string.
// If the tool is marked as dangerous, it first emits an approval_required SSE
// event and blocks until the user resolves the approval via the HTTP API.
//
// Returns an error only for fatal conditions (context cancelled, approval
// rejected). Tool execution errors are returned as a result string so the
// model can see what went wrong.
func (s *Server) executeToolCall(
	ctx context.Context,
	w http.ResponseWriter,
	sessionID string,
	tc llm.ToolCall,
) (string, error) {

	// -------------------------------------------------- approval gate
	if dangerousTools[tc.Function.Name] {
		appr := s.approvals.Create(sessionID, tc.Function.Name, tc.Function.Arguments)

		sendSSE(w, sseEvent{
			Type:       "approval_required",
			ToolCall:   &tc,
			ApprovalID: appr.ID,
		})
		flush(w)

		status, err := s.approvals.Wait(appr.ID, ctx.Done())
		if err != nil {
			return "", fmt.Errorf("tool approval cancelled: %w", err)
		}
		if status != approval.StatusApproved {
			return "", fmt.Errorf("tool call rejected by user")
		}
	}

	// -------------------------------------------------- dispatch
	result := s.dispatchTool(ctx, tc)
	return result, nil
}

// dispatchTool routes a tool call to the appropriate implementation.
func (s *Server) dispatchTool(ctx context.Context, tc llm.ToolCall) string {
	tool, ok := tools.Get(tc.Function.Name)
	if !ok {
		return fmt.Sprintf(`{"error": "unknown tool: %s"}`, tc.Function.Name)
	}
	result, err := tool.Execute(ctx, json.RawMessage(tc.Function.Arguments))
	if err != nil {
		return fmt.Sprintf(`{"error": "tool execution failed: %s"}`, err.Error())
	}
	if result.Error != "" {
		return fmt.Sprintf(`{"error": %q, "output": %q}`, result.Error, result.Output)
	}
	return result.Output
}

// =============================================================================
// SSE helpers
// =============================================================================

// sendSSE serialises ev as a data: line and writes it to w.
func sendSSE(w http.ResponseWriter, ev sseEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flush(w)
}

// sendDone writes the terminal SSE [DONE] marker.
func sendDone(w http.ResponseWriter) {
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flush(w)
}

// flush calls Flush on w if it implements http.Flusher.
func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// =============================================================================
// Helpers
// =============================================================================

// runAgentLoop runs the LLM agent loop and returns the final assistant response.
// This is the non-HTTP version used by messaging bots. Tool calls that require
// approval are auto-approved (bots have no interactive approval UI).
func (s *Server) runAgentLoop(ctx context.Context, sessionID string, userMessage string) (string, error) {
	client := s.getLLMClient()
	if client == nil {
		return "", fmt.Errorf("no LLM client configured")
	}

	// Persist the user message.
	_ = s.db.AddMessage(sessionID, "user", userMessage)

	// Build messages with persona.
	systemPrompt, _ := persona.Read(s.profile)
	msgs := buildMessages(systemPrompt, []llm.Message{
		{Role: "user", Content: userMessage},
	})

	const maxTurns = 20
	var finalContent string

	for turn := 0; turn < maxTurns; turn++ {
		ch, err := client.ChatStream(ctx, msgs, nil)
		if err != nil {
			return "", fmt.Errorf("LLM error: %w", err)
		}

		var assistantContent string
		var toolCalls []llm.ToolCall

		for ev := range ch {
			switch ev.Type {
			case "chunk":
				assistantContent += ev.Content
			case "tool_call":
				if ev.ToolCall != nil {
					toolCalls = append(toolCalls, *ev.ToolCall)
				}
			case "error":
				return "", fmt.Errorf("LLM error: %s", ev.Error)
			}
		}

		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		msgs = append(msgs, llm.Message{
			Role:      "assistant",
			Content:   assistantContent,
			ToolCalls: toolCalls,
		})

		if assistantContent != "" {
			finalContent = assistantContent
			_ = s.db.AddMessage(sessionID, "assistant", assistantContent)
		}

		if len(toolCalls) == 0 {
			break
		}

		// Execute tool calls (auto-approve for bots — skip approval gate).
		for _, tc := range toolCalls {
			result := s.dispatchTool(ctx, tc)
			msgs = append(msgs, llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
		}
	}

	return finalContent, nil
}

// buildMessages prepends a system prompt (if non-empty) to msgs.
// The original slice is not modified.
func buildMessages(systemPrompt string, msgs []llm.Message) []llm.Message {
	if systemPrompt == "" {
		return msgs
	}
	out := make([]llm.Message, 0, len(msgs)+1)
	out = append(out, llm.Message{Role: "system", Content: systemPrompt})
	out = append(out, msgs...)
	return out
}

// buildMessagesWithSkills prepends the system prompt and a skills inventory
// user message. The skills message comes BEFORE the conversation history so
// it sits at a stable cache boundary — providers cache the longest stable
// prefix, and inventory changes only when skills are installed/removed.
func buildMessagesWithSkills(systemPrompt, skillsMsg string, msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs)+2)
	if systemPrompt != "" {
		out = append(out, llm.Message{Role: "system", Content: systemPrompt})
	}
	if skillsMsg != "" {
		out = append(out, llm.Message{Role: "user", Content: skillsMsg})
	}
	out = append(out, msgs...)
	return out
}

// skillsInventoryMessage renders a compact inventory of installed skills as a
// single string suitable for use as the body of a user message. Returns ""
// when there are no skills, in which case no inventory message is injected.
func skillsInventoryMessage(profile string) string {
	installed, err := skills.ListInstalled(profile)
	if err != nil || len(installed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Available skills\n\n")
	b.WriteString("You have access to these skills via the `skill_view` tool. ")
	b.WriteString("Load a skill by passing its id (e.g. `coding/refactor`).\n\n")
	for _, s := range installed {
		fmt.Fprintf(&b, "- `%s/%s` — %s\n", s.Category, s.Name, s.Description)
	}
	return b.String()
}

// logSkillToolUsage records skill-related tool calls in the SkillUsage table.
// Best-effort: any failure is logged but never bubbled up. Recognised tools:
//
//   - skill_view   → records the loaded skill id (success/error inferred from result body)
//   - skill_manage → records the affected skill id when present
func logSkillToolUsage(db *storage.DB, sessionID string, tc llm.ToolCall, resultBody string) {
	if db == nil {
		return
	}
	skillID := extractSkillIDFromCall(tc)
	if skillID == "" {
		return
	}
	outcome := "success"
	if strings.Contains(resultBody, `"error"`) {
		outcome = "error"
	}
	_, err := db.LogSkillUsage(storage.SkillUsage{
		SessionID:   sessionID,
		SkillID:     skillID,
		UsedAt:      time.Now().UnixMilli(),
		Outcome:     outcome,
		ContextHint: tc.Function.Name,
	})
	if err != nil {
		log.Printf("[chat] LogSkillUsage error: %v", err)
	}
}

// extractSkillIDFromCall pulls the skill id from a tool call's arguments JSON.
// Both skill_view and skill_manage use the `name` parameter for the skill id
// when operating on existing skills (`<category>/<name>` form). For
// skill_manage(action="create"), the id is constructed from category + name.
func extractSkillIDFromCall(tc llm.ToolCall) string {
	if tc.Function.Name != "skill_view" && tc.Function.Name != "skill_manage" {
		return ""
	}
	var args struct {
		Name     string `json:"name"`
		Category string `json:"category"`
		Action   string `json:"action"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return ""
	}
	// "create" sends category + bare name; everything else sends "<cat>/<name>".
	if tc.Function.Name == "skill_manage" && args.Action == "create" {
		if args.Category == "" || args.Name == "" {
			return ""
		}
		if strings.Contains(args.Name, "/") {
			return args.Name
		}
		return args.Category + "/" + args.Name
	}
	if !strings.Contains(args.Name, "/") {
		return ""
	}
	return args.Name
}
