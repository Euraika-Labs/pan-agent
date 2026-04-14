# HTTP API Reference

Complete catalog of all 43 endpoints exposed by the Pan-Agent backend.

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
  "appVersion": "0.2.0",
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
Check for updates. Returns `{"available": false, "current": "0.2.0"}` (currently a stub — the real updater is the Tauri plugin).

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
List installed + bundled skills, combined.

### POST /v1/skills/install
Install a skill. Body: `{"id": "category/skill-name", "profile": "..."}`.

### POST /v1/skills/uninstall
Uninstall a skill. Body: `{"id": "category/skill-name", "profile": "..."}`.

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

## Total: 43 endpoints

## Read next
- [[02 - HTTP API Surface]]
- [[01 - Quick Start]]
