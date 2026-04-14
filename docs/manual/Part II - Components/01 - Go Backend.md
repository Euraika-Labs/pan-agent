# Go Backend

The Go backend is the core of Pan-Agent. It compiles to a single binary with no runtime dependencies.

## Quick reference

| Item | Value |
|---|---|
| Module path | `github.com/euraika-labs/pan-agent` |
| Go version | 1.25.0+ |
| Binary name | `pan-agent` (`.exe` on Windows) |
| Default port | 8642 |
| Test count | 72 across 6 packages |

## Direct dependencies

| Dependency | Purpose |
|---|---|
| `modernc.org/sqlite` | Pure Go SQLite (no CGo for the DB) |
| `github.com/go-rod/rod` | Browser automation via Chromium DevTools Protocol |
| `github.com/google/uuid` | UUID generation |
| `github.com/kbinani/screenshot` | Cross-platform screen capture |
| `github.com/jezek/xgb` | X11 protocol (Linux PC control) |
| `github.com/mymmrac/telego` | Telegram bot library |
| `github.com/bwmarrin/discordgo` | Discord bot library |
| `github.com/slack-go/slack` | Slack bot library |

## Build

```bash
# Local development
go build -o pan-agent.exe ./cmd/pan-agent

# With version stamping (used by CI)
go build -ldflags "-X github.com/euraika-labs/pan-agent/internal/version.Version=0.2.0 \
                   -X github.com/euraika-labs/pan-agent/internal/version.Commit=$(git rev-parse --short HEAD) \
                   -X github.com/euraika-labs/pan-agent/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
         -o pan-agent.exe ./cmd/pan-agent

# Tests
go test ./... -count=1 -timeout 120s
```

On macOS in CI: `MACOSX_DEPLOYMENT_TARGET=14.0` must be set or the screenshot package fails to compile against the macOS 15 SDK.

## CLI subcommands

The entry point at `cmd/pan-agent/main.go` dispatches by first arg:

- `pan-agent serve` (default) — starts the HTTP server
- `pan-agent chat` — interactive terminal chat
- `pan-agent doctor` — runs health checks
- `pan-agent version` — prints version info

Each subcommand has its own `flag.FlagSet` with subcommand-specific flags (`--port`, `--profile`, `--model`, etc.).

## Build tags in use

| Tag | Files | Purpose |
|---|---|---|
| `windows` | `*_windows.go` | Windows-specific PC control implementations |
| `darwin` | `*_darwin.go` | macOS-specific PC control (CGo) |
| `linux` | `*_linux.go` | Linux-specific PC control (X11) |
| `!windows && !darwin && !linux` | `*_stub.go` | Stub for unsupported platforms |

The screenshot and OCR tools have NO build tag — they use `kbinani/screenshot` which handles platform detection internally.

## Adding a new tool

1. Create `internal/tools/<name>.go` (or split with build tags if platform-specific).
2. Define a struct: `type MyTool struct{}`
3. Implement the four `Tool` interface methods: `Name()`, `Description()`, `Parameters()`, `Execute()`.
4. Add `func init() { Register(MyTool{}) }`.
5. The tool is now visible to the LLM. No frontend changes needed.

## Read next
- [[02 - Tauri Desktop Frontend]]
- [[03 - LLM Client and Providers]]
- [[04 - Tool Registry]]
