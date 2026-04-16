package claw3d

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestSessionStore_ReaperReclaimsExpired verifies the size-threshold sweep
// added after the M1 code review: never-verified tokens must not accumulate
// past reapThreshold.
func TestSessionStore_ReaperReclaimsExpired(t *testing.T) {
	s := newSessionStore()

	// Fill past threshold with already-expired tokens.
	past := time.Now().Add(-time.Hour)
	s.mu.Lock()
	for i := 0; i < reapThreshold+5; i++ {
		s.live[string(rune('a'+i%26))+string(rune('a'+i/26))] = past
	}
	s.mu.Unlock()

	// Issue a fresh token — this should trigger the eager sweep.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://127.0.0.1/office/", nil)
	_ = s.issue(w, r)

	s.mu.RLock()
	n := len(s.live)
	s.mu.RUnlock()
	if n > 5 {
		t.Errorf("expected reaper to prune expired tokens, map still has %d entries", n)
	}
}

// TestPublicHostFor_RejectsUnsafeForwardedHost covers the sanitation guard
// against header-injection into the config.js JS string.
func TestPublicHostFor_RejectsUnsafeForwardedHost(t *testing.T) {
	a := &Adapter{}
	cases := []struct {
		name   string
		xfh    string
		rHost  string
		expect string
	}{
		{"clean dns host", "office.example.com", "fallback", "office.example.com"},
		{"clean with port", "office.example.com:443", "fallback", "office.example.com:443"},
		{"ipv4", "10.0.0.1:8080", "fallback", "10.0.0.1:8080"},
		{"ipv6 bracketed", "[::1]:8080", "fallback", "[::1]:8080"},
		{"backtick injection", "evil.com`;alert(1)//", "fallback", "fallback"},
		{"space injected", "a b c", "fallback", "fallback"},
		{"quote injected", `a"b`, "fallback", "fallback"},
		{"empty header", "", "fallback", "fallback"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/office/config.js", nil)
			if tc.xfh != "" {
				r.Header.Set("X-Forwarded-Host", tc.xfh)
			}
			r.Host = tc.rHost
			got := a.publicHostFor(r)
			if got != tc.expect {
				t.Errorf("publicHostFor(XFH=%q, Host=%q) = %q, want %q",
					tc.xfh, tc.rHost, got, tc.expect)
			}
		})
	}
}

// TestSessionStore_VerifyRejectsExpired guards the normal-path TTL behavior.
func TestSessionStore_VerifyRejectsExpired(t *testing.T) {
	s := newSessionStore()
	// Manually inject an expired token.
	s.mu.Lock()
	s.live["stale"] = time.Now().Add(-time.Second)
	s.mu.Unlock()

	r := httptest.NewRequest("GET", "/office/ws?session_token=stale", nil)
	if s.verify(r) {
		t.Fatal("verify accepted an expired token")
	}
	s.mu.RLock()
	_, stillLive := s.live["stale"]
	s.mu.RUnlock()
	if stillLive {
		t.Error("verify did not prune the expired entry")
	}
}

// ── M5 auth polish tests ──────────────────────────────────────────────
//
// Five new test cases covering:
//   1. recordFailure + isLockedOut after failureThreshold
//   2. lockout clears after lockoutDuration
//   3. recordSuccess resets consecutive-failure counter
//   4. originAllowed respects strictOrigin=true (empty origin → denied)
//   5. originAllowed respects strictOrigin=false (empty origin → allowed, default)
//   6. allowRate burst + refill semantics

// TestSessionStore_LockoutAfterFailures verifies 3 consecutive failures
// trip isLockedOut and the 3rd recordFailure returns lockedOut=true.
func TestSessionStore_LockoutAfterFailures(t *testing.T) {
	s := newSessionStore()
	const ip = "10.0.0.1"

	// Two failures — still below threshold, not locked out.
	if locked := s.recordFailure(ip); locked {
		t.Fatal("locked out after 1 failure")
	}
	if s.isLockedOut(ip) {
		t.Fatal("isLockedOut returned true after 1 failure")
	}
	if locked := s.recordFailure(ip); locked {
		t.Fatal("locked out after 2 failures")
	}

	// Third failure crosses the threshold.
	if locked := s.recordFailure(ip); !locked {
		t.Fatal("expected lockout after 3rd failure, got false")
	}
	if !s.isLockedOut(ip) {
		t.Fatal("isLockedOut false after lockout returned true")
	}
}

// TestSessionStore_LockoutExpiresNaturally fabricates an already-expired
// lockout timestamp and verifies isLockedOut reads it as cleared. The
// real lockoutDuration (30s) is too long for a unit test, so we poke
// the lockoutState directly — same thing the real clock would do.
func TestSessionStore_LockoutExpiresNaturally(t *testing.T) {
	s := newSessionStore()
	const ip = "10.0.0.2"

	// Simulate a past lockout by writing directly under the mutex.
	s.mu.Lock()
	s.lockoutMap[ip] = &lockoutState{
		failures:    failureThreshold,
		lockedUntil: time.Now().Add(-time.Second), // expired 1s ago
		tokens:      bucketBurst,
		lastRefill:  time.Now(),
	}
	s.mu.Unlock()

	if s.isLockedOut(ip) {
		t.Fatal("isLockedOut returned true for an expired lockout")
	}
}

// TestSessionStore_RecordSuccessResetsFailures verifies a successful auth
// clears the consecutive-failure counter so a user who typed one wrong
// cookie then refreshed with the right one isn't punished.
func TestSessionStore_RecordSuccessResetsFailures(t *testing.T) {
	s := newSessionStore()
	const ip = "10.0.0.3"

	// Record two failures — below threshold.
	s.recordFailure(ip)
	s.recordFailure(ip)

	// Simulate a successful auth.
	s.recordSuccess(ip)

	// Now record two more failures; should NOT trip lockout because
	// the counter was reset.
	if locked := s.recordFailure(ip); locked {
		t.Fatal("locked out after only 1 failure post-reset")
	}
	if locked := s.recordFailure(ip); locked {
		t.Fatal("locked out after only 2 failures post-reset")
	}
	if s.isLockedOut(ip) {
		t.Fatal("isLockedOut true after recordSuccess cleared the counter")
	}
}

// TestOriginAllowed_StrictMode covers both the default-permissive and
// strict-mode paths through the extended originAllowed function.
func TestOriginAllowed_StrictMode(t *testing.T) {
	cases := []struct {
		name         string
		origin       string
		strictOrigin bool
		want         bool
	}{
		{"empty origin permissive", "", false, true},
		{"empty origin strict", "", true, false},
		{"known origin permissive", "http://localhost:5173", false, true},
		{"known origin strict", "http://localhost:5173", true, true},
		{"unknown origin permissive", "https://evil.example.com", false, false},
		{"unknown origin strict", "https://evil.example.com", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := originAllowed(tc.origin, tc.strictOrigin)
			if got != tc.want {
				t.Errorf("originAllowed(%q, %v) = %v, want %v",
					tc.origin, tc.strictOrigin, got, tc.want)
			}
		})
	}
}

// TestSessionStore_AllowRateBurstAndRefill verifies the token bucket
// allows bucketBurst requests immediately, then denies the next one,
// and refills after a wait. The refill wait is computed from
// bucketRefillRate so the test is tuned to reality without sleeping
// for seconds.
func TestSessionStore_AllowRateBurstAndRefill(t *testing.T) {
	s := newSessionStore()
	const ip = "10.0.0.4"

	// Drain the initial burst — all 20 should succeed.
	for i := 0; i < bucketBurst; i++ {
		if !s.allowRate(ip) {
			t.Fatalf("request %d/%d denied during initial burst", i+1, bucketBurst)
		}
	}

	// 21st request — bucket is empty, should deny.
	if s.allowRate(ip) {
		t.Fatal("request past burst was allowed")
	}

	// Poke lastRefill back so the bucket looks like it's been idle
	// long enough to refill one token. At 5 tokens/sec, 220ms ≈ 1.1
	// tokens worth.
	s.mu.Lock()
	st := s.lockoutMap[ip]
	st.lastRefill = time.Now().Add(-220 * time.Millisecond)
	s.mu.Unlock()

	if !s.allowRate(ip) {
		t.Fatal("request denied after ~220ms idle (1+ token refill expected)")
	}
}
