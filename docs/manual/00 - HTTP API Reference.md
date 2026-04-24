# HTTP API Reference

Complete catalog of all 56 endpoints exposed by the Pan-Agent backend (as of v0.4.4 + unreleased Phase 12 foundation on `main`). Live count verified via `scripts/verify-api.sh`. Top-level resource groups: `approvals`, `chat`, `config`, `cron`, `health`, `memory`, `models`, `office`, `persona`, `recovery`, `sessions`, `skills`, `tools`.

## Conventions

| Item | Value |
|---|---|
| Base URL | `http://127.0.0.1:8642` |
| Auth | None |
| Content-Type | `application/json` (most endpoints) |
| Profile resolution | `?profile=<name>` query → server's startup profile |

Errors return `{"error": "message"}` with appropriate HTTP status codes.

## Chat & Approvals

### POST /v1/chat/completions
SSE streaming chat with the agent loop. Body:
```json
{
  "messages": [{"role": "user", "content": "..."}],
  "model": "optional-override",
  "stream": true,
  "tools": [],
  "session_id": "optional-resume"
}
```
Response: `text/event-stream`. Events: `chunk`, `tool_call`, `approval_required`, `tool_result`, `usage`, `error`, `done`. Stream terminates with `data: [DONE]\n\n`.

### POST /v1/chat/abort
Cancel in-flight generation. Body: `{"session_id": "..."}`. Returns 200 on cancel, 404 if no active stream.

### GET /v1/approvals
List pending approval requests. Returns array.

### GET /v1/approvals/{id}
Get a single approval by ID.

### POST /v1/approvals/{id}
Resolve an approval. Body: `{"approved": true|false}`. Returns 200, 404 (not found), or 409 (already resolved).

## Sessions

### GET /v1/sessions
List sessions. Query params: `?limit=50&offset=0&q=search`. With `q`, returns FTS5 search results with snippets.

### GET /v1/sessions/{id}
Get all messages for a session. Returns array of `{id, role, content, created_at}`.

## Models

### GET /v1/models
List the local model library. Returns array.

### POST /v1/models
Set the active model. Body: `{"provider": "...", "model": "...", "base_url": "..."}`. Refreshes the in-process LLM client.

### DELETE /v1/models/{id}
Remove a saved model from the library.

### POST /v1/models/sync
Fetch and cache the model list from a remote provider. Body: `{"provider": "...", "base_url": "...", "api_key": "..."}`.

## Configuration

### GET /v1/config
Returns:
```json
{
  "env": {"REGOLO_API_KEY": "sk-...", ...},
  "agentHome": "...",
  "model": {"provider": "...", "model": "...", "baseUrl": "..."},
  "credentialPool": {...},
  "appVersion": "0.4.4",
  "agentVersion": null
}
```

### PUT /v1/config
Union body — any combination:
```json
{
  "profile": "default",
  "env": {"KEY": "value"},
  "model": {"provider": "...", "model": "...", "baseUrl": "..."},
  "credentialPool": {"openrouter": [...]},
  "platformEnabled": {"telegram": true}
}
```

## Profiles

### GET /v1/config/profiles
List all profiles with metadata. Returns:
```json
{
  "profiles": [
    {
      "name": "default",
      "isDefault": true,
      "isActive": true,
      "model": "...",
      "provider": "...",
      "hasEnv": true,
      "hasSoul": false,
      "skillCount": 0,
      "gatewayRunning": false
    }
  ]
}
```

### POST /v1/config/profiles
Create a profile. Body: `{"name": "...", "cloneConfig": true}`. Returns `{"success": true}` or `{"success": false, "error": "..."}`.

Name validation: `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`. Cannot create "default".

### DELETE /v1/config/profiles/{name}
Delete a profile. Cannot delete "default". Returns `{"success": true}` or error.

## Diagnostics

### POST /v1/config/doctor
Run health checks. Returns `{"output": "pan-agent doctor\n----------------\n  [OK] ..."}`.

### POST /v1/config/update
Check for updates. Returns `{"available": false, "current": "0.4.4"}` (currently a stub — the real updater is the Tauri plugin).

## Memory

### GET /v1/memory
Read all memory entries. Returns the full `MEMORY.md` parsed.

### POST /v1/memory
Add an entry. Body: `{"content": "..."}`.

### PUT /v1/memory/{index}
Update entry at zero-based index. Body: `{"content": "..."}`.

### DELETE /v1/memory/{index}
Remove entry at zero-based index.

## Persona

### GET /v1/persona
Read the persona. Returns `{"persona": "..."}` (full SOUL.md content).

### PUT /v1/persona
Update the persona. Body: `{"persona": "..."}`.

### POST /v1/persona/reset
Reset to bundled default persona.

## Tools

### GET /v1/tools
List all registered tools. Returns array of `{name, description}`.

### PUT /v1/tools/{key}
Toggle a tool on/off. Currently always returns 200 (toggle state not yet persisted).

## Skills

### GET /v1/skills
List installed + bundled skills, combined. Reserved subdirs (`_proposed/`, `_archived/`, `_history/`, `_merged/`, `_rejected/`) are excluded; they're surfaced via the endpoints below.

### POST /v1/skills/install
Install a skill. Body: `{"id": "category/skill-name", "profile": "..."}`.

### POST /v1/skills/uninstall
Uninstall a skill. Body: `{"id": "category/skill-name", "profile": "..."}`.

## Skill Proposals (Phase 11)

The main agent proposes new/edited skills via the `skill_manage` tool — proposals land in `_proposed/<uuid>/` awaiting review rather than mutating active state directly. The curator agent also writes into this queue with a non-empty `intent` field (`refine`/`merge`/`split`/`archive`/`recategorize`).

### GET /v1/skills/proposals
Returns the queue, newest first. Each entry is `{metadata, content, guard_result, dir}` where `metadata` carries `id, name, category, description, trust_tier, created_by, created_at, source, status, intent, intent_targets, intent_new_category, intent_reason`, and `guard_result` is the 30+-pattern scan (`{blocked, findings, scanned_at, duration_ms}`).

### GET /v1/skills/proposals/{id}
Single proposal, same shape as above. Path `{id}` is the UUID.

### POST /v1/skills/proposals/{id}/approve
Promote a proposal to an active skill. Body (all optional): `{"refined_content": "...", "reviewer_note": "..."}`. If `refined_content` is supplied, it replaces the proposal body and re-runs the guard before write. Intent-aware:
- `create`/`refine`/`merge`: calls `PromoteProposal` (writes to `<category>/<name>/SKILL.md`, snapshots prior version to `_history/` if overwriting). `merge` also archives loser skills.
- `archive`/`recategorize`: no SKILL.md promotion — calls `ApplyCuratorIntent` directly (archive removes the target; recategorize renames the active dir). Proposal parked under `_rejected/` as "applied" for audit.
- `split`: materialises each child as a new active skill from the proposal's `split_children/` subdir, then archives the source.

Response: `{"status":"approved","approved":"<category>/<name>"}` or `{"status":"applied","intent":"archive|recategorize|split"}`.

### POST /v1/skills/proposals/{id}/reject
Move a proposal to `_rejected/<uuid>/` with metadata updated (`status=rejected`, `reject_reason=<body.reason>`). Body: `{"reason": "..."}` (required).

## Skill History (Phase 11)

Every write to an active skill snapshots the previous version to `_history/<category>/<name>/SKILL.<timestamp_ms>.md` so rollbacks are reversible.

### GET /v1/skills/history/{category}/{name}
Returns snapshot list newest first: `[{category, name, timestamp_ms, path}]`. Path is the absolute on-disk path to the snapshot.

### POST /v1/skills/history/{category}/{name}/rollback
Restore the active SKILL.md from a snapshot. Body: `{"timestamp_ms": 1234567890}` (required, must match an existing snapshot filename). The *current* active version is itself snapshotted to history before being overwritten, so rollbacks are reversible in both directions.

## Skill Usage (Phase 11)

Every call the main agent makes to `skill_view` or `skill_manage` is logged into the `skill_usage` SQLite table. The curator agent reads this to decide which skills are stale, failing, or worth refining.

### GET /v1/skills/usage/{category}/{name}
Recent usage rows for one skill, newest first. Query: `?limit=N` (default 50). Each row: `{id, session_id, skill_id, message_id, used_at, outcome, context_hint}`. Outcomes: `success`, `error`, `abandoned`, `unknown`.

### GET /v1/skills/usage/{category}/{name}/stats
Aggregate stats: `{skill_id, total_count, success_rate_pct, last_used_at}`.

## Skill Agents (Phase 11)

Both agent loops are bounded (10 turns max) and run synchronously on request. They use the model currently configured under `GET /v1/config` — override it with `PUT /v1/config` before running if you want a specific model.

### POST /v1/skills/reviewer/run
Fires one reviewer cycle: persona injection + current inventory + proposal queue → LLM with only the `skill_review` tool → process each proposal → reply. Response is a `SkillAgentReport`:
```json
{
  "agent": "reviewer",
  "profile": "<name>",
  "started_at": 1234567890123,
  "finished_at": 1234567891234,
  "turns": 3,
  "tool_calls": 5,
  "final_reply": "done — 2 approved, 1 refined, 1 rejected",
  "error": ""
}
```
Short-circuits with `final_reply: "no proposals queued"` when the queue is empty.

### POST /v1/skills/curator/run
Fires one curator cycle against the current active inventory + usage stats. Same `SkillAgentReport` shape. The curator writes proposals to the queue — they do NOT mutate active state until a subsequent reviewer run approves them.

## Cron

### GET /v1/cron
List scheduled jobs.

### POST /v1/cron
Create a job. Body: `{"name": "...", "schedule": "0 9 * * *", "prompt": "..."}`.

### DELETE /v1/cron/{id}
Delete a job.

## Health & Gateway

### GET /v1/health
Returns:
```json
{
  "gateway": false,
  "env": {"TELEGRAM_BOT_TOKEN": "...", ...},
  "platformEnabled": {"telegram": false, "discord": false, ...}
}
```

### POST /v1/health/gateway/start
Start messaging bots for all enabled platforms with valid tokens. Returns `{"status": "ok", "started": ["telegram", ...]}`.

### POST /v1/health/gateway/stop
Stop all messaging bots. Returns `{"status": "ok"}`.

## Claw3D

### GET /v1/claw3d/status
Get installation and process state. Returns `{installed, devServerRunning, adapterRunning, ...}`.

### POST /v1/claw3d/setup
Clone the upstream pan-office repo and run npm install. Streams progress as `application/x-ndjson`:
```
{"progress": "Cloning..."}
{"progress": "Running npm install..."}
{"done": true}
```

### POST /v1/claw3d/start
Start the dev server and adapter processes.

### POST /v1/claw3d/stop
Stop both processes.

## Recovery (Phase 12 foundation — unreleased on main)

`internal/recovery/` (WS2) landed post-0.4.4 as an action journal + rollback layer: every tool execution can be recorded into a SQLite-backed journal with filesystem / registry / browser-state snapshots, and the recorded entries can be replayed in reverse. Routes below are wired into the gateway but NOT yet invoked from tool execution — they're reachable, but on `main` the journal is still empty unless populated by tests. Scheduled to be wired into the tool dispatch path for v0.5.0.

### GET /v1/recovery/list
List journal entries. Query: `?session_id=...&limit=N` (default 50). Returns an array of journal rows (`{id, session_id, action_type, target, snapshot_ref, started_at, finished_at, status, error}`).

### POST /v1/recovery/undo
Reverse a journal entry. Body: `{"id": "..."}`. Dispatches to the registered reverser for that `action_type` (filesystem, registry, browser) and flips status to `undone`.

### GET /v1/recovery/diff
Compute a diff between an action's pre-snapshot and the current on-disk state. Query: `?id=<entry_id>`. Returns unified-diff text.

## Total: 56 endpoints

## Read next
- [[02 - HTTP API Surface]]
- [[01 - Quick Start]]
