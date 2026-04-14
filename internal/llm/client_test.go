package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFakeSSEServer returns an httptest.Server that responds to POST
// /chat/completions with the given SSE body. Good enough for parser tests.
func newFakeSSEServer(t *testing.T, sseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sseBody))
	}))
}

// collectStream drains all StreamEvents out of the client's ChatStream into
// concrete slices for assertions.
func collectStream(t *testing.T, body string) (toolCalls []*ToolCall, chunks []string, done bool, errs []string) {
	t.Helper()
	srv := newFakeSSEServer(t, body)
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, "", "test-model")
	ch, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	for ev := range ch {
		switch ev.Type {
		case "tool_call":
			toolCalls = append(toolCalls, ev.ToolCall)
		case "chunk":
			chunks = append(chunks, ev.Content)
		case "done":
			done = true
		case "error":
			errs = append(errs, ev.Error)
		}
	}
	return
}

// TestSSERegoloGPTOSSBugForBugCompat is the regression test for a real bug
// caught in production against Regolo's gpt-oss-120b: the provider sends
// argument continuations under an *incremented* `index` instead of reusing
// the original call's index. Our parser must coalesce these back into one
// tool call. Payload captured from a live Regolo request.
func TestSSERegoloGPTOSSBugForBugCompat(t *testing.T) {
	body := `data: {"id":"x","created":1,"model":"gpt-oss-120b","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"}}]}

data: {"id":"x","created":1,"model":"gpt-oss-120b","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"id":"call-1","function":{"arguments":"","name":"test_tool"},"type":"function","index":0},{"function":{"arguments":"{\"action\":\"foo"},"type":"function","index":0}]}}]}

data: {"id":"x","created":1,"model":"gpt-oss-120b","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"\",\"name\":\"bar\"}"},"type":"function","index":1}]}}]}

data: {"id":"x","created":1,"model":"gpt-oss-120b","object":"chat.completion.chunk","choices":[{"finish_reason":"tool_calls","index":0,"delta":{}}]}

data: [DONE]

`
	toolCalls, _, _, errs := collectStream(t, body)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1 (Regolo gpt-oss-120b bug regressed)", len(toolCalls))
	}
	tc := toolCalls[0]
	if tc.Function.Name != "test_tool" {
		t.Errorf("function name = %q, want test_tool", tc.Function.Name)
	}
	expected := `{"action":"foo","name":"bar"}`
	got := strings.Join(strings.Fields(tc.Function.Arguments), "")
	if got != expected {
		t.Errorf("args = %q, want %q", tc.Function.Arguments, expected)
	}
	if tc.ID != "call-1" {
		t.Errorf("id = %q, want call-1", tc.ID)
	}
}

// TestSSEStandardSingleToolCall covers the well-behaved single-call case.
func TestSSEStandardSingleToolCall(t *testing.T) {
	body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"c1","index":0,"type":"function","function":{"name":"foo","arguments":""}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":1}"}}]}}]}

data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}

data: [DONE]

`
	toolCalls, _, _, _ := collectStream(t, body)
	if len(toolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(toolCalls))
	}
	if toolCalls[0].Function.Arguments != `{"x":1}` {
		t.Errorf("args = %q, want {\"x\":1}", toolCalls[0].Function.Arguments)
	}
}

// TestSSEParallelToolCalls covers legitimate multi-call streams (parallel
// tool calling): two calls, each with their own index, their own id+name.
// Our bug-for-bug compat MUST NOT coalesce these — they're distinct calls.
func TestSSEParallelToolCalls(t *testing.T) {
	body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"c1","index":0,"type":"function","function":{"name":"foo","arguments":"{\"a\":1}"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"c2","index":1,"type":"function","function":{"name":"bar","arguments":"{\"b\":2}"}}]}}]}

data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}

data: [DONE]

`
	toolCalls, _, _, _ := collectStream(t, body)
	if len(toolCalls) != 2 {
		t.Fatalf("got %d tool calls, want 2 (parallel calls must stay separate)", len(toolCalls))
	}
	names := map[string]string{toolCalls[0].Function.Name: toolCalls[0].Function.Arguments, toolCalls[1].Function.Name: toolCalls[1].Function.Arguments}
	if names["foo"] != `{"a":1}` || names["bar"] != `{"b":2}` {
		t.Errorf("unexpected parallel-call payloads: %+v", names)
	}
}

// TestSSETextContentStreaming — plain content-only stream should emit
// chunks and a done, no tool calls.
func TestSSETextContentStreaming(t *testing.T) {
	body := `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}

data: {"choices":[{"index":0,"delta":{"content":" world"}}]}

data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}]}

data: [DONE]

`
	toolCalls, chunks, done, _ := collectStream(t, body)
	if len(toolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(toolCalls))
	}
	if strings.Join(chunks, "") != "Hello world" {
		t.Errorf("chunks = %v, want [Hello, \" world\"]", chunks)
	}
	if !done {
		t.Error("expected done event")
	}
}
