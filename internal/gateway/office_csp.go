package gateway

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// cspLogMaxBytes caps the csp-violations.log file at 10 MB to prevent
// unbounded growth under adversarial conditions. Gate-1 refinement #7:
// even with the 60-second front-end dedup window, a misconfigured
// iframe could in principle generate thousands of unique (directive,
// URI) tuples over a long session. The hard cap is the last-line
// defence; once hit, subsequent reports are silently dropped and the
// handler still returns 204 so the front-end doesn't retry. M6's
// cleanup cron will handle rotation.
//
// The size check is atomic: we take cspWriteMu BEFORE stat-and-open,
// so concurrent requests cannot both read a sub-cap size and both
// append. Gate-2 refinement #1 (TOCTOU) resolved by keeping the
// entire stat+open+write sequence inside one critical section. The
// lock cost is negligible at this endpoint's realistic rate.
const cspLogMaxBytes int64 = 10 * 1024 * 1024

// cspReport is the JSON shape main.tsx POSTs per securitypolicyviolation
// event. All fields are best-effort strings; the log is for human
// diagnosis during a 5-day dogfood window, not machine processing.
type cspReport struct {
	TS                 string `json:"ts"`
	ViolatedDirective  string `json:"violatedDirective"`
	EffectiveDirective string `json:"effectiveDirective"`
	BlockedURI         string `json:"blockedURI"`
	SourceFile         string `json:"sourceFile"`
	LineNumber         int    `json:"lineNumber"`
	ColumnNumber       int    `json:"columnNumber"`
	DocumentURI        string `json:"documentURI"`
	Sample             string `json:"sample"`
}

// cspWriteMu serialises appends to csp-violations.log. Low-rate endpoint
// (front-end dedupes 60 s windows) so contention risk is negligible but
// we still take the lock to prevent interleaved line writes on bursty
// browser violations.
var cspWriteMu sync.Mutex

// handleCSPReport appends a JSON-per-line entry to csp-violations.log
// inside AgentHome. Returns 204 on success/cap-reached, 400 on malformed
// body, 500 on filesystem error. Never blocks and never fans out.
//
// Rationale: Tauri v2 has no cspReportOnly mode, so we ship enforcing
// CSP with this local collector for observability during the 5-day
// manual dogfood that precedes the 0.4.0 tag. The 10 MB hard cap
// (Gate-1 refinement #7) and atomic-under-mutex size check (Gate-2
// refinement #1) are the two load-bearing design choices.
func (s *Server) handleCSPReport(w http.ResponseWriter, r *http.Request) {
	var rep cspReport
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	// Default the timestamp server-side if the client omits it — keeps
	// every row in the log sortable even if a browser quirks the field.
	if rep.TS == "" {
		rep.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}

	// Centralised in paths.CSPViolationsLog() so doctor --csp-violations
	// (M6-C1) reads the exact same path the writer uses. Previously this
	// was duplicated in two files; M5 prep consolidated it.
	logPath := paths.CSPViolationsLog()

	cspWriteMu.Lock()
	defer cspWriteMu.Unlock()

	// Hard cap: one os.Stat inside the critical section. The mutex makes
	// the stat+open+write sequence atomic against concurrent POSTs, so
	// two reports cannot both read a sub-cap size and both append. We
	// don't error when the file doesn't exist — that's the first-ever
	// report and the append will create it via O_CREATE below.
	if st, err := os.Stat(logPath); err == nil && st.Size() >= cspLogMaxBytes {
		// Silent drop — still return 204 so the front-end doesn't retry
		// and potentially wedge its queue on a permanently capped log.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// json.Marshal cannot fail on a plain struct of strings and ints —
	// the blank return is safe. f.Write CAN fail (disk full, quota) —
	// surface it as 500 so the front-end doesn't presume success on a
	// partial write. Review finding #1 (MEDIUM) — don't swallow disk
	// errors silently even when the handler is best-effort.
	line, _ := json.Marshal(rep)
	if _, err := f.Write(append(line, '\n')); err != nil {
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
