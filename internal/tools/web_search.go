package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

func init() {
	Register(&webSearchTool{})
}

// webSearchTool implements Tool for web search via Tavily, with an HTTP+HTML
// fallback when no API key is present.
type webSearchTool struct{}

func (w *webSearchTool) Name() string { return "web_search" }

func (w *webSearchTool) Description() string {
	return "Search the web and return a list of results (title, URL, snippet). " +
		"Uses the Tavily Search API when TAVILY_API_KEY is set, otherwise falls " +
		"back to a plain HTTP fetch with HTML text extraction."
}

func (w *webSearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "The search query."
			},
			"max_results": {
				"type": "integer",
				"description": "Maximum number of results to return (default 5).",
				"default": 5
			}
		},
		"required": ["query"]
	}`)
}

// webSearchParams mirrors the JSON parameters accepted by this tool.
type webSearchParams struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

// searchResult is a single item returned to callers.
type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func (w *webSearchTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p webSearchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}
	if strings.TrimSpace(p.Query) == "" {
		return &Result{Error: "query must not be empty"}, nil
	}
	if p.MaxResults <= 0 {
		p.MaxResults = 5
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var results []searchResult
	var execErr error

	if key := os.Getenv("TAVILY_API_KEY"); key != "" {
		results, execErr = tavilySearch(ctx, key, p.Query, p.MaxResults)
	} else {
		results, execErr = fallbackSearch(ctx, p.Query, p.MaxResults)
	}

	if execErr != nil {
		return &Result{Error: execErr.Error()}, nil
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return &Result{Error: "failed to encode results: " + err.Error()}, nil
	}
	return &Result{Output: string(out)}, nil
}

// ---------------------------------------------------------------------------
// Tavily path
// ---------------------------------------------------------------------------

type tavilyRequest struct {
	APIKey      string `json:"api_key"`
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth"`
}

type tavilyResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func tavilySearch(ctx context.Context, apiKey, query string, maxResults int) ([]searchResult, error) {
	body, err := json.Marshal(tavilyRequest{
		APIKey:      apiKey,
		Query:       query,
		MaxResults:  maxResults,
		SearchDepth: "basic",
	})
	if err != nil {
		return nil, fmt.Errorf("tavily: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.tavily.com/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("tavily: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var tr tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("tavily: decode response: %w", err)
	}

	out := make([]searchResult, 0, len(tr.Results))
	for _, r := range tr.Results {
		out = append(out, searchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: truncate(r.Content, 300),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Fallback path: plain HTTP GET + HTML extraction
// ---------------------------------------------------------------------------

var (
	reTag       = regexp.MustCompile(`<[^>]+>`)
	reSpaces    = regexp.MustCompile(`\s{2,}`)
	reHTTPLinks = regexp.MustCompile(`https?://[^\s"'<>]+`)
	reTitleTag  = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
)

func fallbackSearch(ctx context.Context, query string, maxResults int) ([]searchResult, error) {
	// Use DuckDuckGo HTML endpoint as a best-effort fallback.
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fallback: build request: %w", err)
	}
	req.Header.Set("User-Agent", "pan-agent/1.0 (web_search fallback)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fallback: request failed: %w", err)
	}
	defer resp.Body.Close()

	rawHTML, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024)) // 512 KB cap
	if err != nil {
		return nil, fmt.Errorf("fallback: read body: %w", err)
	}

	return extractResults(string(rawHTML), query, maxResults), nil
}

// extractResults parses DuckDuckGo HTML results into searchResult entries.
// It looks for <a class="result__a"> anchors which contain the title and href.
func extractResults(html, query string, maxResults int) []searchResult {
	// Pattern for DDG result links:  <a rel="nofollow" class="result__a" href="...">Title</a>
	reResult := regexp.MustCompile(`(?i)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	// Snippet pattern: <a class="result__snippet" ...>text</a>
	reSnippet := regexp.MustCompile(`(?i)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)

	anchors := reResult.FindAllStringSubmatch(html, maxResults*2)
	snippets := reSnippet.FindAllStringSubmatch(html, maxResults*2)

	var results []searchResult
	for i, m := range anchors {
		if len(results) >= maxResults {
			break
		}
		rawURL := m[1]
		title := cleanHTML(m[2])

		// DDG wraps the real URL in a redirect; extract the uddg= parameter when present.
		if parsed, err := url.Parse(rawURL); err == nil {
			if real := parsed.Query().Get("uddg"); real != "" {
				if dec, err := url.QueryUnescape(real); err == nil {
					rawURL = dec
				}
			}
		}

		snippet := ""
		if i < len(snippets) && len(snippets[i]) > 1 {
			snippet = truncate(cleanHTML(snippets[i][1]), 300)
		}

		results = append(results, searchResult{
			Title:   title,
			URL:     rawURL,
			Snippet: snippet,
		})
	}

	// If no structured results were found, fall back to extracting bare URLs.
	if len(results) == 0 {
		links := reHTTPLinks.FindAllString(html, maxResults*3)
		seen := map[string]bool{}
		for _, l := range links {
			if len(results) >= maxResults {
				break
			}
			if seen[l] {
				continue
			}
			seen[l] = true
			results = append(results, searchResult{
				Title:   l,
				URL:     l,
				Snippet: "",
			})
		}
	}

	return results
}

// cleanHTML strips HTML tags and collapses whitespace.
func cleanHTML(s string) string {
	s = reTag.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// truncate cuts s to at most n runes, appending "…" when trimmed.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
