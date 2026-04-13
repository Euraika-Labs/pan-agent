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
│   ├── config/             .env, YAML, credentials, platforms
│   ├── memory/             MEMORY.md + USER.md (§ delimiter)
│   ├── persona/            SOUL.md persona system
│   ├── models/             model library + remote sync
│   ├── skills/             SKILL.md discovery + install
│   ├── cron/               scheduled task management
│   └── paths/              cross-platform path resolution
├── desktop/                Tauri + React frontend
│   ├── src-tauri/          Rust WebView2 shim
│   ├── src/                14 React screens (migrated from Electron)
│   └── package.json
└── go.mod
```

## API Endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/v1/chat/completions` | SSE streaming chat |
| POST | `/v1/chat/abort` | Cancel generation |
| POST | `/v1/approvals/{id}` | Resolve approval |
| GET | `/v1/sessions` | List sessions |
| GET | `/v1/sessions/{id}` | Session messages |
| GET | `/v1/models` | List models |
| POST | `/v1/models` | Add model |
| GET | `/v1/memory` | Read memory |
| POST | `/v1/memory` | Add entry |
| GET | `/v1/persona` | Read persona |
| PUT | `/v1/persona` | Write persona |
| GET | `/v1/tools` | List toolsets |
| GET | `/v1/skills` | List skills |
| GET | `/v1/cron` | List cron jobs |
| GET | `/v1/health` | Health check |

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
| Binary size | 15.3 MB |
| Go files | 39 |
| Go lines | 7,221 |
| Go tests | 60 |
| React screens | 14 |
| HTTP endpoints | 32 |
| Approval patterns | 103 |
| Supported providers | 9 |
