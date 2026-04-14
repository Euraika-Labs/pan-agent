# Quick Start

Use this note when you want to get Pan-Agent running in under five minutes.

## First-time setup question
Ask this first:
- Do you want the desktop app (recommended) or just the headless CLI?
- Do you have an API key from a cloud provider, or are you using a local LLM (Ollama, LM Studio)?
- What platform are you on?

## Quick reference

| Item | Value |
|---|---|
| Latest release | v0.2.0 |
| GitHub Releases | `https://github.com/Euraika-Labs/pan-agent/releases` |
| API server port | `:8642` (localhost only) |
| Desktop dev port | `:5173` (Vite) |
| Repo (primary) | `git.euraika.net/euraika/pan-agent` |
| Repo (mirror) | `github.com/Euraika-Labs/pan-agent` |

## Installation

### Desktop app (recommended)

| Platform | File |
|---|---|
| Windows | `Pan.Desktop_0.2.0_x64-setup.exe` (NSIS, unsigned — click "More info" → "Run anyway") |
| macOS ARM | `Pan.Desktop_0.2.0_aarch64.dmg` (right-click → Open to bypass Gatekeeper) |
| Linux | `Pan.Desktop_0.2.0_amd64.AppImage` (chmod +x then run) or `.deb` |

### Headless CLI

```bash
# Download the platform binary from GitHub Releases, or build from source
go build -o pan-agent ./cmd/pan-agent

./pan-agent serve --port 8642        # HTTP API server
./pan-agent chat --model gpt-4o-mini # Interactive CLI chat
./pan-agent doctor                    # Health check
./pan-agent version                   # Print version
```

## First run

The first time you launch the desktop app, it shows the Setup Wizard. Pick a provider, paste your API key, click Continue. The wizard saves credentials to the profile `.env` and triggers model sync in the background. You go straight to Chat.

If you already have an API key in `.env` (e.g., from a previous install), the wizard is skipped.

## Fast health checks

```bash
# API health (returns gateway status, env, platform toggles)
curl -sf http://localhost:8642/v1/health | jq

# Run diagnostics via API
curl -sf -X POST http://localhost:8642/v1/config/doctor | jq -r .output

# List configured models
curl -sf http://localhost:8642/v1/models | jq
```

## Fast route to the right document

### "I want to use a feature"
- Chat with the agent → [[01 - Chat]]
- Set up a profile → [[03 - Profiles]]
- Connect Telegram/Discord/Slack → [[08 - Messaging Gateway Bots]]
- Schedule a recurring task → [[07 - Schedules]]

### "Something is broken"
- Setup wizard keeps appearing → [[01 - Setup Wizard Issues]]
- Bot not responding → [[02 - Gateway Bot Issues]]
- Screenshot/keyboard/mouse not working → [[03 - PC Control Tool Issues]]
- CI build failing → [[04 - Build and CI Issues]]

### "I want to understand how it works"
- High-level architecture → [[01 - System Overview]]
- HTTP API → [[02 - HTTP API Surface]] or [[00 - HTTP API Reference]]
- Cross-platform tools → [[03 - Cross-Platform Tool Architecture]]

## User rule
Pan-Agent is single-user, localhost-only, and unauthenticated by design. Do not expose port 8642 to the network. The Tauri desktop app and any local script can call it freely — that is intentional.

## Read next
- [[01 - System Overview]]
- [[03 - Top 10 Things Every User Should Know]]
- [[02 - Reading Guide]]
