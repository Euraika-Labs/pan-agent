package claw3d

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/google/uuid"
)

// chat.* streams model output via WebSocket "chat" events (delta/final/error/
// aborted states). Under the hood it reuses pan-agent's existing
// internal/llm.ChatStream channel source; we simply wrap each event in the
// Claw3D envelope.
func init() {
	registerMethod("chat.send", handleChatSend)
	registerMethod("chat.abort", handleChatAbort)
	registerMethod("chat.history", handleChatHistory)
	registerMethod("agent.wait", handleAgentWait)
}

// activeRuns is an in-memory index of cancellable in-flight chat runs, keyed
// by runID so chat.abort can cancel the right context. It dies with the
// process; that is the documented semantic at Gate 2.
var activeRuns sync.Map // runID → context.CancelFunc

type chatSendParams struct {
	SessionID string        `json:"sessionId"`
	AgentID   string        `json:"agentId,omitempty"`
	Message   string        `json:"message"`
	History   []llm.Message `json:"history,omitempty"`
	Model     string        `json:"model,omitempty"`
}

// chatEventPayload is the stable shape emitted on the "chat" event stream.
// Clients key animation/speech-bubble state off `state` + `runId`.
type chatEventPayload struct {
	RunID   string `json:"runId"`
	State   string `json:"state"` // "delta" | "final" | "error" | "aborted"
	Delta   string `json:"delta,omitempty"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

func handleChatSend(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p chatSendParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Message == "" {
		return nil, errors.New("chat.send: message required")
	}
	if p.SessionID == "" {
		p.SessionID = uuid.NewString()
	}

	// Resolve provider + API key the same way cmdChat does. This keeps the
	// secret server-side — the browser never sees an API key.
	profile := "default"
	mc := config.GetModelConfig(profile)
	model := p.Model
	if model == "" {
		model = mc.Model
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	env, _ := config.ReadProfileEnv(profile)
	apiKey := firstNonEmpty(env["REGOLO_API_KEY"], env["OPENAI_API_KEY"], env["API_KEY"])
	baseURL := mc.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// Persist the user turn immediately so chat.history sees it even if the
	// model stream fails halfway.
	_, _ = db.AppendOfficeMessage(storage.OfficeMessage{
		SessionID: p.SessionID, Role: "user", Content: p.Message,
		CreatedAt: time.Now().UnixMilli(),
	})

	runID := uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	activeRuns.Store(runID, cancel)

	history := append([]llm.Message{}, p.History...)
	history = append(history, llm.Message{Role: "user", Content: p.Message})

	go func() {
		defer activeRuns.Delete(runID)
		defer cancel()

		client := llm.NewClient(baseURL, apiKey, model)
		stream, err := client.ChatStream(ctx, history, nil)
		if err != nil {
			c.send(marshalEventFrame("chat", chatEventPayload{
				RunID: runID, State: "error", Error: err.Error(),
			}))
			return
		}
		var acc string
		for ev := range stream {
			switch ev.Type {
			case "chunk":
				acc += ev.Content
				c.send(marshalEventFrame("chat", chatEventPayload{
					RunID: runID, State: "delta", Delta: ev.Content,
				}))
			case "error":
				c.send(marshalEventFrame("chat", chatEventPayload{
					RunID: runID, State: "error", Error: ev.Error,
				}))
				return
			case "done":
				_, _ = db.AppendOfficeMessage(storage.OfficeMessage{
					SessionID: p.SessionID, Role: "assistant",
					Content: acc, CreatedAt: time.Now().UnixMilli(),
				})
				c.send(marshalEventFrame("chat", chatEventPayload{
					RunID: runID, State: "final", Content: acc,
				}))
				return
			}
		}
	}()

	return map[string]string{"runId": runID, "sessionId": p.SessionID}, nil
}

type chatAbortParams struct {
	RunID string `json:"runId"`
}

func handleChatAbort(_ context.Context, _ *adapterClient, raw json.RawMessage) (any, error) {
	var p chatAbortParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.RunID == "" {
		return nil, errors.New("chat.abort: runId required")
	}
	if v, ok := activeRuns.LoadAndDelete(p.RunID); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
	return map[string]bool{"aborted": true}, nil
}

type chatHistoryParams struct {
	SessionID string `json:"sessionId"`
	Limit     int    `json:"limit,omitempty"`
}

func handleChatHistory(_ context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	db, err := requireDB(c)
	if err != nil {
		return nil, err
	}
	var p chatHistoryParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.SessionID == "" {
		return nil, errors.New("chat.history: sessionId required")
	}
	msgs, err := db.ListOfficeMessages(p.SessionID, p.Limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sessionId": p.SessionID, "messages": msgs}, nil
}

type agentWaitParams struct {
	RunID     string `json:"runId"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
}

// agent.wait blocks up to timeoutMs (max 30s) for a run to complete. In the
// Node adapter this was backed by conversationHistory polling; here we just
// check activeRuns — absent = finished.
func handleAgentWait(ctx context.Context, _ *adapterClient, raw json.RawMessage) (any, error) {
	var p agentWaitParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.RunID == "" {
		return nil, errors.New("agent.wait: runId required")
	}
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, ok := activeRuns.Load(p.RunID); !ok {
			return map[string]bool{"done": true}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return map[string]bool{"done": false}, nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
