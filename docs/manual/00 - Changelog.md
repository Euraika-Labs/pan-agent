# Changelog

## 2026-04-14 — v0.2.0 cross-platform + gateway bots + auto-update

Major release closing the last feature gaps with hermes-desktop. Pan-Agent is now at full feature parity plus cross-platform support.

**Phases shipped:**
- Phase 7 — Onboarding wizard, profile CRUD, config/health API alignment with frontend
- Phase 8 — Cross-platform tool architecture (Windows + macOS + Linux)
- Phase 9 — Messaging gateway backends (Telegram, Discord, Slack)
- Phase 10 — Auto-update via Tauri updater + signed release workflow

**Cross-platform PC control tools:**
- Refactored 3 monolithic Windows tool files (keyboard, mouse, window_manager) into a clean platform-split architecture: `_common.go` (struct + Execute dispatch), `_windows.go`, `_darwin.go`, `_linux.go`, `_stub.go`.
- macOS implementations use CGo with CoreGraphics (CGEventPost for input) and AppleScript via osascript for window manipulation. Requires Accessibility permission for window operations.
- Linux implementations use `jezek/xgb` (already an indirect dependency — zero new downloads) for X11 XTest input injection and EWMH for window management. X11/XWayland required.
- Tools only register on platforms where they work — invisible to the LLM on unsupported platforms (no wasted tokens trying to use unavailable tools).

**Messaging gateway backends:**
- Telegram via `mymmrac/telego` (long polling, no public URL needed). User filtering via `TELEGRAM_ALLOWED_USERS`.
- Discord via `bwmarrin/discordgo` (WebSocket gateway with Message Content intent).
- Slack via `slack-go/slack` (Socket Mode, requires `SLACK_APP_TOKEN` xapp- in addition to bot token xoxb-).
- Extracted `runAgentLoop()` from `handleChatCompletions` so bots reuse the same LLM pipeline, tool execution, persona, and session persistence as the HTTP chat handler.
- Bots auto-approve tool calls (no interactive approval UI on a chat platform).
- Each bot maps platform chat ID → SQLite session ID for conversation continuity (`tg-<chat_id>`, `dc-<channel_id>`, `sl-<channel_id>`).

**Auto-update:**
- Tauri v2 updater plugin wired in `Cargo.toml` + `main.rs`.
- Ed25519 signing key generated locally; public key embedded in `tauri.conf.json`; private key + password stored as GitHub Actions secrets.
- `capabilities/default.json` grants the updater + shell + process permissions to the frontend.
- CSP tightened from `null` to a restrictive policy.

**Release pipeline:**
- New `.github/workflows/release.yml` triggered on `v*` tags. Three-platform matrix (Windows + macOS-arm + Linux ubuntu-22.04) building NSIS/MSI, DMG, and DEB/AppImage installers respectively.
- `tauri-apps/tauri-action@v0` handles the build + signing + `latest.json` generation + GitHub Release upload.
- Tauri bundle targets changed from `["nsis", "msi"]` to `"all"`.
- Required CI fixes during shake-out: `MACOSX_DEPLOYMENT_TARGET=14.0` (kbinani/screenshot uses CGDisplayCreateImageForRect, obsoleted in macOS 15 SDK), `tauri` npm script added (tauri-action runs `npm run tauri`), `TAURI_SIGNING_PRIVATE_KEY` secret added to CI Tauri job (createUpdaterArtifacts requires signing).

**New operational invariants worth remembering:**
- The HTTP API is localhost-only by design. There is no API authentication. Any local process can call `localhost:8642`. This is acceptable for a single-user desktop app — if an attacker has local code execution, they can read `.env` directly anyway.
- Bot conversations bypass the approval system. Use `TELEGRAM_ALLOWED_USERS` to restrict Telegram access. Discord and Slack rely on the bot's channel access permissions.
- macOS Accessibility permission is needed for window manipulation (move/resize/close via AppleScript). Listing windows works without permission.
- Linux PC control needs X11. Pure Wayland without XWayland is not supported. GNOME and KDE on Wayland default to running XWayland.
- The macOS build requires `MACOSX_DEPLOYMENT_TARGET=14.0` until `kbinani/screenshot` adopts ScreenCaptureKit.

**Notes added:**
- Full Pan-Agent manual under [[00 - Pan-Agent Home]]

## 2026-04-13 — v0.1.1 onboarding + profile CRUD + API alignment

Small but high-impact release fixing API mismatches that broke parts of the Settings and Gateway screens.

**Backend additions:**
- Setup wizard: 6 provider cards (OpenRouter, Anthropic, OpenAI, Regolo, Local LLM, Custom). API key entry, local server presets (LM Studio, Ollama, vLLM, llama.cpp), non-blocking model sync.
- Profile CRUD: `GET/POST/DELETE /v1/config/profiles` with regex name validation preventing path traversal in `os.RemoveAll`.
- `POST /v1/config/doctor` runs the same checks as the CLI `doctor` command and returns the output as JSON.
- `POST /v1/config/update` stub returning current version.
- `POST /v1/health/gateway/start` and `/stop` stubs (replaced with real implementations in v0.2.0).

**Backend changes:**
- `GET /v1/config` rewritten to return structured `ConfigResponse` (env, agentHome, model, credentialPool, appVersion, agentVersion) instead of a flat `map[string]string`. Settings.tsx had been silently broken.
- `PUT /v1/config` rewritten to accept a union body with optional `env`, `model`, `credentialPool`, and `platformEnabled` fields.
- `GET /v1/health` rewritten to return `{gateway, env, platformEnabled}` for Gateway.tsx.
- Extracted `refreshLLMClient` method on Server struct so config and model handlers share one code path for swapping the in-process LLM client.

**Frontend additions:**
- New Setup screen at `desktop/src/screens/Setup/Setup.tsx`.
- First-run detection in `Layout.tsx` with exponential backoff retry for the backend startup race window.

**Bug fixes:**
- Session resume was hitting a non-existent `/v1/sessions/{id}/messages` endpoint — fixed to use `/v1/sessions/{id}`.
- Persona PUT test was sending `"content"` JSON key but handler expected `"persona"` — silent test pass for months.

## 2026-04-12 — v0.1.0 initial release

Initial public release of Pan-Agent.

- Go backend with 32 HTTP endpoints + SSE streaming chat.
- OpenAI-compatible LLM client supporting 9 providers (OpenAI, Anthropic, Regolo, OpenRouter, Groq, Ollama, LM Studio, vLLM, llama.cpp).
- 20 tools: terminal, filesystem, browser (go-rod), web search, code execution, screenshot, keyboard, mouse, OCR, window manager, vision, image gen, TTS, and more.
- 103 dangerous command approval patterns (Level 1 Dangerous + Level 2 Catastrophic) ported from the predecessor Python implementation.
- Pure Go SQLite (`modernc.org/sqlite`) with FTS5 full-text search for sessions and messages.
- Profile-based configuration with `.env` and `config.yaml` per profile.
- MEMORY.md + USER.md persistent memory system.
- SOUL.md persona system.
- Model library with remote sync.
- Skill discovery and install/uninstall.
- Cron job scheduling.
- Claw3D / pan-office integration for document editing.
- Tauri v2 + React 19 desktop app with 14 screens.
- GitLab CI + GitHub Actions build pipeline.

## Scope

This manual documents `pan-agent` — the production replacement for `pan-desktop` (the predecessor Electron + Python stack that itself was forked from `fathah/hermes-desktop`).
Pan-Agent is a single Go binary plus a Tauri desktop app. It runs on Windows, macOS, and Linux.

For the predecessor documentation, see [[Euraika/Hermes V3/README]].
