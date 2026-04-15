package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/euraika-labs/pan-agent/internal/config"
)

// webview2FallbackWindow is how long the WebGL2 probe is skipped after a
// user accepts the fallback. Seven days balances "don't re-prompt after
// every relaunch" against "eventually re-check in case drivers updated".
// Both sides (Go handler below AND desktop/src/main.tsx probe) reference
// this constant; the probe reads BrowserFallbackUntil from /v1/config and
// skips itself when now < until.
const webview2FallbackWindow = 7 * 24 * time.Hour

// fallbackResponse is the shape POST /v1/office/fallback-detected returns.
// Frontend reads FallbackURL to open in system browser and UntilDate to
// render the splash's "won't show again until ..." line.
type fallbackResponse struct {
	FallbackURL string `json:"fallbackUrl"`
	UntilDate   string `json:"untilDate"` // RFC3339
}

// handleFallbackDetected services POST /v1/office/fallback-detected.
//
// Called by desktop/src/main.tsx's WebGL2 probe when the Tauri webview
// reports no usable WebGL2 context. The handler:
//  1. Computes "now + 7 days" as an RFC3339 timestamp.
//  2. Persists it to office.browser_fallback_until via EnsureProfileValue.
//  3. Returns the fallback URL (the embedded Claw3D served from the Go
//     gateway — same URL the Office iframe uses in the non-fallback path)
//     plus the until timestamp for the splash copy.
//
// On persistence failure we still return 200 with the computed timestamp:
// the splash + shell.open must fire regardless, and the next launch will
// simply re-probe. The fallback is user-facing, not a hard security
// boundary — we optimise for always-responsive over persist-or-die.
//
// No request body is read today. Forward-compat: future callers may want
// to pass telemetry about which specific WebGL2 check failed.
func (s *Server) handleFallbackDetected(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	until := now.Add(webview2FallbackWindow)
	untilRFC3339 := until.Format(time.RFC3339)

	// Best-effort persist — don't block the response on disk errors.
	// Next launch re-probes cleanly if this fails.
	_ = config.EnsureProfileValue(s.profile, "office.browser_fallback_until", untilRFC3339)

	// Hardcoded to the standard gateway port. Users on a non-default
	// port know to hit it manually; this endpoint fires rarely enough
	// that we don't need runtime URL derivation.
	fallbackURL := "http://localhost:8642/office/"

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(fallbackResponse{
		FallbackURL: fallbackURL,
		UntilDate:   untilRFC3339,
	})
}
