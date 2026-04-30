<div align="center">

<img src="panagent.png" alt="Pan-Agent" width="128" height="128">

# Pan-Agent

**An AI desktop agent with full PC control.**
A single Go binary plus a Tauri desktop app. Cross-platform. Open source.

[![CI](https://github.com/Euraika-Labs/pan-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/Euraika-Labs/pan-agent/actions/workflows/ci.yml)
[![Release](https://github.com/Euraika-Labs/pan-agent/actions/workflows/release.yml/badge.svg)](https://github.com/Euraika-Labs/pan-agent/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Latest Release](https://img.shields.io/github/v/release/Euraika-Labs/pan-agent)](https://github.com/Euraika-Labs/pan-agent/releases/latest)

[**Download**](https://github.com/Euraika-Labs/pan-agent/releases/latest) ·
[**Manual**](docs/manual/00%20-%20Table%20of%20Contents.md) ·
[**Quick Start**](docs/manual/Part%20I%20-%20Foundations/00%20-%20Start%20Here/01%20-%20Quick%20Start.md) ·
[**API Reference**](docs/manual/00%20-%20HTTP%20API%20Reference.md)

</div>

---

## What is Pan-Agent?

Pan-Agent is a self-hosted AI assistant that runs on your own machine. It can:

- 💬 **Chat** with any OpenAI-compatible LLM (OpenAI, Anthropic, Regolo, OpenRouter, Groq, Ollama, LM Studio, vLLM, llama.cpp)
- 🖥️ **Control your PC** — take screenshots, type, click, manage windows, run commands, browse the web
- 🛡️ **Stay safe** with a 103-pattern approval system that blocks catastrophic commands and asks before dangerous ones
- 📨 **Be reachable** via Telegram, Discord, or Slack bots — chat with your agent from anywhere
- 💾 **Remember** conversations across sessions with persistent memory and multiple isolated profiles
- 🔌 **Run headless** as an HTTP API on `localhost:8642`, scriptable from any language

**One binary. No Python. No Node.js runtime. No Docker. No telemetry.**

---

## Why Pan-Agent?

| Feature | Pan-Agent | Most AI desktop apps |
|---|---|---|
| Open source (MIT) | ✅ | Often closed |
| Single binary | ✅ Pure Go | ❌ Electron + Python + venv |
| Cross-platform | ✅ Windows, macOS, Linux | Often Windows-only |
| Local LLM support | ✅ 4 backends preconfigured | Often cloud-only |
| Headless / API-first | ✅ HTTP API, scriptable | Often GUI-only |
| Messaging bots | ✅ Telegram + Discord + Slack | Rare |
| Auto-update | ✅ Signed Tauri updater | Often manual |
| No telemetry | ✅ Zero phone-home | Common to leak data |

Pan-Agent is built for people who want to own their AI agent — full control, no vendor lock-in, no surprises in the binary.

---

## Quick Start

### 1. Download

Grab the installer for your platform from [GitHub Releases](https://github.com/Euraika-Labs/pan-agent/releases/latest):

| Platform | File | Notes |
|---|---|---|
| **Windows** | `Pan.Desktop_*_x64-setup.exe` | NSIS installer (unsigned — see [SmartScreen note](#windows-smartscreen)) |
| **macOS ARM** | `Pan.Desktop_*_aarch64.dmg` | Drag to Applications. Right-click → Open on first launch. |
| **Linux** | `Pan.Desktop_*_amd64.AppImage` | `chmod +x` then run. Also `.deb` for apt systems. |

> **No installer for your platform?** Build from source — see [Development](#development).

### 2. Launch and Set Up

Open the app. The Setup Wizard appears on first launch.

1. **Pick a provider** — OpenRouter (recommended), Anthropic, OpenAI, Regolo, a local LLM, or a custom endpoint.
2. **Paste your API key** — or skip if you're running a local LLM with no auth.
3. **Click Continue** — the agent fetches your model list and you go straight to Chat.

That's it. You're talking to an AI that can use tools.

### 3. Try it

Prompts to get a feel for what Pan-Agent can do:

```
"What's in my Downloads folder? Sort by size."
→ Uses filesystem tool to list and sort files

"Take a screenshot and tell me what app is in the foreground."
→ Uses screenshot + window_manager tools

"Search the web for the latest Tauri release notes and summarize."
→ Uses web_search tool

"Open Notepad, type 'Hello world', and save it as test.txt"
→ Uses window_manager + keyboard tools

"Schedule a daily 9am summary of yesterday's git commits."
→ Creates a cron job
```

---

## Headless Usage

Pan-Agent ships two command-line binaries:

- `pan` - user-facing terminal chat CLI.
- `pan-agent` - backend/server CLI for the local HTTP API and desktop sidecar.

### Download The CLI

Download the standalone CLI binary from
[GitHub Releases](https://github.com/Euraika-Labs/pan-agent/releases/latest).

| Platform | File |
|---|---|
| **Windows** | `pan-0.6.5-windows-x64.exe` |
| **macOS ARM** | `pan-0.6.5-macos-arm64` |
| **Linux x64** | `pan-0.6.5-linux-x64` |

Install examples:

```powershell
# Windows PowerShell
mkdir $HOME\.local\bin -Force
Copy-Item .\pan-0.6.5-windows-x64.exe $HOME\.local\bin\pan.exe
pan help
```

```bash
# macOS / Linux
mkdir -p ~/.local/bin
cp ./pan-0.6.5-linux-x64 ~/.local/bin/pan
chmod +x ~/.local/bin/pan
pan help
```

Make sure `~/.local/bin` is on your `PATH`. On Windows you can also put
`pan.exe` in any directory already on `PATH`.

### `pan` Terminal Chat

`pan` uses the same profile configuration as Pan Desktop. If you already saved
an API key and model in Settings, the CLI picks it up automatically.

```bash
# Start interactive terminal chat
pan

# Same thing, explicit command
pan chat

# Single prompt mode for scripts
pan -z "Summarize this repository in three bullets"
pan chat -z "Write a short haiku"

# Override model/provider/profile for one invocation
pan -m gpt-4o-mini
pan --provider regolo
pan --profile work
```

Built-in commands:

```bash
pan help                  # Full command list
pan chat --help           # Help for chat
pan model                 # Show active model/provider/base URL
pan status                # Show CLI/provider/key status
pan config                # Show active profile config used by the CLI
pan doctor                # Run basic config checks
pan version               # Print version metadata
```

Inside interactive chat:

```text
/help       show in-chat commands
/model      show active model
/profile    show active profile
/clear      clear the screen and reset chat history
/exit       quit
```

You can also create a custom launcher name:

```bash
pan configure profile hello
hello
```

When launched as `pan`, the assistant label is `PAN`. When launched through a
custom alias like `hello`, the assistant label uses that alias. Running
`pan configure profile <new-name>` again replaces the previous Pan-created
alias.

### `pan-agent` Server CLI

Use `pan-agent` when you want the HTTP API server, diagnostics, or the lower
level backend commands:

```bash
# Start the HTTP API server
pan-agent serve --port 8642

# Interactive terminal chat (legacy backend CLI)
pan-agent chat --model gpt-4o-mini

# Run health checks
pan-agent doctor

# Print version
pan-agent version
```

The HTTP API has 50 endpoints — see the [HTTP API Reference](docs/manual/00%20-%20HTTP%20API%20Reference.md) for the full catalog.

Quick examples:

```bash
# Health check
curl http://localhost:8642/v1/health | jq

# Send a chat message
curl -N http://localhost:8642/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Hello"}],"stream":true}'

# List configured models
curl http://localhost:8642/v1/models | jq

# Create a profile
curl -X POST http://localhost:8642/v1/config/profiles \
  -H "Content-Type: application/json" \
  -d '{"name":"work","cloneConfig":true}'
```

---

## Features

### 💬 Chat with streaming
Server-Sent Events for token-by-token rendering. Multi-turn agent loop with tool execution. Sessions persist in SQLite with FTS5 full-text search.

### 🛠️ 20+ tools out of the box
| Category | Tools |
|---|---|
| **Core** | terminal, filesystem, code_execution, browser (Chromium), web_search |
| **PC control** | screenshot, keyboard, mouse, window_manager, OCR |
| **AI** | vision, image_gen, tts, clarify, delegation, mixture-of-agents |
| **Utility** | memory, session_search, todo, cron |

PC control tools work natively on Windows (user32), macOS (CoreGraphics), and Linux (X11 XTest + EWMH).

### 🛡️ Approval system
103 regex patterns classify commands into Safe / Dangerous / Catastrophic. Dangerous commands trigger an interactive approval modal. Catastrophic commands (`vssadmin delete shadows`, `format C:`, `mimikatz`) are blocked by default.

### 👤 Profiles
Isolated environments with their own API keys, model config, persona, memory, and skills. Switch contexts without losing setup.

### 📨 Messaging gateway
Connect to Telegram, Discord, or Slack with built-in bot support. Long polling / WebSocket / Socket Mode — no public URL needed. Each chat maps to a persistent session for conversation continuity.

### ⏰ Scheduled tasks
Cron expressions trigger agent runs. Daily briefings, hourly checks, weekly reviews — anything an AI agent could do, scheduled.

### 🔄 Auto-update
Tauri updater plugin verifies Ed25519 signatures before installing updates. No manual reinstallation.

### 🧠 Memory & Persona
`MEMORY.md` for cross-session facts. `SOUL.md` for the agent's identity and standing instructions. Both per-profile.

### 📦 Skills
Drop-in capability bundles via `SKILL.md` files. Extend the agent's abilities without code changes.

### 🏢 Office (Claw3D)
Embedded document/spreadsheet/presentation editor (via the [pan-office](https://github.com/Euraika-Labs/pan-office) project) for AI-assisted writing.

---

## Documentation

The full manual is in [`docs/manual/`](docs/manual/) — 42 documents organized in four parts:

| Part | What's in it |
|---|---|
| [**I — Foundations**](docs/manual/Part%20I%20-%20Foundations/) | Quick Start, System Overview, Architecture, HTTP API Surface |
| [**II — Components**](docs/manual/Part%20II%20-%20Components/) | Go Backend, Tauri Frontend, LLM Client, Tool Registry, Approval, Storage, Profiles, Bots |
| [**III — Operations**](docs/manual/Part%20III%20-%20Operations/) | Installation, Build pipeline, release readiness checklist, Auto-update, Config reference, Security + Troubleshooting runbooks |
| [**IV — User Guide**](docs/manual/Part%20IV%20-%20User%20Guide/) | Chat, Tools, Profiles, Models, Memory, Skills, Schedules, Office |

**Where to start:**
- 🆕 New users → [Quick Start](docs/manual/Part%20I%20-%20Foundations/00%20-%20Start%20Here/01%20-%20Quick%20Start.md)
- 👤 End users → [Chat](docs/manual/Part%20IV%20-%20User%20Guide/01%20-%20Chat.md) → [Tools Catalog](docs/manual/Part%20IV%20-%20User%20Guide/02%20-%20Tools%20Catalog.md)
- 🧰 Operators → [Installation](docs/manual/Part%20III%20-%20Operations/01%20-%20Installation%20and%20First%20Run.md) → [Configuration](docs/manual/Part%20III%20-%20Operations/04%20-%20Configuration%20Reference.md)
- 👨‍💻 Developers → [System Overview](docs/manual/Part%20I%20-%20Foundations/01%20-%20Platform%20Overview/01%20-%20System%20Overview.md) → [Service Architecture](docs/manual/Part%20I%20-%20Foundations/02%20-%20Architecture/01%20-%20Service%20Architecture.md)
- 🤖 API users → [HTTP API Reference](docs/manual/00%20-%20HTTP%20API%20Reference.md)
- 🐛 Troubleshooting → [Troubleshooting Index](docs/manual/Part%20III%20-%20Operations/00%20-%20Troubleshooting%20Index.md)

---

## Architecture

```
┌────────────────────────────────────────────────────────┐
│  Desktop App (Tauri v2 + React 19)                    │
│  15 screens — Chat, Setup, Profiles, Models,          │
│  Memory, Soul, Skills, Tools, Schedules, Gateway,     │
│  Sessions, Settings, Office, Search, Layout           │
└────────────────────┬───────────────────────────────────┘
                     │ HTTP + SSE (fetch / EventSource)
                     │
┌────────────────────▼───────────────────────────────────┐
│  Go Backend (single binary)                           │
│  • HTTP server on localhost:8642 (50 endpoints)       │
│  • OpenAI-compatible streaming LLM client             │
│  • 20+ registered tools (cross-platform)              │
│  • Approval system (103 patterns)                     │
│  • SQLite storage (FTS5)                              │
│  • Profile-based config (.env + config.yaml)          │
│  • Telegram + Discord + Slack bot goroutines         │
└────────────────────┬───────────────────────────────────┘
                     │
                     ├─► LLM Provider (OpenAI / Anthropic / Regolo / Ollama / ...)
                     ├─► OS APIs (user32 / CoreGraphics / X11 XTest)
                     └─► Messaging Platforms (Telegram / Discord / Slack)
```

For the full architecture deep-dive, see [System Overview](docs/manual/Part%20I%20-%20Foundations/01%20-%20Platform%20Overview/01%20-%20System%20Overview.md) and [Service Architecture](docs/manual/Part%20I%20-%20Foundations/02%20-%20Architecture/01%20-%20Service%20Architecture.md).

---

## Repository Layout

```
pan-agent/
├── cmd/pan-agent/          Go CLI entry point (serve, chat, doctor, version)
├── internal/
│   ├── gateway/            HTTP API server + chat agent loop + bot lifecycle
│   ├── llm/                OpenAI-compatible streaming client (9 providers)
│   ├── tools/              20+ tool implementations (cross-platform via build tags)
│   ├── approval/           103 dangerous command patterns
│   ├── storage/            SQLite + FTS5 (sessions, messages)
│   ├── config/             .env, YAML, profiles, doctor
│   ├── memory/             MEMORY.md + USER.md
│   ├── persona/            SOUL.md
│   ├── models/             Model library + remote sync
│   ├── skills/             Skill discovery + install
│   ├── cron/               Scheduled tasks
│   ├── paths/              Cross-platform path resolution
│   └── claw3d/             pan-office subprocess management
├── desktop/                Tauri v2 + React 19 frontend
│   ├── src-tauri/          Rust shell + plugins (shell, updater)
│   ├── src/                15 React screens
│   └── package.json
├── docs/                   Comprehensive manual (42 documents)
│   ├── README.md
│   └── manual/
└── .github/workflows/      CI matrix (Win/Mac/Linux) + release pipeline
```

---

## Development

### Prerequisites

- **Go 1.25+** — for the backend
- **Node.js 22+ with npm** — for the desktop frontend (`cd desktop && npm ci`)
- **Rust via rustup** — required for native Tauri work. After installing rustup, run `rustup default stable`, then verify with `cargo --version` and `rustc --version`.
- **Linux extras**: `libwebkit2gtk-4.1-dev libappindicator3-dev librsvg2-dev patchelf libgtk-3-dev libsoup-3.0-dev libjavascriptcoregtk-4.1-dev`
- **macOS extras**: Xcode Command Line Tools, `MACOSX_DEPLOYMENT_TARGET=14.0` env var

If `npm run check:tauri` prints `rustup could not choose a version of cargo to run`, rustup is installed but no default toolchain is configured yet. Fix it with `rustup default stable`.

### Build

```bash
git clone https://git.euraika.net/euraika/pan-agent.git
cd pan-agent

# Backend
go build -o pan-agent.exe ./cmd/pan-agent
go test ./... -count=1 -timeout 120s

# Desktop frontend dependencies + checks
cd desktop
npm ci
npm run lint
npm run typecheck
npm run build:vite

# Native Rust/Tauri prerequisites check
cargo --version
rustc --version
npm run check:tauri

# Full Tauri installer (needs Rust + Go sidecar)
cd ..
# Build the Go sidecar with the target-triple filename Tauri expects.
# Example for Linux x86_64; replace the filename on other platforms:
#   Windows: pan-agent-x86_64-pc-windows-msvc.exe
#   macOS ARM: pan-agent-aarch64-apple-darwin
#   Linux x86_64: pan-agent-x86_64-unknown-linux-gnu
go build -o desktop/src-tauri/binaries/pan-agent-x86_64-unknown-linux-gnu ./cmd/pan-agent
cd desktop
# For local unsigned testing builds, use --no-sign.
npx tauri build --no-sign
```

### Run in dev mode

```bash
# Terminal 1: Go backend
go run ./cmd/pan-agent serve --port 8642

# Terminal 2: Vite dev server (no Tauri shell, just the React UI in a browser)
cd desktop && npm run dev:vite
# → http://localhost:5173

# Or full Tauri dev mode (needs Rust)
cd desktop && npm run dev
```

### Adding a new tool

1. Create `internal/tools/mytool.go`.
2. Implement the four-method `Tool` interface (`Name`, `Description`, `Parameters`, `Execute`).
3. Add `func init() { Register(MyTool{}) }`.
4. Build. The tool is now visible to the LLM.

For platform-specific tools, follow the [Cross-Platform Tool Architecture](docs/manual/Part%20I%20-%20Foundations/02%20-%20Architecture/03%20-%20Cross-Platform%20Tool%20Architecture.md) pattern with `_common.go` + `_windows.go` / `_darwin.go` / `_linux.go` + `_stub.go`.

---

## Configuration

Pan-Agent stores configuration per platform:

| Platform | AgentHome path |
|---|---|
| Windows | `%LOCALAPPDATA%\pan-agent\` |
| macOS | `~/Library/Application Support/pan-agent/` |
| Linux | `~/.local/share/pan-agent/` |

Inside AgentHome:

| File | Purpose |
|---|---|
| `.env` | API keys (per-profile) |
| `config.yaml` | Provider, model, base URL, platform toggles (per-profile) |
| `state.db` | SQLite database (sessions, messages) |
| `MEMORY.md` | Persistent agent memory (per-profile) |
| `SOUL.md` | Agent persona (per-profile) |
| `models.json` | Cached model library |
| `cron/jobs.json` | Scheduled tasks |
| `skills/` | Installed skills |
| `profiles/<name>/` | Named profile directories |

For the full reference, see [Configuration Reference](docs/manual/Part%20III%20-%20Operations/04%20-%20Configuration%20Reference.md).

---

## Notes

### Windows SmartScreen & Defender

Pan-Agent ships **unsigned** on Windows. You will see one or both of these on first launch:

**SmartScreen** (`Windows protected your PC`) — click **More info** → **Run anyway**. One-time per binary.

**Windows Defender may flag the binary as a false positive**, typically with a generic ML-heuristic name like `Trojan:Win32/Wacatac.B!ml`. This is a known pattern with Go-built executables across the ecosystem (rclone, syncthing, lazygit, Hugo, and most Go projects hit this at some point). Pan-Agent's actual feature set — keyboard/mouse injection, screen capture, localhost HTTP server, browser automation — overlaps the capabilities Defender's heuristic engine looks for in remote-access tools. The engine is correct that the binary *can* do those things; it can't tell that it's doing them under your authorisation.

**How to verify the binary is the one CI built:**

Every release page lists SHA256 hashes for every artefact. After downloading, in PowerShell:

```powershell
Get-FileHash .\pan-agent-x86_64-pc-windows-msvc.exe -Algorithm SHA256
```

The hash must match the value on the [release page](https://github.com/Euraika-Labs/pan-agent/releases). If it doesn't, don't run it.

**If Defender quarantines a release binary**, you can submit it for review at https://www.microsoft.com/wdsi/filesubmission — Microsoft typically whitelists confirmed-clean hashes within ~3 days. Pan-Agent maintainers do this on every release; if you hit the warning before the whitelist propagates, you'll need to either wait or restore the file from quarantine manually.

This warning will go away when Pan-Agent ships code-signed installers (planned around `v1.0.0`).

### macOS Gatekeeper

Pan-Agent is not notarized. First launch is blocked:

```sh
xattr -cr "/Applications/Pan Desktop.app"
# Or right-click the app → Open → Open
```

### Linux Wayland

PC control tools (keyboard, mouse, window_manager, screenshot) require X11. They work under XWayland (default on GNOME and KDE on Wayland). Pure Wayland without XWayland is not supported.

### macOS Accessibility

Window manipulation (move/resize/close) uses AppleScript and requires Accessibility permission:

> System Preferences → Privacy & Security → Accessibility → add Pan Desktop

---

## Security

Pan-Agent's HTTP API binds to `127.0.0.1` only and has no authentication. This is by design for a single-user desktop app.

- **In scope**: approval system, profile name validation, CSP, signed updates
- **Out of scope**: defending against arbitrary local code execution

For the full threat model, see [Security Model](docs/manual/Part%20III%20-%20Operations/05%20-%20Security%20Model.md).

To report a vulnerability: **bert@euraika.net** — see [SECURITY.md](SECURITY.md).

---

## Contributing

Contributions welcome — bug fixes, new features, doc improvements.

1. Fork or branch from `main`.
2. Make your changes.
3. Run `go test ./...`, `cd desktop && npm run lint`, and `cd desktop && npm run typecheck` — all must pass.
4. Commit using [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `refactor:`).
5. Open a merge request on GitLab (primary) or pull request on GitHub (mirror).

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow.

---

## Project Status

| Metric | Value |
|---|---|
| Latest release | [v0.6.5](https://github.com/Euraika-Labs/pan-agent/releases/latest) |
| Platforms | Windows, macOS, Linux |
| Go test functions | 242 (across 17 packages) |
| Vitest cases (desktop) | 73 |
| HTTP endpoints | 64 (across 15 `/v1/*` resource groups) |
| React screens | 18 |
| Approval patterns | 103 (Dangerous + Catastrophic) |
| Supported LLM providers | 9 |
| Built-in tools | 22+ (includes `interact`, `skill_manage`, `skill_view`, `skills_list`, `skill_review`, `skill_curator`) |
| Self-healing skill system | Phase 11 — proposal queue, reviewer + curator agents, history+rollback |
| Trust-First substrate | Phase 12 — `internal/secret/` (HMAC redaction + keyring), `internal/recovery/` (action journal, snapshots, reversers, two-lane History UI), `internal/taskrunner/` (durable task runner with CAS + heartbeat reaper), per-session cost budgets |
| macOS permission wizard | Phase 12 WS#5 — Tauri Setup step probes Accessibility / Screen Recording / Automation / Full Disk Access via public TCC APIs only, with MDM-managed banner |
| SaaS deep-link library | Phase 12 + Phase 13 WS#13.F — Gmail / Stripe / Google Calendar / Slack / Notion / Jira URL builders in `internal/saaslinks/` (sealed contract; tools that consume these arrive in Phase 13) |
| Documentation | 3 manual documents (Table of Contents · HTTP API Reference · Changelog) + 2 design docs (`docs/design/phase12.md`, `docs/design/phase13.md`) |
| License | MIT |

Pan-Agent shipped Phase 12 ("Trust-First Desktop Automation") in v0.6.0 on 2026-04-26 — every backend workstream (browser persistence + cost budgets, action journal + recovery, vision + interact tool, durable task runner, macOS permission wizard) now has its desktop counterpart. v0.6.5 adds the standalone `pan` terminal CLI and desktop packaging fixes. Phase 13 is in progress: see [`docs/design/phase13.md`](docs/design/phase13.md) for the workstream sequencing and [CHANGELOG.md](CHANGELOG.md) for the version history.

---

## Repositories

| Role | URL |
|---|---|
| Source of truth | [git.euraika.net/euraika/pan-agent](https://git.euraika.net/euraika/pan-agent) (GitLab) |
| CI + binary distribution | [github.com/Euraika-Labs/pan-agent](https://github.com/Euraika-Labs/pan-agent) (GitHub mirror) |
| Releases | [github.com/Euraika-Labs/pan-agent/releases](https://github.com/Euraika-Labs/pan-agent/releases) |

GitLab is the primary repo. GitHub is the mirror used for CI runners and binary releases. All contributions should target GitLab.

---

## License

[MIT](LICENSE) — Copyright (c) 2026 Euraika Labs.

Pan-Agent is the spiritual successor to [Pan Desktop](https://git.euraika.net/euraika/pan-desktop), itself a hard fork of [fathah/hermes-desktop](https://github.com/fathah/hermes-desktop). See the [Changelog](CHANGELOG.md) for the lineage.

---

<div align="center">

**Built by [Euraika Labs](https://euraika.net)** · Tienen, Belgium 🇧🇪 / 🇪🇺

[Website](https://euraika.net) · [Issues](https://github.com/Euraika-Labs/pan-agent/issues) · [Releases](https://github.com/Euraika-Labs/pan-agent/releases)

</div>
