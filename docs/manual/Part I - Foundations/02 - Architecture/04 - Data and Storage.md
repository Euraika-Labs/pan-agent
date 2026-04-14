# Data and Storage

This note describes where Pan-Agent stores everything on disk.

## AgentHome

The root directory for all Pan-Agent data. Resolved by `internal/paths/paths.go:AgentHome()` once per process via `sync.Once`.

| Platform | Path |
|---|---|
| Windows | `%LOCALAPPDATA%\pan-agent` |
| macOS | `~/Library/Application Support/pan-agent` |
| Linux | `~/.local/share/pan-agent` (XDG_DATA_HOME if set) |

The directory is created with mode `0700` on first access.

## Default profile vs named profiles

```
<AgentHome>/                       ← default profile root
├── .env                           ← API keys (default profile)
├── config.yaml                    ← model + platform config (default)
├── state.db                       ← SQLite database (shared across profiles)
├── MEMORY.md                      ← agent memory (default profile)
├── USER.md                        ← user info (default profile)
├── SOUL.md                        ← persona (default profile)
├── models.json                    ← model library cache
├── auth.json                      ← credential pool
├── cron/
│   └── jobs.json                  ← scheduled tasks
├── skills/                        ← installed skills (default profile)
│   └── <category>/<skill-name>/
├── logs/
├── cache/
├── pan-office/                    ← Claw3D clone
└── profiles/
    ├── work/
    │   ├── .env
    │   ├── config.yaml
    │   ├── MEMORY.md
    │   ├── SOUL.md
    │   └── skills/
    └── personal/
        └── ...
```

Note that `state.db`, `models.json`, `auth.json`, `cron/`, and `pan-office/` are global (shared across profiles). Sessions are stored in `state.db` regardless of which profile created them — sessions are not profile-scoped.

## SQLite schema

`internal/storage` opens `state.db` with `MaxOpenConns(1)` (modernc.org/sqlite is goroutine-safe but not parallel-safe).

Tables:

| Table | Purpose |
|---|---|
| `sessions` | One row per chat session: id, model, created_at, updated_at |
| `messages` | One row per message: id, session_id, role, content, created_at |
| `messages_fts` | FTS5 virtual table mirroring messages.content for full-text search |

The FTS5 index is maintained by triggers on `messages` insert/update/delete.

## .env format

Plain `KEY=value` lines, no quoting needed for simple values. Comments start with `#`.

```
REGOLO_API_KEY=sk-xxx
OPENAI_API_KEY=sk-xxx
TELEGRAM_BOT_TOKEN=123456:ABC-DEF
TELEGRAM_ALLOWED_USERS=12345,67890
DISCORD_BOT_TOKEN=xxx
SLACK_BOT_TOKEN=xoxb-xxx
SLACK_APP_TOKEN=xapp-xxx
```

Read by `config.ReadProfileEnv(profile)`. Cached in-memory with a 5-second TTL.

## config.yaml format

```yaml
provider: "regolo"
default: "Llama-3.3-70B-Instruct"
base_url: "https://api.regolo.ai/v1"
streaming: true
platforms:
  telegram:
    enabled: true
  discord:
    enabled: false
  slack:
    enabled: false
smart_model_routing:
  enabled: false
```

The YAML is parsed with regex (no full YAML library) for the specific keys Pan-Agent cares about. This is intentional — full YAML support would mean another dependency, and Pan-Agent only reads/writes a handful of well-known keys.

## API key resolution order

When the backend needs an API key, it checks (in this order):

1. `REGOLO_API_KEY` from profile `.env`
2. `OPENAI_API_KEY` from profile `.env`
3. `API_KEY` from profile `.env`
4. `OPENAI_API_KEY` from system environment

This order exists because Regolo.ai is the Euraika Labs partner provider, and its key is checked first to avoid accidental misconfiguration.

## auth.json (credential pool)

Structure:

```json
{
  "credential_pool": {
    "openrouter": [
      {"key": "sk-or-aaa", "label": "Work account"},
      {"key": "sk-or-bbb", "label": "Personal"}
    ],
    "anthropic": [
      {"key": "sk-ant-xxx", "label": "Default"}
    ]
  }
}
```

The credential pool is global (not per-profile). It's used for load balancing or rotating API keys across multiple accounts.

## File permissions

| File | Mode |
|---|---|
| Directories | `0700` |
| `.env`, `auth.json`, secrets | `0600` |
| Other files | `0644` |

On Windows, NTFS ACLs are used instead. The Go `os.Chmod` calls translate to ACL changes that grant only the current user access.

## What is NOT stored

- **No telemetry**. Pan-Agent does not phone home.
- **No analytics**. No usage tracking, no error reporting (unless you wire up Sentry yourself).
- **No bundled model weights**. All inference is via external APIs.

## Operator rule
Backups should include `<AgentHome>/state.db`, all `.env` files, all `config.yaml` files, and `MEMORY.md` / `USER.md` / `SOUL.md`. Skills can be reinstalled. Models are re-synced. Logs and cache can be discarded.

## Read next
- [[06 - Storage Layer]]
- [[07 - Profile System]]
- [[04 - Configuration Reference]]
