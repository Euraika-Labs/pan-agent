# WebSocket Backend Benchmark — 2026-Q2 Decision Record

**Status: DEFERRED to 0.5.0**

## Context

During M5 planning, Phase 1 research surfaced a potential optimization:
replacing the current `gorilla/websocket` backend with `coder/websocket`
(formerly `nhooyr.io/websocket`) which is actively maintained and has
a slightly cleaner API. Gate-1 of the M5 embrace decided to **defer**
the bench + swap to 0.5.0 because:

1. **No measured pain.** The gateway's WS hot path (per-connection
   read/write goroutines + bounded outbox) handles the observed
   traffic profile comfortably. No user complaint, no profile trace
   showing gorilla as a bottleneck.

2. **Ship-risk > reward.** Swapping WebSocket libraries is a
   structural change — the existing 3-gate handleWS (lockout → rate
   limit → verify) is tuned to gorilla's Upgrader+Conn API. A library
   swap would touch adapter_server.go, client.go, and the chaos test
   harness. Not worth the churn to chase a hypothetical.

3. **0.5.0 has a natural re-benchmark moment.** When the legacy Node
   engine is finally removed (0.5.0 per runbook §11), the rate limiter
   + strict_origin logic get simplified. That's a better time to
   revisit the library choice with a real baseline.

## Methodology (when the bench lands)

To be written in 0.5.0. Expected shape:

- **Load profile:** 100 concurrent clients, each sending ~1 req/sec,
  receiving presence broadcasts at ~1 Hz. Mirrors a realistic
  multi-agent orchestration scenario.
- **Metrics:** p50 / p95 / p99 request latency, allocations per
  request, steady-state goroutine count, memory footprint.
- **Tooling:** `go test -bench` + `pprof` heap/cpu profiles.
- **Comparison:** gorilla/websocket (baseline) vs coder/websocket
  (candidate) vs raw net/http Hijacker (stretch).
- **Decision criterion:** swap only if the candidate wins on at least
  two of the four metrics without regressing on the other two.

## Related references

- [gorilla/websocket](https://github.com/gorilla/websocket) — current
- [coder/websocket](https://github.com/coder/websocket) — candidate
- [nhooyr/websocket](https://github.com/nhooyr/websocket) — archived,
  forked to coder/
- `internal/claw3d/adapter_server.go` — current handleWS implementation
- `internal/claw3d/client.go` — per-conn goroutine wiring
- `docs/protocol.md` — frame shape (library-agnostic)

## Reviewer notes

This file exists at 0.4.0 as a placeholder so the decision record has
a stable URL. Replace with real bench data before tagging 0.5.0.
