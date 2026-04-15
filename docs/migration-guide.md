# Migrating from pan-agent 0.3.x to 0.4.0

This guide walks existing 0.3.x users through the changes in 0.4.0 —
most importantly the shift from the Node-based Claw3D sidecar to an
embedded Go adapter + statically-served bundle. The migration is
opt-out: users who do nothing still get the new default but can
revert to the legacy path via one config flag.

## 1. What changed

| Subsystem | 0.3.x | 0.4.0 |
|---|---|---|
| Claw3D Office | Node sidecar (`npm run dev`) on a separate port | Embedded Go adapter on gateway port 8642 |
| Bundle delivery | User runs Claw3D from source | Pre-built static bundle embedded via `go:embed` |
| Session history | `~/.hermes/clawd3d-history.json` | SQLite tables under `state.db` |
| WebGL2 fallback | None | 7-day skip window + system-browser open |
| CSP policy | `index.html` only | `tauri.conf.json` + `index.html` byte-identical |
| Engine toggle | N/A | `office.engine: go\|node` runtime toggle |
| Gateway auth | No rate limit | Token bucket + 3-fail lockout |
| Doctor subcommand | Basic 5 checks | +PID file, CSP log, --switch-engine, --csp-violations, --deprecated-usage, --json |
| Chaos tests | None | 2 scenarios under `//go:build chaos` |
| Real-webview E2E | None | wdio + tauri-driver matrix (Win + Linux) |

## 2. Upgrade path (happy case)

For 95% of users:

1. Install the 0.4.0 installer from GitHub Releases.
2. First launch runs the migration importer against any existing
   `~/.hermes/clawd3d-history.json` (if present).
3. A one-time `MigrationBanner` appears in the UI asking whether to
   Import history, Keep as backup, or Dismiss. All three ack the
   migration — only "Import history" actually runs `pan-agent migrate-office`.
4. Office tab works as before, now rendered from the embedded bundle.
5. Done.

## 3. Rollback to legacy Node sidecar

If the embedded Go adapter breaks for you (rare — covered by the
chaos tests + tauri-driver matrix — but possible on unusual
WebView2 configurations), switch back:

```yaml
# ~/.config/pan-agent/default/config.yaml
office:
  engine: node
```

Then restart pan-agent. The gateway proxies `/office/*` to a Node
sidecar on `office.node_port` (default 3000). You must have Node.js
installed and run the Claw3D dev server manually in that mode — we
do not auto-spawn it in 0.4.0.

To restore the embedded path:

```yaml
office:
  engine: go
```

Or use the doctor CLI:

```sh
pan-agent doctor --switch-engine=go
```

## 4. Config schema additions

Your existing `config.yaml` stays valid. 0.4.0 adds these optional keys
under the `office:` section:

```yaml
office:
  engine: go                              # go|node, default go
  node_port: 3000                         # used only when engine=node
  migration_ack: false                    # banner dismissal flag
  strict_origin: false                    # WS upgrade rejects empty Origin when true
  usage_log: true                         # record /v1/office/* hits
  windows_fallback: browser               # browser|none, default browser
  browser_fallback_until: "2026-04-22T..."  # RFC3339, set by fallback flow
  access_token: ""                        # gate loopback when non-loopback host
```

No key is required. Unset keys fall back to documented defaults.

## 5. SQLite schema changes

0.4.0 adds 5 new tables under `state.db`:

- `office_agents` (id, name, workspace, identity, role, created_at, updated_at)
- `office_sessions` (id, agent_id, state, settings, created_at, updated_at)
- `office_messages` (id, session_id, role, content, content_hash, created_at)
- `office_cron` (id, name, schedule, payload, enabled, last_run)
- `office_audit` (id, ts, actor, method, params_digest, result)

The `content_hash` column on `office_messages` is new — migrations
backfill it for existing rows via batched SQLite UPDATE with
`sha256(content)`. A separate lookup index on
`(session_id, content_hash)` was added — deliberately NOT unique
because legitimate duplicate messages (retries, acknowledgements) are
valid and should not be silently dropped.

## 6. API additions

New endpoints under `/v1/office/*`:

| Method | Path | Purpose |
|---|---|---|
| GET | `/v1/office/engine` | Current engine |
| POST | `/v1/office/engine` | Swap engine with drain |
| GET | `/v1/office/migration/status` | Banner-gating |
| POST | `/v1/office/migration/run` | Import legacy JSON |
| POST | `/v1/office/csp-report` | CSP violation collector (called by main.tsx) |
| POST | `/v1/office/fallback-detected` | WebView2 fallback trigger |

And the `/office/*` path serves the embedded bundle (static assets +
`/office/config.js` for runtime bootstrap + `/office/ws` for the
WebSocket).

## 7. Breaking changes

One. The `/api/gateway/ws` endpoint (upstream Claw3D contract) is now
`/office/ws`. This only affects users who were talking directly to
the WebSocket bypassing the Claw3D UI — a rare case. Redirect your
client to the new path.

## 8. Deprecations (removed in 0.5.0)

- `PAN_OFFICE_ENGINE` env var — replaced by `office.engine` config key.
  Still honored in 0.4.0 for override convenience; gone in 0.5.0.
- `/v1/office/setup|start|stop|logs` — the legacy lifecycle endpoints
  from the pre-embedded era. No-op since 0.4.0, removed in 0.5.0.

## 9. Getting help

- `pan-agent doctor --json` — diagnostic dump for support tickets
- `pan-agent doctor --csp-violations` — tail the CSP log
- `docs/runbook.md` — operator playbook
- `docs/protocol.md` — frozen WebSocket contract
