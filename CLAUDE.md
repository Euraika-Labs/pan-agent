# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**Pan-Agent** — AI desktop agent with full PC control. Go backend + Tauri/React frontend. Replaces the Electron+Python stack (Pan Desktop).

- **Repo:** `git.euraika.net/euraika/pan-agent` (GitLab primary), `github.com/Euraika-Labs/pan-agent` (mirror)
- **Owner:** Bert Colemont (`bert@euraika.net`)
- **Status:** Phase 1-7 complete. Agent core, tools, Tauri desktop (15 screens), PC control, onboarding wizard, profile CRUD, config/health API parity with frontend.
- **Version:** 0.1.1 (tagged, released on GitHub)

## Build & Run

```sh
# Go is at C:\Users\bertc\go-sdk\go\bin\go.exe (user-local install)
alias go='C:/Users/bertc/go-sdk/go/bin/go.exe'

# Build
go build -o pan-agent.exe ./cmd/pan-agent

# Build with version info (used in CI)
go build -ldflags "-X github.com/euraika-labs/pan-agent/internal/version.Version=0.1.1 \
  -X github.com/euraika-labs/pan-agent/internal/version.Commit=$(git rev-parse --short HEAD)" \
  -o pan-agent.exe ./cmd/pan-agent

# Run
./pan-agent serve --port 8642        # HTTP API server (default subcommand)
./pan-agent doctor                    # Check config/DB/API key health
./pan-agent chat --model kimi-k2-0905 # CLI chat mode
./pan-agent version                   # Print version

# Test — all packages
go test ./... -count=1 -timeout 120s

# Test — single package
go test ./internal/approval/ -v
go test ./internal/storage/ -v -run TestCreateSession

# Desktop (Tauri + React)
cd desktop && npm install && npm run dev:vite    # Vite dev server only on :5173
cd desktop && npm run dev                        # Vite + Tauri dev (needs Rust)
cd desktop && npx tsc --noEmit                   # TypeScript typecheck
```

## Architecture

Monorepo: Go backend serves REST+SSE on localhost:8642. The Tauri/React frontend is one client — the agent also works headless via CLI or any HTTP client.

### Go backend (`cmd/` + `internal/`)

- **`cmd/pan-agent/main.go`** — CLI entry point. Subcommands: `serve` (default), `chat`, `doctor`, `version`. Uses stdlib `flag` per subcommand.
- **`internal/gateway/`** — HTTP server using Go 1.22+ `ServeMux` pattern routing (`"METHOD /path"`). Routes defined in `routes.go`, chat SSE streaming in `chat.go`, CORS middleware in `middleware.go`. Allowed origins: `localhost:5173` (Vite) and `tauri://localhost`.
- **`internal/llm/`** — OpenAI-compatible streaming client. `client.go` implements SSE parsing with tool-call accumulation. `providers.go` has base URLs for 9 providers. `types.go` defines `Message`, `ToolCall`, `StreamEvent`, `ToolDef`.
- **`internal/tools/`** — Tool interface (`Name`, `Description`, `Parameters`, `Execute`) with global registry (`Register`/`Get`/`All`). Each file is one tool: `terminal.go`, `filesystem.go`, `browser.go`, `screenshot.go`, `keyboard.go`, `mouse.go`, `ocr.go`, `window_manager.go`, etc.
- **`internal/approval/`** — Regex-based command safety classification. `Check(cmd) -> {Level, Pattern}`. Three levels: `Safe` (0), `Dangerous` (1), `Catastrophic` (2). Catastrophic checked before Dangerous.
- **`internal/storage/`** — Pure Go SQLite (`modernc.org/sqlite`) with FTS5. Sessions and messages. Tests use `t.TempDir()` for isolated DB files.
- **`internal/config/`** — Profile-based configuration. `.env` parser, `config.yaml` reader, credential management, profile CRUD (`profiles.go`), diagnostics (`doctor.go`). API key resolution order: `REGOLO_API_KEY` > `OPENAI_API_KEY` > `API_KEY` > env var.
- **`internal/memory/`** — MEMORY.md + USER.md files with `§` delimiter and character limits.
- **`internal/persona/`** — SOUL.md persona system.
- **`internal/models/`** — Model library with remote sync.
- **`internal/claw3d/`** — 3D claw process management (platform-specific: `process_windows.go`, `process_unix.go`).

### Desktop frontend (`desktop/`)

React 19 + Vite 7 + Tailwind CSS 4 + Tauri v2. 15 screens in `src/screens/` (including Setup onboarding wizard). API client in `src/api.ts` using `fetchJSON` and `streamSSE` helpers against `VITE_API_BASE` (defaults to `http://localhost:8642`). First-run detection in `Layout.tsx` redirects to Setup when no LLM provider is configured.

## Key Design Decisions

- **HTTP API, not IPC:** The Go binary exposes REST+SSE on localhost. The Tauri frontend talks via fetch/EventSource. This means the agent works headless and the UI is just one client.
- **Pure Go SQLite:** Uses `modernc.org/sqlite` — no CGo, no C compiler needed.
- **go-rod for browser:** Chromium DevTools Protocol via `github.com/go-rod/rod`. Auto-downloads Chromium on first use.
- **Approval patterns in Go regex:** 103 patterns ported from Python. Level 2 (catastrophic) checked before Level 1 (dangerous).
- **Go 1.22+ routing:** Routes use `"METHOD /path"` syntax with `r.PathValue("id")` for path params. No third-party router.
- **Minimal dependencies:** Four direct deps (sqlite, rod, uuid, screenshot). Everything else is stdlib.

## Data Paths

- **Windows:** `%LOCALAPPDATA%\pan-agent\`
- **macOS:** `~/Library/Application Support/pan-agent/`
- **Linux:** `~/.local/share/pan-agent/`

Files: `.env`, `config.yaml`, `state.db`, `MEMORY.md`, `USER.md`, `SOUL.md`, `models.json`, `auth.json`, `cron/jobs.json`, `skills/`

## CI

GitLab CI (`.gitlab-ci.yml`): `test:go` runs `go test ./...`, `test:desktop` runs `tsc --noEmit` + `vite build`, `build:binary` produces versioned binary with ldflags. Includes SAST and dependency scanning templates.

GitHub CI (`.github/workflows/ci.yml`): Go build/test on Linux+Windows+macOS, desktop typecheck+build, Tauri build on Windows. Releases published on GitHub via `gh release create` with versioned binary.

## API Overview

40 endpoints across 10 resource groups. Key additions in v0.1.1:

- `GET /v1/config` returns structured `ConfigResponse` (env, agentHome, model, credentialPool, appVersion, agentVersion)
- `PUT /v1/config` accepts union body with optional `env`, `model`, `credentialPool`, `platformEnabled` fields
- `GET /v1/health` returns `{gateway, env, platformEnabled}` (not just `{status: ok}`)
- `GET/POST/DELETE /v1/config/profiles` — profile CRUD with path traversal protection
- `POST /v1/config/doctor` — run diagnostics via HTTP (same checks as CLI `doctor`)
- `POST /v1/health/gateway/start` and `/stop` — messaging gateway toggle (stubs)
