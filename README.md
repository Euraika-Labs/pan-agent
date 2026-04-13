# Pan-Agent

AI desktop agent with full PC control. Single Go binary + Tauri desktop app.

## Quick Start

```sh
# Build the agent
go build -o pan-agent ./cmd/pan-agent

# Run the HTTP API server
./pan-agent serve --port 8642

# Health check
curl http://localhost:8642/v1/health

# System diagnostics
./pan-agent doctor

# Interactive CLI chat
./pan-agent chat --model kimi-k2-0905
```

## Architecture

```
pan-agent/
├── cmd/pan-agent/          Go CLI (serve, chat, doctor, version)
├── internal/
│   ├── gateway/            HTTP API (32 endpoints + SSE streaming)
│   ├── llm/                OpenAI-compatible client (9 providers)
│   ├── tools/              terminal, filesystem, browser, web search, code execution
│   ├── approval/           103 dangerous command patterns (Level 1 + Level 2)
│   ├── storage/            SQLite sessions + FTS5 search
│   ├── config/             .env, YAML, credentials, platforms, profiles, doctor
│   ├── memory/             MEMORY.md + USER.md (§ delimiter)
│   ├── persona/            SOUL.md persona system
│   ├── models/             model library + remote sync
│   ├── skills/             SKILL.md discovery + install
│   ├── cron/               scheduled task management
│   └── paths/              cross-platform path resolution
├── desktop/                Tauri + React frontend
│   ├── src-tauri/          Rust WebView2 shim
│   ├── src/                15 React screens (incl. Setup onboarding wizard)
│   └── package.json
└── go.mod
```

## API Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/v1/chat/completions` | SSE streaming chat |
| POST | `/v1/chat/abort` | Cancel generation |
| POST | `/v1/approvals/{id}` | Resolve approval |
| GET | `/v1/approvals` | List pending approvals |
| GET | `/v1/sessions` | List sessions |
| GET | `/v1/sessions/{id}` | Session messages |
| GET | `/v1/models` | List models |
| POST | `/v1/models` | Set active model |
| DELETE | `/v1/models/{id}` | Remove model |
| POST | `/v1/models/sync` | Sync remote models |
| GET | `/v1/config` | Full config (env, model, pool) |
| PUT | `/v1/config` | Update config (union body) |
| GET | `/v1/config/profiles` | List profiles |
| POST | `/v1/config/profiles` | Create profile |
| DELETE | `/v1/config/profiles/{name}` | Delete profile |
| POST | `/v1/config/doctor` | Run diagnostics |
| POST | `/v1/config/update` | Check for updates |
| GET | `/v1/memory` | Read memory |
| POST | `/v1/memory` | Add entry |
| PUT | `/v1/memory/{index}` | Update entry |
| DELETE | `/v1/memory/{index}` | Remove entry |
| GET | `/v1/persona` | Read persona |
| PUT | `/v1/persona` | Write persona |
| POST | `/v1/persona/reset` | Reset to default |
| GET | `/v1/tools` | List toolsets |
| PUT | `/v1/tools/{key}` | Toggle tool |
| GET | `/v1/skills` | List skills |
| POST | `/v1/skills/install` | Install skill |
| POST | `/v1/skills/uninstall` | Uninstall skill |
| GET | `/v1/cron` | List cron jobs |
| POST | `/v1/cron` | Create cron job |
| DELETE | `/v1/cron/{id}` | Delete cron job |
| GET | `/v1/health` | Health + gateway status |
| POST | `/v1/health/gateway/start` | Start messaging gateway |
| POST | `/v1/health/gateway/stop` | Stop messaging gateway |

## Desktop App

```sh
cd desktop
npm install
npm run dev        # Vite dev server on :5173

# Full Tauri build (requires Rust)
npx tauri dev      # Dev mode with hot reload
npx tauri build    # Production NSIS + MSI installer
```

## Providers

OpenAI, Anthropic, Regolo, OpenRouter, Groq, Ollama, LM Studio, vLLM, llama.cpp

## Stats

| Metric | Value |
|---|---|
| Version | 0.2.0 |
| Go test functions | 72 |
| React screens | 15 |
| HTTP endpoints | 43 |
| Platforms | Windows, macOS, Linux |
| Approval patterns | 103 |
| Supported providers | 9 |
