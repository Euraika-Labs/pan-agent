# Changelog

All notable changes to Pan-Agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-04-14

### Added
- Cross-platform PC control: keyboard, mouse, and window manager now work on Windows (SendInput), macOS (CoreGraphics CGo), and Linux (X11 XTest via jezek/xgb)
- Messaging gateway backends: Telegram (telego, long polling), Discord (discordgo, WebSocket), Slack (slack-go, Socket Mode)
- Auto-update via Tauri updater plugin with Ed25519 signing
- Release workflow producing signed installers for all 3 platforms (NSIS/MSI, DMG, DEB/AppImage)
- `runAgentLoop()` extracted from chat handler for reuse by bots
- Comprehensive manual at `docs/MANUAL.md`

### Changed
- Tools refactored to cross-platform architecture: shared `_common.go` + platform-specific `Execute()` + `_stub.go` for unsupported platforms
- Tools only register on platforms where they work (invisible to LLM on unsupported platforms)
- CI matrix expanded from Windows-only to Windows + macOS + Linux
- Tauri bundle targets changed from `["nsis", "msi"]` to `"all"`
- CSP tightened from `null` to restrictive policy in tauri.conf.json
- Gateway start/stop endpoints now actually start/stop bot goroutines (no longer stubs)

### Fixed
- macOS build: `MACOSX_DEPLOYMENT_TARGET=14.0` for kbinani/screenshot compatibility with macOS 15 SDK
- macOS CGo: `unsafe.Pointer` for `CFDictionaryGetValueIfPresent` in window_manager_darwin.go

## [0.1.1] - 2026-04-13

### Added
- First-run onboarding wizard (Setup screen) with 6 provider cards, API key entry, and local server presets
- Profile CRUD endpoints (`GET/POST/DELETE /v1/config/profiles`) with path traversal protection
- `POST /v1/config/doctor` — run diagnostics via HTTP API
- `POST /v1/config/update` — update check stub
- `POST /v1/health/gateway/start` and `/stop` — messaging gateway toggle stubs
- First-run detection in Layout.tsx with exponential backoff retry

### Changed
- `GET /v1/config` now returns structured `ConfigResponse` (env, agentHome, model, credentialPool, appVersion, agentVersion)
- `PUT /v1/config` now accepts union body with optional env, model, credentialPool, and platformEnabled fields
- `GET /v1/health` now returns `{gateway, env, platformEnabled}` instead of `{status: "ok"}`
- Extracted `refreshLLMClient` method to unify LLM client refresh across config and model handlers

### Fixed
- Session resume calling non-existent `/v1/sessions/{id}/messages` endpoint (now uses `/v1/sessions/{id}`)
- Persona PUT test sending `"content"` key instead of `"persona"`
- Added regex validation to `DeleteProfile` to prevent path traversal via `os.RemoveAll`

## [0.1.0] - 2026-04-12

### Added
- Go backend with 32 HTTP endpoints + SSE streaming chat
- OpenAI-compatible LLM client supporting 9 providers
- 20 tools: terminal, filesystem, browser, web search, code execution, screenshot, keyboard, mouse, OCR, window manager, and more
- 103 dangerous command approval patterns (Level 1 + Level 2)
- SQLite storage with FTS5 full-text search
- Profile-based configuration with `.env` and `config.yaml`
- MEMORY.md + USER.md persistent memory system
- SOUL.md persona system
- Model library with remote sync
- Skill discovery and install/uninstall
- Cron job scheduling
- Claw3D / pan-office integration
- Tauri v2 + React 19 desktop app with 14 screens
- GitLab CI + GitHub Actions build pipeline

[0.2.0]: https://github.com/Euraika-Labs/pan-agent/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/Euraika-Labs/pan-agent/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Euraika-Labs/pan-agent/releases/tag/v0.1.0
