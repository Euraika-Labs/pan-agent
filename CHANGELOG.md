# Changelog

All notable changes to Pan-Agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.1] - 2026-04-14

### Fixed
- `walkSkillsDir` no longer enumerates Phase 11's reserved subdirs (`_proposed/`, `_archived/`, `_history/`, `_merged/`, `_rejected/`) as skill categories. Previously leaked UUID-named "skills" into the active-skills API and the LLM-facing skills-inventory injection in `chat.go`.
- Filesystem tool: tightened created-file perms from `0o644` → `0o600` and directory perms from `0o755` → `0o750` (closes gosec G301/G302).
- `// #nosec G117` on `tavilySearch` request marshal with rationale — `APIKey` IS the Tavily request body field.
- `// #nosec G122` on Walk-time `os.ReadFile` in `opGrep` with threat-model rationale.

### Changed
- `.golangci.yml` migrated to v2 schema with a lean staticcheck-equivalent linter set (`govet`, `staticcheck`, `ineffassign`, `unused`) + `gofmt`/`goimports` formatters.
- `lint.yml` gosec job now explicitly excludes `G104,G115,G204,G304,G703,G704` with per-rule rationale comments; stops chronic false-positive noise in GitHub Code Scanning.
- `lint.yml` clippy job builds the Go sidecar before linting (matches `ci.yml` pattern).
- `lint.yml` gosec job got `security-events: write` permission so SARIF upload works.
- Removed the workflow-driven CodeQL config that conflicted with the repo's default-setup CodeQL.
- Release workflow auto-publishes SHA256 hashes + a Windows Defender FP note + per-artefact VirusTotal analysis links in the release notes, and opens a tracking issue with the WDSI submission checklist.
- README: expanded "Windows SmartScreen" into "Windows SmartScreen & Defender" with the hash verification recipe and the maintainer WDSI submission practice.

### Security
- Closed 43 gosec false-positive alerts via API dismissal with appropriate reason + rationale. Remaining enabled rules still fail CI loudly.

## [0.3.0] - 2026-04-14

Phase 11 — self-healing skill system, full feature-parity with hermes-agent's skill autonomy plus the additions hermes lacks.

### Added
- Proposal queue at `<ProfileSkillsDir>/_proposed/<uuid>/`. The main agent's `skill_manage(action="create"|"edit"|...)` writes proposals instead of mutating active skills directly. Every proposal carries `ProposalMetadata` (UUID, trust tier, source, status, intent) plus the SKILL.md body, plus a guard-scan result.
- Guard scanner with 30+ regex patterns across 6 categories (exec, fs, net, creds, obfuscation, prompt_injection). Blocks proposals with `severity=block` findings before they reach disk.
- History snapshots at `_history/<category>/<name>/SKILL.<timestamp>.md`, `ListHistory` + `Rollback` manager methods, HTTP `GET /v1/skills/history/{category}/{name}` + `POST .../rollback`.
- Atomic writes (temp file + rename) throughout the skill manager, with rollback snapshots on guard-blocked edits.
- **Reviewer agent** — bundled `reviewer.md` persona, `skill_review` tool (list/get/approve/reject/merge), `runReviewerAgent` bounded 10-turn LLM loop, HTTP `POST /v1/skills/reviewer/run`. Approve is intent-aware so curator-originated proposals trigger the right `ApplyCuratorIntent` side-effect (archive losers, materialise split children, move recategorised dir).
- **Curator agent** — bundled `curator.md` persona, `skill_curator` tool (`list_active_with_usage` + 5 propose actions), `runCuratorAgent` loop, HTTP `POST /v1/skills/curator/run`. Curator proposals land in the same queue the reviewer consumes, with intent metadata (`refine` / `merge` / `split` / `archive` / `recategorize`).
- Per-skill usage tracking in a new SQLite table `skill_usage` with indexes by skill, session, and time. `storage.DB.LogSkillUsage / ListSkillUsage / GetSkillUsageStats`. HTTP `GET /v1/skills/usage/{category}/{name}` + `.../stats`.
- Chat integration: skills inventory injected as a stable user message at a cache-friendly boundary (not in the system prompt — preserves the Anthropic prompt cache across turns). Tool calls on `skill_view` / `skill_manage` are logged to `SkillUsage` for curator input.
- 10 new HTTP endpoints: `/v1/skills/proposals` (list, get, approve, reject), `/v1/skills/history/{category}/{name}` (list + rollback), `/v1/skills/usage/{category}/{name}` (list + stats), `/v1/skills/reviewer/run`, `/v1/skills/curator/run`.
- 22 new unit tests across `internal/skills/` covering the proposal lifecycle, history+rollback, guard blocking, curator intents + apply, path-traversal rejection at every helper boundary, and the `walkSkillsDir` underscore-dir exclusion regression test.

### Security
- All skill-path construction funnels through `resolveActiveDir` / `resolveProposalDir` / `resolveHistoryDir` / `splitAndResolveActiveID` helpers that perform `filepath.Rel` containment checks. Same sanitiser pattern as `9618500` + `56cad02`. CodeQL recognises these as clean.
- Profile-name validation (`ValidateProfile`, regex `^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`) with belt-and-braces `filepath.Rel` containment in `paths.ProfileHome` — closes the 14 "Uncontrolled data used in path expression" CodeQL alerts.
- `models.SyncRemote` base URL sanitiser rejects non-http(s) schemes, missing host, and credential-laden URLs — closes the critical `go/request-forgery` CodeQL alert.

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

[0.3.1]: https://github.com/Euraika-Labs/pan-agent/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/Euraika-Labs/pan-agent/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/Euraika-Labs/pan-agent/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/Euraika-Labs/pan-agent/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Euraika-Labs/pan-agent/releases/tag/v0.1.0
