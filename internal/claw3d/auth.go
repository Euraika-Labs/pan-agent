package claw3d

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"net/http"
	"sync"
	"time"
)

// adapterAllowedOrigins mirrors the gateway's allowlist. Wildcard-port checks
// (http://localhost:*) were rejected at Gate-2 — any local process can set an
// Origin header, so we require exact string matches and add our own vetted
// values. This is a stricter superset of internal/gateway/middleware.go's
// allowedOrigins.
var adapterAllowedOrigins = map[string]bool{
	"http://localhost:5173":  true, // Vite dev
	"http://127.0.0.1:5173":  true, // Vite dev (alt loopback name)
	"http://localhost:8642":  true, // same-origin served bundle
	"http://127.0.0.1:8642":  true,
	"tauri://localhost":      true, // Tauri production shell
}

// sessionTTL is the lifetime of a session token cookie issued on
// GET /office/ and verified at WS upgrade. Short because the Claw3D bundle
// connects immediately after load; there is no reason to keep tokens alive
// for hours.
const sessionTTL = 5 * time.Minute

// reapThreshold triggers eager pruning inside issue() when the live map
// grows past this many entries. Chosen pessimistically — a desktop app rarely
// holds more than a handful of concurrent sessions, so crossing 64 implies
// we're leaking (never-verified tokens from rapid reloads, for example).
const reapThreshold = 64

// M5 auth-polish constants. All IP-keyed, all in-memory, all bounded:
//   - failureThreshold: after this many CONSECUTIVE auth failures, the
//     originating IP is locked out for lockoutDuration. Reset on any
//     successful verify().
//   - bucketBurst + bucketRefillRate: classic token-bucket rate limiter.
//     burst=20 matches what a real user's session-cookie + WS upgrade
//     sequence does in a tight loop during a reconnect storm, while
//     refill=5/sec is a rough "legitimate reconnect cadence" ceiling.
const (
	failureThreshold = 3
	lockoutDuration  = 30 * time.Second
	bucketBurst      = 20
	bucketRefillRate = 5.0 // tokens per second
)

// lockoutState tracks both the consecutive-failure lockout AND the token
// bucket for a single remote IP. Packing them into one struct lets
// sessionStore keep ONE mutex + ONE map instead of two — fewer moving
// parts, fewer race windows.
type lockoutState struct {
	failures    int
	lastFailure time.Time
	lockedUntil time.Time

	// tokens is fractional to support the sub-integer refill under
	// bucketRefillRate per second (e.g. 4.7 tokens after 940ms). Capped
	// at bucketBurst in allowRate().
	tokens     float64
	lastRefill time.Time
}

// sessionStore is an in-memory TTL map of valid session tokens + a
// per-IP lockout/rate-limit map. It is process-local — there is no
// federation across pan-agent instances, which is fine for a single-user
// desktop app.
//
// Pruning policy (hardened after M1 code review): tokens are swept both
// lazily inside verify() AND eagerly inside issue() once the map grows past
// reapThreshold. The eager sweep guarantees never-verified tokens cannot
// accumulate indefinitely (e.g., dev workflow with hot-reload on every save).
//
// Load-bearing invariant (M5): `s.mu` guards BOTH `live` and `lockoutMap`.
// All methods that touch either field take the same mutex — do NOT split
// this into two locks. An attacker who can race session-issue against
// lockout-clear wins a way to bypass the lockout by hitting verify() at
// just the right moment, which is exactly what the single-mutex design
// prevents.
type sessionStore struct {
	mu         sync.RWMutex
	live       map[string]time.Time
	lockoutMap map[string]*lockoutState // M5: per-IP failure + rate-limit state
}

func newSessionStore() *sessionStore {
	return &sessionStore{
		live:       make(map[string]time.Time),
		lockoutMap: make(map[string]*lockoutState),
	}
}

// reapExpired removes all expired entries. Caller MUST hold s.mu for write.
func (s *sessionStore) reapExpiredLocked() {
	now := time.Now()
	for tok, exp := range s.live {
		if now.After(exp) {
			delete(s.live, tok)
		}
	}
}

// issue mints a new 128-bit token, records its expiry, and sets a
// loopback-scoped HttpOnly cookie on the response. Crosses reapThreshold →
// eagerly prune before insert.
func (s *sessionStore) issue(w http.ResponseWriter) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	tok := hex.EncodeToString(buf)
	expires := time.Now().Add(sessionTTL)
	s.mu.Lock()
	if len(s.live) >= reapThreshold {
		s.reapExpiredLocked()
	}
	s.live[tok] = expires
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "claw3d_sess",
		Value:    tok,
		Path:     "/office/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires,
	})
	return tok
}

// verify returns true iff the request carries a live session token via
// cookie or ?session_token= query. Missing/expired tokens return false so
// the caller can decide whether to 401 or issue a new one.
func (s *sessionStore) verify(r *http.Request) bool {
	got := ""
	if ck, err := r.Cookie("claw3d_sess"); err == nil {
		got = ck.Value
	} else {
		got = r.URL.Query().Get("session_token")
	}
	if got == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.live[got]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.live, got)
		return false
	}
	return true
}

// originAllowed enforces the exact-match allowlist. An empty Origin header
// is permitted by default because native WebSocket clients (curl, Go
// tests) often omit it; browser WS connections always set it, so
// spoofing requires being an actual browser cooperating with a malicious
// page. The session-token cookie (mandatory at upgrade) is the primary
// CSWSH mitigation regardless of Origin — see adapter_server.go handleWS.
//
// M5 adds the strictOrigin flag. When true, empty-Origin requests are
// rejected outright. Intended for deployments that expose the adapter
// beyond loopback (an anti-pattern this codebase discourages but must
// support). Default (false) preserves the permissive behaviour so CLI
// tools and curl probes keep working for local-dev.
func originAllowed(origin string, strictOrigin bool) bool {
	if origin == "" {
		return !strictOrigin
	}
	return adapterAllowedOrigins[origin]
}

// ── M5 auth-polish methods on sessionStore ───────────────────────────────
//
// All four methods below take s.mu (not the lockoutMap directly) so the
// single-mutex invariant documented on sessionStore holds. The write lock
// is acquired even in read-ish paths (allowRate, recordSuccess) because
// they all mutate lockoutState counters.

// isLockedOut returns true iff the IP has an active lockout window.
// Callers (handleWS) use this to fail the upgrade BEFORE running verify()
// — we don't want to leak "valid session vs expired session" timing
// information to a blocked client.
func (s *sessionStore) isLockedOut(remoteIP string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.lockoutMap[remoteIP]
	if !ok {
		return false
	}
	return time.Now().Before(st.lockedUntil)
}

// recordFailure increments the consecutive-failure count for remoteIP.
// On reaching failureThreshold, starts a lockoutDuration window and
// returns true so the caller can emit an audit row tagged "lockout".
// Below the threshold, returns false for an "attempted" audit row.
func (s *sessionStore) recordFailure(remoteIP string) (lockedOut bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.lockoutMap[remoteIP]
	if !ok {
		st = &lockoutState{
			tokens:     bucketBurst,
			lastRefill: time.Now(),
		}
		s.lockoutMap[remoteIP] = st
	}
	st.failures++
	st.lastFailure = time.Now()
	if st.failures >= failureThreshold {
		st.lockedUntil = time.Now().Add(lockoutDuration)
		return true
	}
	return false
}

// recordSuccess zeros the failure counter AND clears any active lockout.
// Called on a successful verify() so a user who typed one wrong cookie
// then refreshed with the right one isn't punished for the brief
// confusion. Rate-limit tokens are deliberately NOT refilled here —
// successful auth shouldn't be a free way to refill the bucket.
func (s *sessionStore) recordSuccess(remoteIP string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.lockoutMap[remoteIP]; ok {
		st.failures = 0
		st.lockedUntil = time.Time{}
	}
}

// allowRate implements a classic token bucket. Tokens refill at
// bucketRefillRate per second (capped at bucketBurst). A request
// consumes one token; if the bucket is empty the request is denied
// until the next refill tick (sub-second granularity via fractional
// accumulation).
//
// Returns false when the caller should be rate-limited. handleWS maps
// false to HTTP 429 with a Retry-After header.
func (s *sessionStore) allowRate(remoteIP string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.lockoutMap[remoteIP]
	if !ok {
		st = &lockoutState{
			tokens:     bucketBurst,
			lastRefill: time.Now(),
		}
		s.lockoutMap[remoteIP] = st
	}
	now := time.Now()
	elapsed := now.Sub(st.lastRefill).Seconds()
	if elapsed > 0 {
		st.tokens = math.Min(bucketBurst, st.tokens+elapsed*bucketRefillRate)
		st.lastRefill = now
	}
	if st.tokens < 1 {
		return false
	}
	st.tokens--
	return true
}
