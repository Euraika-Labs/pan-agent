# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**Pan-Agent** — AI desktop agent with full PC control. A single Go binary (headless HTTP API + CLI) plus a Tauri/React desktop app that talks to it over `localhost:8642`.

- **Repo:** `git.euraika.net/euraika/pan-agent` (GitLab primary), `github.com/Euraika-Labs/pan-agent` (mirror used for Releases/CI)
- **Module path:** `github.com/euraika-labs/pan-agent`
- **Version:** backend `internal/version.Version` and `desktop/package.json` are kept in sync (currently `0.4.4`)
- **Go toolchain:** `go 1.25.7` (see `go.mod`) — routes use `ServeMux` `"METHOD /path"` syntax, no third-party router

## Build, Test, Run

```sh
# Backend — from repo root
go build ./...                              # compile everything
go build -o pan-agent ./cmd/pan-agent       # produce the binary
go run ./cmd/pan-agent serve --port 8642    # default subcommand: HTTP+SSE on :8642
go run ./cmd/pan-agent doctor               # env/config/DB/API-key health
go run ./cmd/pan-agent chat --model <id>    # CLI chat
go run ./cmd/pan-agent version              # build info

# Release-style build with version stamped in
go build -ldflags "-X github.com/euraika-labs/pan-agent/internal/version.Version=$(cat VERSION 2>/dev/null || echo dev) \
  -X github.com/euraika-labs/pan-agent/internal/version.Commit=$(git rev-parse --short HEAD)" \
  -o pan-agent ./cmd/pan-agent

# Tests
go test ./... -count=1 -timeout 120s        # full suite (skips chaos)
go test ./internal/approval/ -v             # single package
go test ./internal/storage/ -v -run TestCreateSession  # single test
go test -tags chaos ./internal/claw3d/      # chaos tests (opt-in build tag)

# OpenAPI / route drift guard — run after any gateway/routes change
bash scripts/verify-api.sh                  # diffs live routes vs docs/openapi.yaml

# Lint (CI runs golangci-lint v2 with govet, staticcheck, ineffassign, unused + gofmt/goimports)
gofmt -l .                                  # must be empty
goimports -l .                              # must be empty
golangci-lint run                           # if installed locally

# Desktop — from desktop/
npm ci
npm run dev:vite          # Vite only on :5173 (no Rust required)
npm run dev               # Vite + Tauri dev shell (needs Rust toolchain)
npm run typecheck         # tsc --noEmit
npm run lint              # ESLint over src/**/*.{ts,tsx}
npm run build:vite        # tsc -noEmit + vite build (web assets)
npm run build             # full Tauri build (installers)
npm run check:tauri       # cargo check on src-tauri
```

On Windows where Go is installed under the user profile, the canonical path is `C:\Users\<user>\go-sdk\go\bin\go.exe`. On Linux/macOS assume `go` is on `PATH`.

## Architecture

Monorepo. Go backend serves REST + SSE on `localhost:8642`. The Tauri/React app is one client among several (Telegram/Discord/Slack bots and any HTTP client also work). No IPC — everything goes over HTTP.

### Go backend (`cmd/` + `internal/`)

- **`cmd/pan-agent/main.go`** — CLI entry point. Subcommands `serve` (default), `chat`, `doctor`, `version`. Uses stdlib `flag` per subcommand.
- **`internal/gateway/`** — HTTP server using Go `ServeMux` method+path routing (`r.PathValue("id")` for params). `routes.go` wires endpoints; `chat.go` handles SSE streaming; `middleware.go` enforces CORS for `localhost:5173` (Vite) and `tauri://localhost`. `skill_agents.go` runs the bounded 10-turn reviewer/curator LLM loops. `office_csp.go` + `server.go` own Office/CSP logs and the PID file.
- **`internal/llm/`** — OpenAI-compatible streaming client. `client.go` parses SSE and accumulates tool-call deltas (coalescing index-incremented fragments). `providers.go` has base URLs for 9 providers. `types.go` defines `Message`, `ToolCall`, `StreamEvent`, `ToolDef`.
- **`internal/tools/`** — Tool interface (`Name`, `Description`, `Parameters`, `Execute`) with a global registry (`Register`/`Get`/`All`). Cross-platform pattern: shared `_common.go` + per-platform files (`_windows.go`, `_darwin.go`, `_linux.go`) + `_stub.go` for unsupported platforms; tools only register where they work. `skill_review_tool.go` and `skill_curator_tool.go` are agent-loop-only tools, never exposed directly via the gateway.
- **`internal/approval/`** — Regex command-safety classifier. `Classify(cmd) -> {Level, Pattern}`. Levels: `Safe` (0), `Dangerous` (1), `Catastrophic` (2). Catastrophic is checked before Dangerous. Since 0.4.2, this is wired into `internal/tools/code_execution.go` so LLM-supplied shell strings can't reach `exec.Command` without a UI round-trip.
- **`internal/storage/`** — Pure Go SQLite (`modernc.org/sqlite`, no CGo) with FTS5. Sessions, messages, skill usage log. Tests use `t.TempDir()` for isolated DB files.
- **`internal/config/`** — Profile-based configuration. `.env` parser, `config.yaml` reader, credential management, profile CRUD (`profiles.go`), diagnostics (`doctor.go`). API key resolution order: `REGOLO_API_KEY` > `OPENAI_API_KEY` > `API_KEY` > provider-specific env var.
- **`internal/skills/`** — Phase 11 self-healing skill system. Proposal queue under `<SkillsDir>/_proposed/<uuid>/`, history under `_history/<category>/<name>/`, plus `_archived/`, `_rejected/`, `_merged/`. Path containment is enforced via `filepath.Rel` in `resolveActiveDir` / `resolveProposalDir` / `resolveHistoryDir`. `guard.go` runs 30+ regex patterns across 6 categories (exec, fs, net, creds, obfuscation, prompt_injection) — proposals with `severity=block` findings are rejected before hitting disk. `curator.go` exposes `Propose{CuratorRefinement,Merge,Split,Archive,Recategorize}` which write proposals carrying an `Intent`; reviewer approval triggers `ApplyCuratorIntent`. Reviewer and curator persona contracts live in `embed/` and are compiled in via `go:embed`.
- **`internal/recovery/`** — Phase 12 WS2. Action journal + filesystem snapshots + per-tool reversers, exposed as `/v1/recovery/*`. Pure Go, shares the `*sql.DB` handle that `internal/storage` owns. `journal.go` records `Receipt`s with `ReceiptKind` / `ReversalStatus`; `snapshot*.go` does before/after capture with capability probes (`SnapshotTier`, `mountKey`, `capabilityCache`) and platform splits (`_linux.go`, `_darwin.go`, `_stub.go`, `snapshot_copy.go`); `reversers.go` + `reversers_shell_patterns.go` implement the undo registry with dependency-injected `ApprovalRequester` and `ShellExecFn` for testability; `reaper.go` drives execution; `endpoints.go` is the HTTP handler.
- **`internal/secret/`** — Phase 12 WS1. Two independent primitives: (1) keyring wrapper with distinct `ErrNotFound` / `ErrUnsupportedPlatform` / `ErrKeyringUnavailable` / `ErrInvalidKey` sentinels and a pluggable `backend` interface (`keyring_linux.go`, `keyring_darwin.go`, `keyring_windows.go`, `keyring_stub.go`, backed by `go-keyring` / `wincred`); (2) deterministic HMAC-SHA256 redaction pipeline (`redaction.go`, `redaction_patterns.go`) with Presidio-derived recognizers for email, AWS keys, API keys, etc. Load order matters — specific patterns (`AKIA…`) must run before generic ones (`API_KEY`). `Redact`, `RedactBytes`, `RedactWithMap` are the entry points.
- **`internal/parentwatch/`** — Graceful shutdown trigger for sidecar launches. Polls the parent PID every 2s and invokes an `onExit` callback when it's gone. Unix uses `syscall.Kill(pid, 0)` (kernel checks existence without delivering a signal); Windows uses `OpenProcess(SYNCHRONIZE)` + `WaitForSingleObject`. Conservative: permission errors return "alive" rather than killing the agent on a transient failure. Gated by an env var (typically `PAN_AGENT_PARENT_PID`) so CLI usage is unaffected.
- **`internal/paths/`** — Single source of truth for data paths. `AgentHome`, `ProfileHome`, plus per-file accessors (`EnvFile`, `ConfigFile`, `MemoryFile`, `UserFile`, `SoulFile`, `StateDB`, `ModelsFile`, `AuthFile`, `PidFile`, `CSPViolationsLog`, `SkillsDir`, `ProfileSkillsDir`). Directories are created lazily via `MkdirAll`. `ValidateProfile` enforces `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$` to prevent path traversal when profile names flow into FS paths. OS roots: Windows `%LOCALAPPDATA%\pan-agent`, macOS `~/Library/Application Support/pan-agent`, Linux `~/.local/share/pan-agent`.
- **`internal/claw3d/`** — 3D claw process manager. Platform-split (`process_windows.go`, `process_unix.go`). Chaos tests live under `-tags chaos`. Since 0.4.3, `migrate.go` canonicalises paths through `sanitizeMigrationPath` (`filepath.Clean` → `filepath.Abs`) before any FS syscall.
- **`internal/memory/`** — `MEMORY.md` + `USER.md` files with `§` delimiter and char limits.
- **`internal/persona/`** — `SOUL.md` persona system.
- **`internal/models/`** — Model library with remote provider sync (no hardcoded defaults since 0.3.x).
- **`internal/cron/`** — Scheduled jobs in `cron/jobs.json`.

### Desktop frontend (`desktop/`)

React 19 + Vite 8 + Tailwind 4 + Tauri v2. Screens in `src/screens/` (includes a Setup onboarding wizard). API client in `src/api.ts` uses `fetchJSON` and `streamSSE` helpers against `VITE_API_BASE` (default `http://localhost:8642`). First-run detection in `Layout.tsx` redirects to Setup when no LLM provider is configured. E2E tests in `desktop/tests/e2e/` and `desktop/tests/real-webview/` (Playwright).

## Key Design Decisions

- **HTTP API, not IPC.** The Tauri frontend is just a client. The agent is fully usable headless or from any language.
- **Pure-Go SQLite.** `modernc.org/sqlite` — no CGo, no C compiler.
- **go-rod for browser automation** over Chromium DevTools Protocol. Auto-downloads Chromium on first use.
- **Approval patterns in Go regex.** 103 patterns; Level 2 checked before Level 1; classifier is on the shell-exec code path.
- **Go 1.22+ routing.** `"METHOD /path"` mux strings, `r.PathValue("id")`, no third-party router.
- **Platform splits use file suffixes + build tags**, e.g. `keyboard_linux.go` / `keyboard_windows.go` / `snapshot_stub.go`. Stubs exist for every platform-specific package so `go build ./...` works everywhere.
- **Minimal dependencies.** Core: `modernc.org/sqlite`, `go-rod`, `uuid`, `screenshot`, `go-keyring`, `wincred`. Gateway bots: `telego`, `discordgo`, `slack-go`.
- **Least-privilege file modes.** Since 0.4.3, user-local files that don't need a group/world reader are 0o600 (PID file, CSP log) and backup dirs are 0o750.

## Contribution Rules (from AGENTS.md)

- **Conventional Commits** — history uses `feat(scope): ...`, `fix(security): ...`, `chore(release): ...`. Keep scopes short.
- **OpenAPI drift.** When you add or change an HTTP route, update `docs/openapi.yaml`, or add a documented exemption in `scripts/openapi-exempt.txt`. `scripts/verify-api.sh` enforces this.
- **PRs** — summary, changes, test plan, related issues. Add screenshots/recordings for visible UI changes. GitLab is primary; GitHub is a release/CI mirror.
- **Never commit** private Tauri signing keys, API keys, local `.env` files, or profile data.

## CI

- **GitLab CI** (`.gitlab-ci.yml`): `test:go` runs the Go suite, `test:desktop` runs typecheck + vite build, `build:binary` produces a versioned binary via ldflags. SAST + dependency-scan templates included.
- **GitHub Actions** (`.github/workflows/`): `ci.yml` builds+tests Go on Linux/Windows/macOS and the desktop typecheck+build; `release.yml` triggers on `v*` tags and produces signed installers (Windows NSIS/MSI, macOS DMG, Linux DEB/AppImage) plus `latest.json` for the Tauri auto-updater; `chaos.yml` and `e2e-real-webview.yml` carry explicit `permissions: contents: read` blocks (0.4.4 hardening).

## API Surface

`scripts/verify-api.sh` reports **56 live routes, 12 documented in `docs/openapi.yaml`, 44 exempt via `scripts/openapi-exempt.txt`**. All routes live under `/v1/`. Top-level resource groups (from `internal/gateway/routes.go`):

`approvals`, `chat`, `config` (includes profiles + doctor sub-paths), `cron`, `health`, `memory`, `models`, `office` (claw3d/hermes-office integration), `persona`, `recovery` (new in Phase 12), `sessions`, `skills` (list/install + Phase 11 proposals/history/usage/reviewer/curator), `tools`, plus `/v1/openapi.yaml` self-service.

Human-readable API docs: `docs/manual/00 - HTTP API Reference.md`. Machine-readable spec: `docs/openapi.yaml`.
