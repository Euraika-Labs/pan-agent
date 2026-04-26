package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Embedder turns text into a fixed-dimensionality float32 vector. The
// next-slice index wrapper consumes Embedder; tests inject a fake so
// no HTTP server is required for unit-level coverage.
//
// Embed returns the vector + the dimensionality + the model id the
// provider actually used (for the rare case where a request for
// model "X" gets back model "X-v2"; we record what was actually
// computed so re-index detection is accurate).
//
// EmbedBatch is the bulk variant — providers commonly accept an
// array `input` for a single round-trip; the order of vectors in
// the returned slice matches the input order. Implementations MUST
// either return all vectors or an error; partial results are
// rejected at the boundary so callers don't have to track which
// inputs succeeded.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dim() int
}

// ErrEmbedFailed wraps non-network failures from the embedder
// (provider returned a structured error, decoded an empty data
// array, dim disagreed with a previous call). errors.Is supports
// matching either the wrapper or the underlying cause.
var ErrEmbedFailed = errors.New("rag: embed failed")

// HTTPEmbedderConfig is the constructor input for an OpenAI-shaped
// embeddings client. Most providers (OpenAI, Regolo, Ollama,
// LM Studio, vLLM, llama.cpp) implement the same /embeddings shape.
type HTTPEmbedderConfig struct {
	// BaseURL is the provider root, e.g. "https://api.regolo.ai/v1".
	// Trailing slashes are trimmed. Required.
	BaseURL string
	// APIKey is the bearer token. Empty is OK for local providers
	// (Ollama, LM Studio, vLLM, llama.cpp).
	APIKey string
	// Model is the embedding model id, e.g.
	// "regolo:bge-small-en-v1.5". Required — the rag_embeddings.model
	// column is keyed off this string, so changing it triggers a
	// re-embed pass for affected rows.
	Model string
	// Dim is the expected output dimensionality. Required for
	// pre-flight validation: providers that silently return a
	// different dim from a model upgrade would otherwise corrupt
	// the rag_embeddings.dim column. Set to 0 to skip validation
	// (only useful for tests that don't care).
	Dim int
	// Timeout caps a single HTTP round-trip. 0 = no timeout
	// (honour the caller-supplied context only). Defaults to 30s
	// when constructed via NewHTTPEmbedder.
	Timeout time.Duration
	// HTTPClient lets tests inject a custom client (e.g. an
	// httptest server's RoundTripper). nil = a fresh net/http
	// Client with Timeout.
	HTTPClient *http.Client
}

// HTTPEmbedder implements Embedder against an OpenAI-compatible
// /v1/embeddings endpoint.
type HTTPEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	dim     int
	client  *http.Client
}

// NewHTTPEmbedder validates cfg and returns an Embedder. Returns an
// error when the configuration is unusable so callers don't carry a
// half-broken client around.
func NewHTTPEmbedder(cfg HTTPEmbedderConfig) (*HTTPEmbedder, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("rag: HTTPEmbedder: BaseURL required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("rag: HTTPEmbedder: Model required")
	}
	if cfg.Dim < 0 {
		return nil, fmt.Errorf("rag: HTTPEmbedder: Dim must be >= 0, got %d", cfg.Dim)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		t := cfg.Timeout
		if t == 0 {
			t = 30 * time.Second
		}
		hc = &http.Client{Timeout: t}
	}
	return &HTTPEmbedder{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		dim:     cfg.Dim,
		client:  hc,
	}, nil
}

// Model returns the configured embedding model id.
func (e *HTTPEmbedder) Model() string { return e.model }

// Dim returns the configured output dimensionality. Zero means
// "validation skipped" — the index writer should fall back to the
// length of the first returned vector in that case.
func (e *HTTPEmbedder) Dim() int { return e.dim }

// Embed sends a single-input embedding request. Returns
// ErrEmbedFailed-wrapped errors for provider-side problems; the
// raw context-cancellation / network errors pass through unwrapped
// so callers can distinguish "retry later" from "fix your config".
func (e *HTTPEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	out, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(out) != 1 {
		return nil, fmt.Errorf("%w: expected 1 vector, got %d", ErrEmbedFailed, len(out))
	}
	return out[0], nil
}

// embedRequest mirrors the OpenAI /embeddings request shape.
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse mirrors the OpenAI /embeddings response shape.
type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// EmbedBatch sends multi-input embedding request. Re-orders the
// returned vectors by data[i].index so callers see input-aligned
// output regardless of provider re-ordering.
func (e *HTTPEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("%w: empty input batch", ErrEmbedFailed)
	}

	body, err := json.Marshal(embedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("rag: marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rag: build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rag: embed http: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("rag: read embed body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d: %s",
			ErrEmbedFailed, resp.StatusCode, truncateForLog(string(rawBody)))
	}

	var out embedResponse
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return nil, fmt.Errorf("%w: decode body: %v", ErrEmbedFailed, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%w: provider error: %s",
			ErrEmbedFailed, out.Error.Message)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("%w: returned %d vectors for %d inputs",
			ErrEmbedFailed, len(out.Data), len(texts))
	}

	// Order by index — provider may return them in arrival order.
	vectors := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return nil, fmt.Errorf("%w: out-of-range index %d (batch size %d)",
				ErrEmbedFailed, d.Index, len(texts))
		}
		if vectors[d.Index] != nil {
			return nil, fmt.Errorf("%w: duplicate index %d in response",
				ErrEmbedFailed, d.Index)
		}
		if e.dim > 0 && len(d.Embedding) != e.dim {
			return nil, fmt.Errorf("%w: embedding[%d] dim = %d, configured Dim = %d",
				ErrEmbedFailed, d.Index, len(d.Embedding), e.dim)
		}
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("%w: embedding[%d] is empty",
				ErrEmbedFailed, d.Index)
		}
		vectors[d.Index] = d.Embedding
	}
	for i, v := range vectors {
		if v == nil {
			return nil, fmt.Errorf("%w: response missing index %d",
				ErrEmbedFailed, i)
		}
	}
	return vectors, nil
}

// truncateForLog caps an error body so a malformed-JSON page from a
// proxy doesn't dump a megabyte into the user's log. The full body
// is already capped at 16MB by the LimitReader; this is a second
// layer for error formatting only.
func truncateForLog(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
