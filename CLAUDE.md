# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**Pan-Agent** — AI desktop agent with full PC control. Go backend + Tauri/React frontend. Replaces the Electron+Python stack (Pan Desktop + Hermes Agent).

- **Repo:** `git.euraika.net/euraika/pan-agent` (GitLab primary), `github.com/Euraika-Labs/pan-agent` (mirror)
- **Owner:** Bert Colemont (`bert@euraika.net`)
- **Status:** Phase 1-2 complete (agent core + tools). Phase 3 (Tauri desktop) scaffolded with all 14 screens migrated.

## Build & Run

```sh
# Go is at C:\Users\bertc\go-sdk\go\bin\go.exe (user-local install)
# Alias for convenience:
alias go='C:/Users/bertc/go-sdk/go/bin/go.exe'

# Build
go build -o pan-agent.exe ./cmd/pan-agent

# Run
./pan-agent serve --port 8642        # HTTP API server
./pan-agent doctor                    # Check config/DB/API key
./pan-agent chat                      # CLI chat mode
./pan-agent version                   # Print version

# Test
go test ./...                         # 60 tests across 5 packages

# Desktop (Tauri + React)
cd desktop && npm install && npm run dev    # Vite dev server on :5173
cd desktop && npx tauri dev                 # Full Tauri app (needs Rust)
```

## Architecture

Monorepo with Go backend + Tauri/React frontend:

```
cmd/pan-agent/main.go         CLI entry point (serve, chat, doctor, version)
internal/gateway/             HTTP API server (32 endpoints, SSE streaming chat)
internal/llm/                 OpenAI-compatible streaming client (9 providers)
internal/tools/               Tool implementations (terminal, filesystem, browser, web, code)
internal/approval/            103 dangerous command patterns (Level 1 + Level 2)
internal/storage/             SQLite with FTS5 (sessions, messages)
internal/config/              .env, config.yaml, credentials, platform toggles
internal/memory/              MEMORY.md + USER.md (§ delimiter, char limits)
internal/persona/             SOUL.md persona system
internal/models/              Model library + remote sync
internal/skills/              SKILL.md discovery + install/uninstall
internal/cron/                Scheduled task management
internal/paths/               Cross-platform path resolution
desktop/                      Tauri v2 + React frontend (14 screens)
```

## Key Design Decisions

- **HTTP API, not IPC:** The Go binary exposes REST+SSE on localhost. The Tauri frontend talks via fetch/EventSource. This means the agent works headless (CLI, server) and the UI is just one client.
- **Pure Go SQLite:** Uses `modernc.org/sqlite` (no CGo, no C compiler needed).
- **go-rod for browser:** Chromium DevTools Protocol via `github.com/go-rod/rod`. Auto-downloads Chromium on first use.
- **Approval patterns in Go regex:** All 103 patterns ported from Python. Level 2 (catastrophic) checked before Level 1 (dangerous).

## Data Paths

- **Windows:** `%LOCALAPPDATA%\pan-agent\`
- **macOS:** `~/Library/Application Support/pan-agent/`
- **Linux:** `~/.local/share/pan-agent/`

Files: `.env`, `config.yaml`, `state.db`, `MEMORY.md`, `USER.md`, `SOUL.md`, `models.json`, `auth.json`, `cron/jobs.json`, `skills/`

## Dependencies

- `modernc.org/sqlite` — pure Go SQLite
- `github.com/go-rod/rod` — browser automation
- `github.com/google/uuid` — UUID generation
- Everything else is stdlib

## Predecessor

Pan-Agent replaces Pan Desktop (Electron + Python Hermes Agent):
- Pan Desktop repo: `git.euraika.net/euraika/pan-desktop`
- Pan Desktop is still the shipping product until Pan-Agent reaches v1.0
- Data migration path documented in `docs/PAN_AGENT_REWRITE_PLAN.md` (in pan-desktop repo)
