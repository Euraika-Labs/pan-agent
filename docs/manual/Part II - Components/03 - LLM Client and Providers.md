# LLM Client and Providers

The `internal/llm` package implements an OpenAI-compatible streaming chat client used by all 9 supported providers.

## Supported providers

| Provider | Base URL | Key env var |
|---|---|---|
| OpenAI | `https://api.openai.com/v1` | `OPENAI_API_KEY` |
| Anthropic | `https://api.anthropic.com/v1` | `ANTHROPIC_API_KEY` |
| Regolo | `https://api.regolo.ai/v1` | `REGOLO_API_KEY` |
| OpenRouter | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| Groq | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` |
| Ollama | `http://localhost:11434/v1` | (none) |
| LM Studio | `http://localhost:1234/v1` | (none) |
| vLLM | `http://localhost:8000/v1` | (none) |
| llama.cpp | `http://localhost:8080/v1` | (none) |

All providers share the same client code. The base URL and API key are the only differences.

## Client API

```go
type Client struct {
    BaseURL string
    APIKey  string
    Model   string
    // ...
}

func NewClient(baseURL, apiKey, model string) *Client

func (c *Client) ChatStream(
    ctx context.Context,
    messages []Message,
    tools []ToolDef,
) (<-chan StreamEvent, error)

func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error)
```

`ChatStream` POSTs to `{BaseURL}/chat/completions` with `stream: true`. Returns a channel that streams `StreamEvent` values until the stream ends or `ctx` is cancelled.

## StreamEvent types

```go
type StreamEvent struct {
    Type string  // "chunk" | "tool_call" | "done" | "error" | "usage"

    Content   string     // chunk
    ToolCall  *ToolCall  // tool_call
    Usage     *Usage     // usage
    Error     string     // error
    SessionID string     // done
}
```

The SSE parser in `client.go:parseSSE` accumulates tool call arguments across chunks. The OpenAI streaming format sends tool calls in pieces (function name first, then arguments token-by-token); the parser maintains an accumulator keyed by tool call index.

## Message structure

```go
type Message struct {
    Role       string     // "system" | "user" | "assistant" | "tool"
    Content    string
    Name       string     // for tool messages: tool name
    ToolCallID string     // for tool messages: id of the call this responds to
    ToolCalls  []ToolCall // for assistant messages: tool calls the model wants made
}

type ToolCall struct {
    ID       string
    Type     string  // "function"
    Function FnCall
}

type FnCall struct {
    Name      string
    Arguments string  // JSON string
}
```

## ToolDef structure

The agent passes available tool definitions to the LLM:

```go
type ToolDef struct {
    Type     string    // "function"
    Function ToolFnDef
}

type ToolFnDef struct {
    Name        string
    Description string
    Parameters  json.RawMessage  // JSON Schema
}
```

`tools.All()` returns all registered tools. Each `Tool.Parameters()` returns the JSON Schema as `json.RawMessage`. The chat handler builds the `[]ToolDef` slice from the registry on each request.

## Operator rule
The Anthropic provider URL ends in `/v1` but Anthropic's native API uses different endpoints (`/v1/messages` vs OpenAI's `/v1/chat/completions`). When using Anthropic models, route through OpenRouter or Regolo (both expose Anthropic via the OpenAI-compatible format). Direct Anthropic API support would need a separate client.

## Read next
- [[04 - Tool Registry]]
- [[05 - Approval System]]
- [[02 - HTTP API Surface]]
