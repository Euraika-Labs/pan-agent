# Claw3D WebSocket Protocol v3 — Reference

Frozen at pan-agent 0.4.0. This document specifies the wire format of
the `/office/ws` WebSocket endpoint served by `internal/claw3d` and
consumed by the embedded Claw3D bundle. The protocol is a direct Go
port of the upstream `hermes-gateway-adapter.js` reference
implementation; any shape change must bump the version integer in
`hello-ok` and be called out explicitly in the release notes.

**Target audience:** future adapter implementers, protocol conformance
testers, and anyone diagnosing why a specific frame was rejected.

---

## 1. Transport

- URL: `ws://<host>:<port>/office/ws`
- Upgrade: standard [RFC 6455](https://datatracker.ietf.org/doc/html/rfc6455) WebSocket handshake
- Authentication: `claw3d_sess` cookie (HttpOnly, SameSite=Strict) minted by `GET /office/` prior to the upgrade. Also accepted via `?session_token=<value>` query parameter.
- Origin check: exact-match allowlist. Empty origins permitted by default; strict mode (`office.strict_origin: true`) rejects them.
- Rate limit: per-remote-IP token bucket, burst 20, refill 5/sec. Lockout after 3 consecutive auth failures for 30 seconds.

## 2. Frame types

All frames are JSON objects sent as WebSocket text messages. Three types exist:

### 2.1 `req` (client → server)

```json
{
  "type": "req",
  "id": "<client-assigned string>",
  "method": "<method name>",
  "params": { ... }
}
```

- `id` correlates the response. Clients assign; server echoes.
- `method` is one of the 26 methods listed in §3.
- `params` is method-specific; may be `null` or omitted.

### 2.2 `res` (server → client)

```json
{
  "type": "res",
  "id": "<echo of req.id>",
  "ok": true,
  "payload": { ... }
}
```

On failure:

```json
{
  "type": "res",
  "id": "<echo of req.id>",
  "ok": false,
  "error": { "code": "<string>", "message": "<string>" }
}
```

- `ok` is always present on `res` frames (never omitted).
- `payload` is method-specific; present only when `ok: true`.
- `error.code` is a stable string identifier; `error.message` is human-readable.

### 2.3 `event` (server → client push)

```json
{
  "type": "event",
  "event": "<event name>",
  "seq": <monotonic uint64>,
  "payload": { ... }
}
```

- `seq` increments globally across all event types. Clients use it to detect drops.
- `event` is one of the 4 names listed in §4.
- Events are fire-and-forget; no client ack.

## 3. Methods (26)

| Method | Params | Payload | Notes |
|---|---|---|---|
| `agents.list` | `{}` | `{agents: Agent[]}` | Returns all persisted office agents |
| `agents.create` | `{name, workspace?, role?, identity?}` | `{agent: Agent}` | Creates a new agent, returns it |
| `agents.update` | `{id, name?, workspace?, role?, identity?}` | `{agent: Agent}` | Partial update by ID |
| `agents.delete` | `{id}` | `{deleted: bool}` | Cascades to sessions + messages |
| `agents.files.get` | `{id}` | `{files: any}` | Returns opaque identity blob |
| `agents.files.set` | `{id, files}` | `{ok: bool}` | Writes opaque identity blob |
| `sessions.list` | `{agentId?}` | `{sessions: Session[]}` | Optional agent filter |
| `sessions.preview` | `{id, limit?}` | `{sessionId, messages: Message[]}` | Newest N messages |
| `sessions.patch` | `{id?, agentId?, state?, settings?}` | `{session: Session}` | Upsert |
| `sessions.reset` | `{id}` | `{reset: bool}` | Deletes all messages, keeps session row |
| `chat.send` | `{sessionId?, agentId?, message, history?, model?}` | `{runId, sessionId}` | Spawns a background run; stream via `chat` events |
| `chat.abort` | `{runId}` | `{aborted: bool}` | Cancels an active run |
| `chat.history` | `{sessionId, limit?}` | `{sessionId, messages: Message[]}` | Alias for `sessions.preview` |
| `agent.wait` | `{runId, timeoutMs?}` | `{done: bool}` | Polls activeRuns; capped at 30s |
| `status` | `{}` | `{protocolVersion, adapterType, adapterVersion, os, uptimeMs}` | Health + metadata |
| `wake` | `{}` | `{awake: true}` | No-op keepalive |
| `config.get` | `{}` | `{profile, model, provider, baseUrl}` | Current effective config |
| `config.set` | `{}` | `{ok, note, ...}` | Read-only on Claw3D surface; use `/v1/config` |
| `config.patch` | *alias for `config.set`* | — | Same shape |
| `models.list` | `{}` | `{models: ModelInfo[]}` | Wraps `internal/models.List` |
| `tasks.list` | `{}` | `{tasks: []}` | Stub; empty array for 0.4.0 |
| `skills.status` | `{}` | `{installed: int, available: int}` | Stub; wired in M5 |
| `exec.approvals.get` | `{}` | `{approvals: []}` | Stub; wired in M5 |
| `exec.approvals.set` | `{}` | `{ok: true}` | Stub |
| `exec.approval.resolve` | `{}` | `{ok: true}` | Stub |
| `cron.list` | `{}` | `{jobs: []}` | Ack-only for 0.4.0 |
| `cron.add` | `{}` | `{ok: true}` | Ack-only for 0.4.0 |
| `cron.remove` | `{}` | `{ok: true}` | Ack-only |
| `cron.patch` | `{}` | `{ok: true}` | Ack-only |
| `cron.run` | `{}` | `{ran: true}` | Ack-only |

## 4. Events (4)

| Event | Payload | Emitted when |
|---|---|---|
| `hello-ok` | `{protocol, adapterType, adapterVersion, features: {methods, events}}` | On WS open, before any req handling |
| `chat` | `{runId, state: "delta"\|"final"\|"error"\|"aborted", delta?, content?, error?}` | During a `chat.send` stream |
| `presence` | `{agents: map<id,snapshot>, dropped: uint64}` | On agent state change; coalesces via `presenceCoalescer` |
| `heartbeat` | `{ts: int64}` | Periodically by `EmitHeartbeat` |
| `cron` | `{}` | Reserved; not emitted in 0.4.0 |

## 5. `hello-ok` handshake

First frame after WS upgrade. Shape:

```json
{
  "type": "event",
  "event": "hello-ok",
  "seq": 1,
  "payload": {
    "protocol": 3,
    "adapterType": "hermes",
    "adapterVersion": "0.4.0-alpha",
    "features": {
      "methods": [...all 26...],
      "events": ["hello-ok", "chat", "presence", "heartbeat", "cron"]
    }
  }
}
```

Clients use `features.methods` to detect capability gaps when talking
to a mismatched adapter version. `protocol` is the hard contract
number; a client that speaks protocol 3 will refuse to talk to a
server claiming any other integer.

## 6. Dedup and backpressure

- **Per-conn outbox:** bounded at 128 frames. Drops are logged in the client reader.
- **Presence coalescing:** server-side `presenceCoalescer` keeps at most one snapshot per agent ID in a pending map. On flush, coalesced frames carry a `dropped` counter indicating how many snapshots were collapsed. Clients treat a non-zero `dropped` as a signal to resync via `sessions.list`.
- **Heartbeat cadence:** one `heartbeat` event per 15 seconds; configurable via `office.heartbeat_secs` (undocumented for 0.4.0).

## 7. Error codes

Stable strings. Never renamed without a protocol version bump.

| Code | Meaning |
|---|---|
| `bad_frame` | JSON unmarshal failed |
| `bad_envelope` | Missing `type`, `id`, or `method` on req |
| `unknown_method` | Method not in registry |
| `handler_error` | Method handler returned non-nil error |
| `not_implemented` | Stub method explicitly rejects |

## 8. Stability guarantees

- **Frozen at pan-agent 0.4.0.** Additions allowed (forward-compat);
  renames and deletions require `protocol` bump to 4 plus a
  dual-serve window across two minor releases.
- **Conformance tests:** `internal/claw3d/adapter_test.go` runs 3
  golden fixtures (status round-trip, unknown-method error, isCritical
  matrix). M6 follow-up expands to full 26-method conformance against
  a captured reference.
- **Upstream drift:** `vendor-sync.yml` cron opens draft PRs on
  `iamlukethedev/Claw3D:main` movement; the protocol reference should
  NOT drift from upstream without an explicit fork note.
