# Pan-Agent Manual

**Version 0.2.0** | **April 2026** | **Euraika Labs**

Pan-Agent is an AI desktop agent with full PC control. It runs as a single Go binary that serves an HTTP API, with a Tauri/React desktop app as the graphical interface. It supports Windows, macOS, and Linux.

---

## Table of Contents

1. [Installation](#1-installation)
2. [First-Run Setup](#2-first-run-setup)
3. [Chat](#3-chat)
4. [Tools](#4-tools)
5. [Profiles](#5-profiles)
6. [Models & Providers](#6-models--providers)
7. [Memory & Persona](#7-memory--persona)
8. [Skills](#8-skills)
9. [Messaging Gateway](#9-messaging-gateway)
10. [Schedules](#10-schedules)
11. [Office (Claw3D)](#11-office-claw3d)
12. [CLI Usage](#12-cli-usage)
13. [HTTP API Reference](#13-http-api-reference)
14. [Configuration](#14-configuration)
15. [Architecture](#15-architecture)
16. [Security](#16-security)
17. [Troubleshooting](#17-troubleshooting)

---

## 1. Installation

### Desktop App (Recommended)

Download the installer for your platform from [GitHub Releases](https://github.com/Euraika-Labs/pan-agent/releases):

| Platform | File | Notes |
|---|---|---|
| Windows | `Pan.Desktop_x.x.x_x64-setup.exe` | NSIS installer (unsigned — click "More info" → "Run anyway" on SmartScreen) |
| macOS | `Pan.Desktop_x.x.x_aarch64.dmg` | ARM64 (Apple Silicon). Right-click → Open to bypass Gatekeeper. |
| Linux | `Pan.Desktop_x.x.x_amd64.AppImage` | Make executable: `chmod +x *.AppImage` then run. Also available as `.deb`. |

The installer bundles both the Go backend binary and the Tauri desktop app. The Go binary (`pan-agent`) runs automatically as a sidecar process.

### Standalone Binary (Headless)

For server or CLI usage without the desktop app:

```sh
# Download from GitHub Releases or build from source
go build -o pan-agent ./cmd/pan-agent

# Start the HTTP API server
./pan-agent serve --port 8642

# Or use the interactive CLI
./pan-agent chat --model kimi-k2-0905
```

### Auto-Update

The desktop app checks for updates automatically via the Tauri updater. When a new version is available, you'll be prompted to download and install it. Updates are signed with Ed25519 for integrity verification.

---

## 2. First-Run Setup

On first launch, Pan-Agent detects that no LLM provider is configured and shows the **Setup Wizard**.

### Step 1: Choose a Provider

Six provider options are available:

| Provider | Description | API Key Required |
|---|---|---|
| **OpenRouter** (Recommended) | 200+ models from multiple providers | Yes |
| **Anthropic** | Claude models | Yes |
| **OpenAI** | GPT models | Yes |
| **Regolo** (EU-hosted) | Open models hosted in the EU | Yes |
| **Local LLM** | LM Studio, Ollama, vLLM, llama.cpp | No |
| **Custom OpenAI-compatible** | Any OpenAI-compatible endpoint | Optional |

### Step 2: Enter Credentials

- For cloud providers: paste your API key. A link to the provider's key management page is provided.
- For local LLMs: select a preset (LM Studio on port 1234, Ollama on 11434, etc.) or enter a custom URL.
- For custom endpoints: enter the base URL and optionally an API key.

### Step 3: Model Sync

Pan-Agent automatically fetches available models from your provider in the background. You can start chatting immediately — model sync happens asynchronously.

### Returning Users

If you already have an API key configured (e.g., from a previous session or manual `.env` edit), the setup wizard is skipped and you go directly to the Chat screen.

---

## 3. Chat

The Chat screen is the primary interface. Type a message and press Enter to send it to the AI.

### Features

- **Streaming responses**: Text appears word-by-word via Server-Sent Events (SSE).
- **Tool use**: The AI can call tools (terminal commands, file operations, web search, etc.) during the conversation. Tool calls are shown in real-time.
- **Approval system**: Dangerous commands (file deletion, system commands, database operations) trigger an approval prompt. You can approve or deny each tool call.
- **Session continuity**: Conversations are persisted in SQLite. Resume any previous session from the Sessions screen.
- **Keyboard shortcuts**: `Ctrl+N` for new chat, `Ctrl+K` for search.

### Approval Levels

| Level | Examples | Behavior |
|---|---|---|
| Safe | `echo hello`, `git status`, `ls` | Auto-approved |
| Dangerous | `rm -rf`, `DROP TABLE`, `git reset --hard` | Requires approval |
| Catastrophic | `format C:`, `vssadmin delete shadows`, `mimikatz` | Blocked by default |

The approval system uses 103 regex patterns to classify commands.

---

## 4. Tools

Pan-Agent includes 20+ built-in tools that the AI can use during conversations:

### Core Tools

| Tool | Description |
|---|---|
| `terminal` | Execute shell commands |
| `filesystem` | Read, write, list, and search files |
| `code_execution` | Run code snippets in sandboxed environments |
| `browser` | Automate Chromium via DevTools Protocol (go-rod) |
| `web_search` | Search the web via configured search APIs |

### PC Control Tools (Cross-Platform)

| Tool | Windows | macOS | Linux |
|---|---|---|---|
| `screenshot` | GDI | CoreGraphics | X11 (xgb) |
| `keyboard` | SendInput | CGEventPost | XTest |
| `mouse` | SendInput | CGEventCreateMouseEvent | XTest + WarpPointer |
| `window_manager` | EnumWindows | CGWindowListCopyWindowInfo + AppleScript | EWMH (_NET_CLIENT_LIST) |
| `ocr` | Vision LLM | Vision LLM | Vision LLM |

**Note**: On Linux, PC control tools require an X11 display (XWayland works). Pure Wayland without XWayland is not supported. On macOS, window management (move/resize/close) requires Accessibility permission in System Preferences > Privacy & Security > Accessibility.

### AI Tools

| Tool | Description |
|---|---|
| `vision` | Analyze images using multimodal LLMs |
| `image_gen` | Generate images via configured image APIs |
| `tts` | Text-to-speech synthesis |
| `ocr` | Extract text from screenshots using vision LLMs |
| `clarify` | Ask the user for clarification |
| `delegation` | Delegate subtasks to specialized agents |
| `moa` | Mixture of Agents — query multiple models |

### Utility Tools

| Tool | Description |
|---|---|
| `memory_tool` | Read/write the agent's persistent memory |
| `session_search` | Search past conversation sessions |
| `todo` | Manage task lists |
| `cron_tool` | Schedule recurring tasks |

Tools register themselves on supported platforms only. If a tool isn't available on your OS, it won't appear in the tool list and the AI won't try to use it.

---

## 5. Profiles

Profiles provide isolated agent environments. Each profile has its own:

- `.env` file (API keys)
- `config.yaml` (model, provider, platform settings)
- `MEMORY.md` (persistent memory)
- `SOUL.md` (persona/system prompt)
- Installed skills

### Managing Profiles

- **Default profile**: Always exists. Located at `<AgentHome>/`.
- **Named profiles**: Stored in `<AgentHome>/profiles/<name>/`.
- **Create**: From the Profiles screen, click "New Profile". Optionally clone configuration from the current profile.
- **Switch**: Click a profile card to switch. The active profile determines which configuration and memory the agent uses.
- **Delete**: Click the delete button on a profile card. The default profile cannot be deleted.

### API

```
GET    /v1/config/profiles           → list all profiles
POST   /v1/config/profiles           → create (body: {name, cloneConfig})
DELETE /v1/config/profiles/{name}    → delete
```

---

## 6. Models & Providers

### Supported Providers

| Provider | Base URL | Key Env Var |
|---|---|---|
| OpenAI | `https://api.openai.com/v1` | `OPENAI_API_KEY` |
| Anthropic | `https://api.anthropic.com/v1` | `ANTHROPIC_API_KEY` |
| Regolo | `https://api.regolo.ai/v1` | `REGOLO_API_KEY` |
| OpenRouter | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| Groq | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` |
| Ollama | `http://localhost:11434/v1` | (none) |
| LM Studio | `http://localhost:1234/v1` | (none) |
| vLLM | `http://localhost:8000/v1` | (none) |
| llama.cpp | `http://localhost:8080/v1` | (none) |

All providers use the OpenAI-compatible chat completions API format.

### Model Sync

Click "Sync Models" in the Models screen (or `POST /v1/models/sync`) to fetch available models from your configured provider. Models are cached locally in `models.json`.

### Switching Models

From the Settings screen, change the provider, model, and base URL. Changes take effect immediately — the LLM client is refreshed in-process.

---

## 7. Memory & Persona

### Memory (MEMORY.md)

The agent has persistent memory stored in `MEMORY.md`. Each entry is a fact, preference, or piece of context the agent should remember across conversations.

- **View/Edit**: Memory screen in the desktop app.
- **Add**: The agent can add memories during conversation via the `memory_tool`.
- **Format**: Entries separated by `§` delimiter with character limits.

### Persona (SOUL.md)

The persona defines the agent's identity and behavior via a system prompt stored in `SOUL.md`.

- **View/Edit**: Persona screen (labeled "Persona" in the sidebar).
- **Reset**: Restore the default persona.
- **Per-profile**: Each profile can have a different persona.

---

## 8. Skills

Skills are installable capabilities defined in `SKILL.md` files. They extend the agent's system prompt with specialized instructions.

- **Bundled skills**: Ship with the binary in a `skills/` directory next to the executable.
- **Installed skills**: User-installed skills in `<AgentHome>/skills/` (or per-profile `<ProfileHome>/skills/`).
- **Install**: From the Skills screen or via `POST /v1/skills/install`.
- **Uninstall**: From the Skills screen or via `POST /v1/skills/uninstall`.

---

## 9. Messaging Gateway

Pan-Agent can connect to messaging platforms, allowing you to chat with your AI agent from Telegram, Discord, or Slack.

### Setup

1. Go to the **Gateway** screen.
2. Enable the platform you want (toggle the switch).
3. Enter the required tokens:

| Platform | Required | How to Get |
|---|---|---|
| Telegram | `TELEGRAM_BOT_TOKEN` | Talk to [@BotFather](https://t.me/BotFather) on Telegram |
| Telegram | `TELEGRAM_ALLOWED_USERS` (optional) | Comma-separated Telegram user IDs for access control |
| Discord | `DISCORD_BOT_TOKEN` | [Discord Developer Portal](https://discord.com/developers/applications) → Bot → Token |
| Slack | `SLACK_BOT_TOKEN` | Slack App settings → OAuth & Permissions → Bot Token (xoxb-...) |
| Slack | `SLACK_APP_TOKEN` | Slack App settings → Basic Information → App-Level Token (xapp-...) |

4. Click **Start Gateway**.

### How It Works

- **Telegram**: Long polling (no public URL needed). The bot receives messages and routes them through the same LLM pipeline as the Chat screen.
- **Discord**: WebSocket connection via the Discord gateway. Responds to messages in any channel the bot is added to.
- **Slack**: Socket Mode (no public URL needed). Listens for message events.

### Session Continuity

Each platform conversation maps to a persistent session:
- Telegram: `tg-{chat_id}`
- Discord: `dc-{channel_id}`
- Slack: `sl-{channel_id}`

The agent remembers context within each conversation.

### Security

- Bot messages auto-approve tool calls (no interactive approval UI).
- Use `TELEGRAM_ALLOWED_USERS` to restrict Telegram bot access to specific user IDs.
- Discord and Slack access is controlled by which channels the bot is added to.

---

## 10. Schedules

Create recurring tasks that run on a cron schedule.

- **Create**: From the Schedules screen, set a name, cron expression, and prompt.
- **List**: View all scheduled tasks.
- **Delete**: Remove a scheduled task.

Cron jobs are stored in `<AgentHome>/cron/jobs.json`.

---

## 11. Office (Claw3D)

The Office screen integrates with [Claw3D / pan-office](https://github.com/Euraika-Labs/pan-office), providing an Office-like document editing experience within Pan-Agent.

- **Setup**: The first time you visit Office, it clones the pan-office repository and runs `npm install`.
- **Start/Stop**: Control the dev server and adapter from the Office screen or API.

---

## 12. CLI Usage

The Go binary supports four subcommands:

```sh
# Start the HTTP API server (default)
pan-agent serve [--port 8642] [--host 127.0.0.1] [--profile default]

# Interactive chat in the terminal
pan-agent chat [--model gpt-4o-mini] [--profile default]

# Print version
pan-agent version

# Run health checks
pan-agent doctor
```

### Doctor Output

```
pan-agent doctor
----------------
  [OK] AgentHome exists — C:\Users\user\AppData\Local\pan-agent
  [OK] Profile .env readable — C:\Users\user\AppData\Local\pan-agent\.env
  [OK] API key present — REGOLO_API_KEY or OPENAI_API_KEY
  [OK] SQLite DB opens — C:\Users\user\AppData\Local\pan-agent\state.db
  [OK] Config file present — C:\Users\user\AppData\Local\pan-agent\config.yaml

All checks passed.
```

---

## 13. HTTP API Reference

The API server listens on `http://127.0.0.1:8642` by default. All endpoints accept and return JSON.

### Chat

| Method | Path | Description |
|---|---|---|
| POST | `/v1/chat/completions` | SSE streaming chat with agent loop (tool calling, approval) |
| POST | `/v1/chat/abort` | Cancel an in-flight generation. Body: `{session_id}` |

### Approvals

| Method | Path | Description |
|---|---|---|
| GET | `/v1/approvals` | List pending approval requests |
| GET | `/v1/approvals/{id}` | Get a specific approval |
| POST | `/v1/approvals/{id}` | Resolve an approval. Body: `{approved: true/false}` |

### Sessions

| Method | Path | Description |
|---|---|---|
| GET | `/v1/sessions` | List sessions. Query: `?limit=50&offset=0&q=search` |
| GET | `/v1/sessions/{id}` | Get all messages for a session |

### Models

| Method | Path | Description |
|---|---|---|
| GET | `/v1/models` | List the local model library |
| POST | `/v1/models` | Set the active model. Body: `{provider, model, base_url}` |
| DELETE | `/v1/models/{id}` | Remove a saved model |
| POST | `/v1/models/sync` | Sync models from a remote provider. Body: `{provider, base_url, api_key}` |

### Configuration

| Method | Path | Description |
|---|---|---|
| GET | `/v1/config` | Full config: `{env, agentHome, model, credentialPool, appVersion, agentVersion}` |
| PUT | `/v1/config` | Update config. Union body: `{profile?, env?, model?, credentialPool?, platformEnabled?}` |

### Profiles

| Method | Path | Description |
|---|---|---|
| GET | `/v1/config/profiles` | List all profiles with metadata |
| POST | `/v1/config/profiles` | Create a profile. Body: `{name, cloneConfig: bool}` |
| DELETE | `/v1/config/profiles/{name}` | Delete a profile (cannot delete "default") |

### Diagnostics

| Method | Path | Description |
|---|---|---|
| POST | `/v1/config/doctor` | Run health checks. Returns: `{output: "..."}` |
| POST | `/v1/config/update` | Check for updates. Returns: `{available: bool, current: "0.2.0"}` |

### Memory

| Method | Path | Description |
|---|---|---|
| GET | `/v1/memory` | Read all memory entries |
| POST | `/v1/memory` | Add an entry. Body: `{content}` |
| PUT | `/v1/memory/{index}` | Update an entry at index. Body: `{content}` |
| DELETE | `/v1/memory/{index}` | Remove an entry at index |

### Persona

| Method | Path | Description |
|---|---|---|
| GET | `/v1/persona` | Read the persona (SOUL.md) |
| PUT | `/v1/persona` | Update the persona. Body: `{persona}` |
| POST | `/v1/persona/reset` | Reset to default persona |

### Tools

| Method | Path | Description |
|---|---|---|
| GET | `/v1/tools` | List all registered tools |
| PUT | `/v1/tools/{key}` | Toggle a tool on/off |

### Skills

| Method | Path | Description |
|---|---|---|
| GET | `/v1/skills` | List installed + bundled skills |
| POST | `/v1/skills/install` | Install a skill. Body: `{id, profile?}` |
| POST | `/v1/skills/uninstall` | Uninstall a skill. Body: `{id, profile?}` |

### Cron

| Method | Path | Description |
|---|---|---|
| GET | `/v1/cron` | List scheduled jobs |
| POST | `/v1/cron` | Create a job. Body: `{name, schedule, prompt}` |
| DELETE | `/v1/cron/{id}` | Delete a job |

### Health & Gateway

| Method | Path | Description |
|---|---|---|
| GET | `/v1/health` | Health status: `{gateway: bool, env, platformEnabled}` |
| POST | `/v1/health/gateway/start` | Start messaging bots for enabled platforms |
| POST | `/v1/health/gateway/stop` | Stop all messaging bots |

### Claw3D

| Method | Path | Description |
|---|---|---|
| GET | `/v1/claw3d/status` | Get Claw3D installation and process state |
| POST | `/v1/claw3d/setup` | Clone and install Claw3D (streams progress) |
| POST | `/v1/claw3d/start` | Start the dev server and adapter |
| POST | `/v1/claw3d/stop` | Stop the dev server and adapter |

---

## 14. Configuration

### Data Directory

| Platform | Path |
|---|---|
| Windows | `%LOCALAPPDATA%\pan-agent\` |
| macOS | `~/Library/Application Support/pan-agent/` |
| Linux | `~/.local/share/pan-agent/` |

### Files

| File | Purpose |
|---|---|
| `.env` | API keys and environment variables (per-profile) |
| `config.yaml` | Provider, model, base URL, platform toggles (per-profile) |
| `state.db` | SQLite database for sessions and messages |
| `MEMORY.md` | Persistent agent memory (per-profile) |
| `USER.md` | User profile information (per-profile) |
| `SOUL.md` | Agent persona / system prompt (per-profile) |
| `models.json` | Cached model library |
| `auth.json` | Credential pool (multiple keys per provider) |
| `cron/jobs.json` | Scheduled tasks |
| `skills/` | Installed skills directory |
| `profiles/` | Named profile directories |

### .env Format

```
REGOLO_API_KEY=sk-xxx
OPENAI_API_KEY=sk-xxx
TELEGRAM_BOT_TOKEN=123456:ABC-DEF
TELEGRAM_ALLOWED_USERS=12345,67890
DISCORD_BOT_TOKEN=xxx
SLACK_BOT_TOKEN=xoxb-xxx
SLACK_APP_TOKEN=xapp-xxx
```

### config.yaml Format

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
```

### API Key Resolution Order

When the backend needs an API key, it checks in this order:
1. `REGOLO_API_KEY` from profile `.env`
2. `OPENAI_API_KEY` from profile `.env`
3. `API_KEY` from profile `.env`
4. `OPENAI_API_KEY` from system environment

---

## 15. Architecture

```
┌─────────────────────────────────────────────────────┐
│  Desktop App (Tauri v2 + React 19)                  │
│  ┌───────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │  Chat UI  │  │ Settings │  │  15 more screens │ │
│  └─────┬─────┘  └────┬─────┘  └────────┬─────────┘ │
│        │              │                  │           │
│        └──────────────┼──────────────────┘           │
│                       │ fetch / EventSource          │
└───────────────────────┼─────────────────────────────┘
                        │ http://localhost:8642
┌───────────────────────┼─────────────────────────────┐
│  Go Backend           │                              │
│  ┌────────────────────▼───────────────────────────┐ │
│  │  HTTP Server (gateway/)                         │ │
│  │  43 REST + SSE endpoints                        │ │
│  │  CORS: localhost:5173 + tauri://localhost        │ │
│  └──┬──────────┬───────────┬──────────┬───────────┘ │
│     │          │           │          │              │
│  ┌──▼──┐  ┌───▼───┐  ┌───▼────┐  ┌──▼──────────┐  │
│  │ LLM │  │ Tools │  │Storage │  │  Gateway     │  │
│  │ SSE │  │ 20+   │  │SQLite  │  │  Bots        │  │
│  │     │  │       │  │ FTS5   │  │  TG/DC/Slack │  │
│  └─────┘  └───────┘  └────────┘  └─────────────┘  │
└─────────────────────────────────────────────────────┘
```

### Key Design Principles

- **HTTP API, not IPC**: The Go binary exposes REST+SSE on localhost. The Tauri frontend talks via fetch/EventSource. This means the agent also works headless (CLI, server mode, curl).
- **Pure Go SQLite**: Uses `modernc.org/sqlite` — no CGo needed for the database (CGo is only used for macOS PC control tools).
- **Build-tag platform architecture**: PC control tools use `_windows.go`, `_darwin.go`, `_linux.go` files. Tools only register on platforms where they work — on unsupported platforms, they're invisible to the AI.
- **Single binary**: The Go backend compiles to one executable. No Python, no Node.js, no runtime dependencies.

---

## 16. Security

### Threat Model

Pan-Agent is a **single-user desktop application**. The API server binds to `127.0.0.1` only and is not accessible from the network.

### Protections

- **CORS**: Only `http://localhost:5173` (dev) and `tauri://localhost` (production) are allowed origins.
- **CSP**: The Tauri webview enforces a Content Security Policy restricting connections to `self` and `localhost:8642`.
- **Approval system**: 103 regex patterns classify commands as Safe, Dangerous, or Catastrophic before execution.
- **Profile name validation**: Regex `^[a-zA-Z0-9][a-zA-Z0-9_-]*$` prevents path traversal in profile operations.
- **Update signing**: Desktop app updates are signed with Ed25519 and verified before installation.

### Limitations

- **No API authentication**: Any local process can call `localhost:8642`. This is by design for a single-user desktop app.
- **Plaintext API keys**: Keys are stored in `.env` files with `0600` permissions. No encryption at rest.
- **Bot auto-approval**: Messaging gateway bots auto-approve all tool calls — use `TELEGRAM_ALLOWED_USERS` to restrict access.

### Reporting Vulnerabilities

Email: bert@euraika.net. See [SECURITY.md](../SECURITY.md) for full details.

---

## 17. Troubleshooting

### "No LLM client configured"

Run `pan-agent doctor` to check your setup. Ensure you have an API key in `.env` and a model configured in `config.yaml`.

### Setup wizard keeps appearing

The setup wizard appears when no LLM provider API key is found AND no custom base URL is configured. If using a local LLM, make sure `config.yaml` has `provider: "custom"` and `base_url` set.

### macOS: "App is damaged" / "unidentified developer"

Pan-Agent is not notarized. Right-click the app → Open → click Open in the confirmation dialog. Or run:
```sh
xattr -cr "/Applications/Pan Desktop.app"
```

### Linux: Screenshot/keyboard/mouse not working

These tools require X11. If using Wayland, ensure XWayland is running (it is by default on GNOME and KDE). Check with:
```sh
echo $XDG_SESSION_TYPE   # should be "x11" or "wayland" (with XWayland)
xdpyinfo | head -5       # should show display info
```

### macOS: Window management not working

Window move/resize/close operations use AppleScript, which requires Accessibility permission. Go to System Preferences → Privacy & Security → Accessibility → add Pan Desktop.

### Gateway bot not responding

1. Check that the bot token is correct in the Gateway screen.
2. Ensure the platform toggle is enabled.
3. Click "Start Gateway" (the gateway must be explicitly started).
4. Check the terminal output for error messages.
5. For Telegram: ensure the bot is added to the chat and `TELEGRAM_ALLOWED_USERS` includes your user ID (if set).

### Build from source

```sh
git clone https://git.euraika.net/euraika/pan-agent.git
cd pan-agent

# Go backend
go build -o pan-agent ./cmd/pan-agent
go test ./... -count=1 -timeout 120s

# Desktop app
cd desktop && npm install
npx tsc --noEmit        # typecheck
npx vite build           # production build
npx tauri dev            # dev mode (needs Rust)
npx tauri build          # production installer (needs Rust)
```

### Environment variables for CI

| Variable | Purpose |
|---|---|
| `MACOSX_DEPLOYMENT_TARGET=14.0` | Required on macOS — `kbinani/screenshot` uses APIs obsoleted in macOS 15 |
| `TAURI_SIGNING_PRIVATE_KEY` | Ed25519 private key for update signing |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | Password for the signing key |

---

## License

MIT License. Copyright (c) 2026 Euraika Labs. See [LICENSE](../LICENSE).
