package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is an OpenAI-compatible streaming chat client.
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	httpClient *http.Client
}

// NewClient constructs a Client with a default HTTP timeout.
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		httpClient: &http.Client{
			Timeout: 0, // no hard timeout; callers use context
		},
	}
}

// --------------------------------------------------------------------------
// Request / response shapes (internal)
// --------------------------------------------------------------------------

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

// SSE delta shapes

type sseResponse struct {
	ID      string      `json:"id"`
	Choices []sseChoice `json:"choices"`
	Usage   *Usage      `json:"usage,omitempty"`
}

type sseChoice struct {
	Index        int      `json:"index"`
	Delta        sseDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type sseDelta struct {
	Role      string          `json:"role"`
	Content   *string         `json:"content"`
	ToolCalls []sseToolCallDelta `json:"tool_calls,omitempty"`
}

type sseToolCallDelta struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function *sseToolFnDelta `json:"function,omitempty"`
}

type sseToolFnDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// modelsResponse is the /models list payload.
type modelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// --------------------------------------------------------------------------
// ChatStream
// --------------------------------------------------------------------------

// ChatStream opens a streaming chat completion request and returns a channel
// of StreamEvents. The channel is closed when the stream ends or ctx is
// cancelled. The caller must drain the channel to avoid goroutine leaks.
func (c *Client) ChatStream(ctx context.Context, messages []Message, tools []ToolDef) (<-chan StreamEvent, error) {
	body := chatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("llm: create request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: http do: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("llm: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamEvent, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		c.parseSSE(ctx, resp.Body, ch)
	}()

	return ch, nil
}

// parseSSE reads the SSE stream from r and sends StreamEvents on ch.
func (c *Client) parseSSE(ctx context.Context, r io.Reader, ch chan<- StreamEvent) {
	// accumulator maps tool-call index → in-progress ToolCall
	tcAcc := map[int]*ToolCall{}

	send := func(e StreamEvent) bool {
		select {
		case ch <- e:
			return true
		case <-ctx.Done():
			return false
		}
	}

	scanner := bufio.NewScanner(r)
	// Increase buffer for large tool-call argument chunks.
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var sessionID string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		line := scanner.Text()

		// SSE comment or empty line
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")

		if payload == "[DONE]" {
			// Flush any accumulated tool calls before signalling done.
			for _, tc := range tcAcc {
				if !send(StreamEvent{Type: "tool_call", ToolCall: tc}) {
					return
				}
			}
			send(StreamEvent{Type: "done", SessionID: sessionID})
			return
		}

		var chunk sseResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			send(StreamEvent{Type: "error", Error: fmt.Sprintf("llm: unmarshal chunk: %v", err)})
			return
		}

		if sessionID == "" {
			sessionID = chunk.ID
		}

		// Usage-only chunk (some providers send this last).
		if chunk.Usage != nil && len(chunk.Choices) == 0 {
			send(StreamEvent{Type: "usage", Usage: chunk.Usage})
			continue
		}

		for _, choice := range chunk.Choices {
			delta := choice.Delta

			// Text content delta
			if delta.Content != nil && *delta.Content != "" {
				if !send(StreamEvent{Type: "chunk", Content: *delta.Content}) {
					return
				}
			}

			// Tool-call deltas — accumulate across chunks
			for _, tcd := range delta.ToolCalls {
				idx := tcd.Index
				if _, ok := tcAcc[idx]; !ok {
					tcAcc[idx] = &ToolCall{Type: "function"}
				}
				tc := tcAcc[idx]

				if tcd.ID != "" {
					tc.ID = tcd.ID
				}
				if tcd.Type != "" {
					tc.Type = tcd.Type
				}
				if tcd.Function != nil {
					if tcd.Function.Name != "" {
						tc.Function.Name += tcd.Function.Name
					}
					if tcd.Function.Arguments != "" {
						tc.Function.Arguments += tcd.Function.Arguments
					}
				}
			}

			// Emit completed tool calls on finish
			if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
				for _, tc := range tcAcc {
					if !send(StreamEvent{Type: "tool_call", ToolCall: tc}) {
						return
					}
				}
				tcAcc = map[int]*ToolCall{}
			}

			// Usage embedded inside choices (some providers)
			if chunk.Usage != nil {
				send(StreamEvent{Type: "usage", Usage: chunk.Usage})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() == nil {
			send(StreamEvent{Type: "error", Error: fmt.Sprintf("llm: scanner: %v", err)})
		}
	}
}

// --------------------------------------------------------------------------
// ListModels
// --------------------------------------------------------------------------

// ListModels fetches the list of available models from the provider.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("llm: list models request: %w", err)
	}
	c.setHeaders(req)

	// Use a shorter timeout for model listing.
	httpC := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpC.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: list models http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("llm: list models status %d: %s", resp.StatusCode, string(body))
	}

	var result modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("llm: list models decode: %w", err)
	}
	return result.Data, nil
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
}
