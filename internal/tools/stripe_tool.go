package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/saaslinks"
)

// Phase 13 WS#13.D — Stripe read-only tool.
//
// Surfaces two actions the agent can use to look up payment data:
//
//   list  — paginated list of recent charges (limit ≤ 100 per call)
//   get   — fetch one charge by id
//
// Auth: STRIPE_API_KEY env var (or STRIPE_TEST_API_KEY for the test
// dashboard). The tool errors out cleanly when neither is set —
// agents can probe with a clarification before retrying.
//
// Output JSON includes the saaslinks.Stripe URL alongside the charge
// data so a human reviewer can click through to the Stripe dashboard
// from the receipt UI. The dashboard URL covers the same payment
// regardless of whether the agent fetched it via charges/<id> or
// payment_intents — Stripe redirects in both directions.
//
// Read-only by design — no charge/refund/payout actions in this
// slice. Those land behind the existing approval gate in a follow-up.

// stripeAPIBase is the production Stripe API root. Tests inject a
// fake server via the package-private stripeAPIBaseFn override.
const stripeAPIBase = "https://api.stripe.com/v1"

// stripeAPIBaseFn lets tests substitute a fake server URL.
// Production code uses stripeAPIBase; only test files reassign this.
var stripeAPIBaseFn = func() string { return stripeAPIBase }

// stripeHTTPClient is a 15-second-timeout client for the Stripe API.
// Stripe's own SLA targets sub-second latency, so 15s is comfortably
// above any non-pathological round-trip. Replaced by tests that
// need to exercise context-cancellation paths.
var stripeHTTPClient = &http.Client{Timeout: 15 * time.Second}

// StripeTool is a read-only Stripe API client tool. Stateless —
// auth + base URL are read on each Execute so the operator can
// rotate keys without restarting the gateway.
type StripeTool struct{}

func (StripeTool) Name() string { return "stripe" }

func (StripeTool) Description() string {
	return "Read-only Stripe API client. " +
		"Actions: list (paginated charges), get (one charge by id). " +
		"Returns JSON with charge data + a Stripe dashboard URL for human review. " +
		"Requires STRIPE_API_KEY env var (or STRIPE_TEST_API_KEY for test mode)."
}

func (StripeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list", "get"],
      "description": "list returns recent charges; get fetches one charge by id."
    },
    "id": {
      "type": "string",
      "description": "Charge id (ch_...) or payment-intent id (pi_...). Required for get."
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "description": "list-only: max charges to return. Default 10, cap 100."
    },
    "starting_after": {
      "type": "string",
      "description": "list-only: cursor for the next page (Stripe-style)."
    },
    "test_mode": {
      "type": "boolean",
      "description": "Use STRIPE_TEST_API_KEY + the test dashboard. Default: live."
    }
  }
}`)
}

type stripeParams struct {
	Action        string `json:"action"`
	ID            string `json:"id,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	StartingAfter string `json:"starting_after,omitempty"`
	TestMode      bool   `json:"test_mode,omitempty"`
}

// Execute dispatches list / get against the Stripe API.
func (t StripeTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p stripeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	apiKey, mode := stripeAuth(p.TestMode)
	if apiKey == "" {
		return &Result{Error: "STRIPE_API_KEY (or STRIPE_TEST_API_KEY for test mode) is required"}, nil
	}

	switch p.Action {
	case "list":
		return t.list(ctx, apiKey, mode, p)
	case "get":
		return t.get(ctx, apiKey, mode, p)
	case "":
		return &Result{Error: "action required: list or get"}, nil
	default:
		return &Result{Error: fmt.Sprintf("unknown action %q (use list or get)", p.Action)}, nil
	}
}

// stripeAuth picks the right env var + Stripe mode based on the
// caller's test_mode flag. test_mode=true falls back to the live
// key when STRIPE_TEST_API_KEY isn't set, so a developer with only
// one key configured can still hit the test endpoint via Stripe's
// test-mode account discovery.
func stripeAuth(testMode bool) (string, saaslinks.StripeMode) {
	if testMode {
		if k := strings.TrimSpace(os.Getenv("STRIPE_TEST_API_KEY")); k != "" {
			return k, saaslinks.StripeTest
		}
	}
	if k := strings.TrimSpace(os.Getenv("STRIPE_API_KEY")); k != "" {
		mode := saaslinks.StripeLive
		if testMode {
			mode = saaslinks.StripeTest
		}
		return k, mode
	}
	return "", saaslinks.StripeLive
}

// list calls GET /v1/charges with the supplied limit + cursor.
func (StripeTool) list(ctx context.Context, apiKey string, mode saaslinks.StripeMode, p stripeParams) (*Result, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	if p.StartingAfter != "" {
		q.Set("starting_after", p.StartingAfter)
	}
	endpoint := stripeAPIBaseFn() + "/charges?" + q.Encode()

	body, err := stripeGET(ctx, endpoint, apiKey)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	// Decode the list to extract charge ids — we want the per-charge
	// dashboard links in the structured output, not just an opaque
	// JSON blob the agent has to re-parse.
	var listResp struct {
		Object string `json:"object"`
		Data   []struct {
			ID       string `json:"id"`
			Amount   int64  `json:"amount"`
			Currency string `json:"currency"`
			Status   string `json:"status"`
			Created  int64  `json:"created"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return &Result{Error: fmt.Sprintf("decode list: %v", err)}, nil
	}

	type chargeView struct {
		ID       string `json:"id"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Status   string `json:"status"`
		Created  int64  `json:"created"`
		URL      string `json:"url"`
	}
	out := struct {
		Charges []chargeView `json:"charges"`
		HasMore bool         `json:"has_more"`
	}{HasMore: listResp.HasMore}

	for _, c := range listResp.Data {
		dashURL, _ := saaslinks.Stripe(mode, c.ID)
		out.Charges = append(out.Charges, chargeView{
			ID: c.ID, Amount: c.Amount, Currency: c.Currency,
			Status: c.Status, Created: c.Created, URL: dashURL,
		})
	}

	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// get calls GET /v1/charges/<id>.
func (StripeTool) get(ctx context.Context, apiKey string, mode saaslinks.StripeMode, p stripeParams) (*Result, error) {
	if strings.TrimSpace(p.ID) == "" {
		return &Result{Error: "id required for get action"}, nil
	}
	// Stripe's own ids are in [A-Za-z0-9_]+; reject anything that
	// would let a caller break out of the URL path. saaslinks.Stripe
	// also validates the id, but rejecting earlier makes the error
	// message clearer.
	if !isStripeID(p.ID) {
		return &Result{Error: fmt.Sprintf("malformed id %q (expected ch_… or pi_…)", p.ID)}, nil
	}

	endpoint := stripeAPIBaseFn() + "/charges/" + p.ID
	body, err := stripeGET(ctx, endpoint, apiKey)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	dashURL, _ := saaslinks.Stripe(mode, p.ID)

	// Append the dashboard URL to the raw response. Two strategies:
	// (a) pre-decode + remarshal to add a top-level "url" field, or
	// (b) pass through the raw bytes alongside the URL. Going with
	// (b) so the agent sees the unmodified Stripe response (better
	// for tools that want to grep for fields without re-parsing).
	out := struct {
		Charge json.RawMessage `json:"charge"`
		URL    string          `json:"url"`
	}{Charge: body, URL: dashURL}

	js, _ := json.MarshalIndent(out, "", "  ")
	return &Result{Output: string(js)}, nil
}

// stripeGET is the shared HTTP helper. Returns the raw body or an
// error wrapping the status + response body when non-200.
func stripeGET(ctx context.Context, endpoint, apiKey string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Stripe-Version", "2024-04-10")
	req.Header.Set("Accept", "application/json")

	resp, err := stripeHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stripe: status %d: %s",
			resp.StatusCode, truncateStripeBody(string(body)))
	}
	return body, nil
}

func truncateStripeBody(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

// isStripeID returns true when s looks like a Stripe object id —
// alphanumerics + underscores only, 1..255 chars. Stripe doesn't
// publish a formal grammar, but every id we've seen fits this shape.
func isStripeID(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}
	for _, c := range s {
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// Compile-time interface check.
var _ Tool = StripeTool{}

func init() {
	Register(StripeTool{})
}
