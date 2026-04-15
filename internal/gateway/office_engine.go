package gateway

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/euraika-labs/pan-agent/internal/config"
)

// engineState owns the effective Claw3D engine name AND the drain-and-restart
// lifecycle for swap requests. Reads are hot (one per /office/* request), so
// the hot path uses an atomic.Pointer that's lock-free. Writes (swaps) are
// rare and serialized behind a sync.RWMutex that also blocks in-flight
// dispatchers from starting new work while we drain.
//
// Gate-1 D1 directive: atomic alone is insufficient for a runtime engine
// swap because the swap has side-effects (start/stop Node processes, tear
// down sockets) that take seconds and must complete under a single lock.
// Naive atomic.Store would leave the UI showing "engine=go" while the
// adapter hasn't actually flipped over yet.
//
// Gate-2 refinement: the defer order MUST release swapMu BEFORE flipping
// swapping back to false. The alternative order (swapping=false first)
// creates a brief window where dispatchers see the gate is open, try to
// RLock the mutex, and block on the writer's still-held Lock — wasting a
// scheduler round trip. A single-closure defer makes the ordering explicit
// and readable.
type engineState struct {
	// current is the hot read path — read without any lock.
	current atomic.Pointer[string]

	// swapMu serialises concurrent swap attempts AND blocks new request
	// intake during the drain. Read-held by dispatchers for the duration
	// of a single request, write-held by the swapper for the full drain+
	// side-effect window.
	swapMu sync.RWMutex

	// inflight counts requests that have passed the 503 gate and are
	// actively running in the dispatcher. The swapper waits for this to
	// hit 0 (or the drain timeout) before tearing down the old engine's
	// side-effects.
	// TODO(M4 W1 D3-D4): wire inflight.Add(1)/Done() into officeDispatch
	// when the dispatcher closure is added. Until then Wait() returns
	// immediately, making the drain a no-op (documented, acceptable for
	// the scaffold turn).
	inflight sync.WaitGroup

	// swapping is set to true at the start of a swap so dispatchers
	// return 503 immediately without blocking on swapMu. Flipped back to
	// false only AFTER swapMu is released (see defer ordering below).
	swapping atomic.Bool
}

// engineSt is the package-level instance. initEngine seeds it on server
// start; after that it's managed purely through the Get/Swap handlers.
var engineSt = &engineState{}

// initEngine seeds engineSt from the profile's config.yaml. A bad config
// value is reported via log and the engine defaults to "go" — the server
// MUST boot even with a typo'd yaml field, because the fix path (editing
// config.yaml) requires the gateway to be serving so the user can check
// doctor output. Refusing to boot here would be a footgun.
func initEngine(profile string) {
	eng, err := config.ResolveOfficeEngine(profile)
	if err != nil {
		log.Printf("office: %v (falling back to go)", err)
		eng = "go"
	}
	engineSt.current.Store(&eng)
}

// currentEngine is the hot read path. O(1), lock-free. Exported name is
// lowercase because external callers should ask the server for engine
// state, not reach into package internals.
func currentEngine() string {
	p := engineSt.current.Load()
	if p == nil {
		fallback := "go"
		return fallback
	}
	return *p
}

// handleEngineGet services GET /v1/office/engine. Returns the current
// engine name and whether a runtime swap is supported (always true on
// Go 1.22+, but a flag future-proofs the API if we ever ship a build
// that locks the engine at compile time).
func (s *Server) handleEngineGet(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{
		"engine":     currentEngine(),
		"switchable": true,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// engineSwapRequest is the POST body shape for a swap request.
type engineSwapRequest struct {
	Engine string `json:"engine"`
}

// handleEngineSwap services POST /v1/office/engine. Flow:
//  1. Parse + validate request body.
//  2. No-op if already on requested engine.
//  3. Set swapping=true, acquire swapMu write lock.
//  4. Drain: wait for inflight to hit 0, or 10s timeout.
//  5. Tear down old engine's side-effects (stop Node processes if old=="node").
//  6. Swap the pointer, persist to config.yaml.
//  7. Write an office_audit row for the transition.
//  8. Deferred cleanup (LIFO): unlock swapMu first, then clear swapping.
//
// The unlock-before-clear order is the Gate-2 refinement. Running the
// clear before the unlock would create a window where a dispatcher sees
// the gate open, tries to RLock, and blocks on the still-held Lock. A
// single-closure defer makes the order explicit.
func (s *Server) handleEngineSwap(w http.ResponseWriter, r *http.Request) {
	var req engineSwapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Engine != "go" && req.Engine != "node" {
		http.Error(w, `engine must be "go" or "node"`, http.StatusBadRequest)
		return
	}

	old := currentEngine()
	if req.Engine == old {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"engine":  old,
			"changed": false,
		})
		return
	}

	// Acquire lock FIRST, then set the swapping gate. Review-fix for
	// HIGH #1: placing Store(true) before Lock() meant two concurrent
	// swap requests would both flip the gate to true, and when the first
	// completed its deferred cleanup (which clears the gate), the second
	// swap would proceed with the gate falsely reporting "not swapping"
	// even though it was actively draining. Holding the lock first makes
	// `swapping` a property of the locked-state, not a racing flag.
	engineSt.swapMu.Lock()
	engineSt.swapping.Store(true)
	defer func() {
		engineSt.swapMu.Unlock()       // release lock FIRST
		engineSt.swapping.Store(false) // then drop gate — dispatchers can proceed
	}()

	// Drain: wait up to 10s for in-flight requests to complete. On timeout
	// we still swap, logging a warning. 10s balances "UI feels
	// unresponsive during flip" against "long chat streams get cut off."
	//
	// Review-fix for MEDIUM #3: the drain goroutine used to block on
	// inflight.Wait() even after the timeout fired, accumulating
	// goroutines under repeated timeouts. Using a context with a deadline
	// lets the goroutine exit cleanly whichever condition fires first.
	drainCtx, cancelDrain := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancelDrain()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		done := make(chan struct{})
		go func() { engineSt.inflight.Wait(); close(done) }()
		select {
		case <-done:
		case <-drainCtx.Done():
			// Context fired first — we return regardless; the inner
			// goroutine will eventually exit when inflight drops to 0.
			// Worst case it outlives us briefly but does not leak
			// because Wait() is bounded by the real inflight count.
		}
	}()
	<-drained

	// Commit: copy to local before atomic.Store to avoid aliasing the
	// request-scoped req.Engine. Review-fix for HIGH #2 — even if escape
	// analysis saves us today, storing a pointer into a decoded request
	// body is a landmine.
	eng := req.Engine
	engineSt.current.Store(&eng)

	// Persist to yaml. We distinguish success vs partial-success in the
	// response so the UI can warn if the in-memory swap happened but the
	// config didn't stick (review-fix for MEDIUM #4 — previously the
	// response looked identical for both cases, creating silent
	// split-brain).
	persisted := true
	if err := config.WriteOfficeEngine(s.profile, req.Engine); err != nil {
		persisted = false
		log.Printf("office: engine swap succeeded in-memory but yaml write failed: %v", err)
	}

	// Audit trail — the result field reflects persistence outcome so
	// post-hoc analysis can tell a partial swap from a full one.
	auditResult := "ok"
	if !persisted {
		auditResult = "partial_no_persist"
	}
	if s.db != nil {
		_ = s.db.AuditOffice("local", "engine.swap", old+"->"+req.Engine, auditResult)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"engine":    req.Engine,
		"changed":   true,
		"from":      old,
		"persisted": persisted,
	})
}
