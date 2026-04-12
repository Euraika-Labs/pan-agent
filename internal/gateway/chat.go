package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/euraika-labs/pan-agent/internal/approval"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/persona"
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
	client := s.llmClient
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

	// ------------------------------------------- persona system prompt
	systemPrompt, err := persona.Read(s.profile)
	if err != nil {
		systemPrompt = ""
	}

	// Build the working message slice: system + history.
	msgs := buildMessages(systemPrompt, req.Messages)

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
			_ = s.db.AddMessage(sessionID, "assistant", assistantContent)
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

	// ----------------------------------------------------------- persist user messages
	// Persist original user messages (they were not stored above).
	for _, m := range req.Messages {
		if m.Role == "user" {
			_ = s.db.AddMessage(sessionID, "user", m.Content)
		}
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
	"shell_exec":      true,
	"file_write":      true,
	"file_delete":     true,
	"process_kill":    true,
	"network_request": true,
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
// TODO: wire up internal/tools and internal/skills once those packages are written.
func (s *Server) dispatchTool(ctx context.Context, tc llm.ToolCall) string {
	// TODO: look up tc.Function.Name in the registered tool registry and call it.
	// For now, return a placeholder so the agent loop continues gracefully.
	_ = ctx
	return fmt.Sprintf(
		`{"error": "tool %q is not yet implemented in pan-agent Go rewrite"}`,
		tc.Function.Name,
	)
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
