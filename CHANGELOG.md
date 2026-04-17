# Changelog

All notable changes to Pan-Agent will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.4] - 2026-04-17

Completes the 0.4.3 security-scanner pass. No runtime behaviour change —
ships only the two workflow-permission blocks that were deferred from 0.4.3
because the release bot token lacked the `workflow` OAuth scope.

### Security
- **Workflow least-privilege `permissions:` blocks** added to
  `.github/workflows/chaos.yml` and `.github/workflows/e2e-real-webview.yml`:
  ```yaml
  permissions:
    contents: read
  ```
  `contents: read` is the minimum needed for `actions/checkout@v4`; artifact
  upload uses the independent `ACTIONS_RUNTIME_TOKEN` so no write scopes are
  required. Closes CodeQL `actions/missing-workflow-permissions` #78 and
  #79 (MEDIUM).

## [0.4.3] - 2026-04-17

Security scanner follow-up on 0.4.2. Clears 11 outstanding alerts on `main`
from CodeQL (5 high + 2 medium) and gosec (4 errors). No behaviour changes,
no new features.

### Security
- **Path sanitisation in claw3d migrate-office.** `internal/claw3d/migrate.go`
  adds `sanitizeMigrationPath` (`filepath.Clean` → `filepath.Abs`) applied to
  `opt.Source` and `opt.BackupDir` before any `os.Stat` / `os.ReadFile` /
  `os.MkdirAll` / `os.Rename` call. Closes CodeQL `go/path-injection` alerts
  #81–#85 (HIGH). The migrate-office CLI runs on the user's own machine
  against their own data, so this is a canonicalisation pass, not a jail —
  but it gives the taint sinks a normalised form.
- **Tightened file/directory permissions** flagged by gosec:
  - `internal/gateway/server.go#writePidFile` — PID file 0o644 → 0o600
    (only pan-agent itself reads it; no group/world reader needed). gosec #90.
  - `internal/gateway/office_csp.go` — CSP violations log 0o644 → 0o600
    (user-local debug log; no external reader). gosec #88.
  - `internal/claw3d/sha-stamp/main.go` — generated `sha.go` 0o644 → 0o600
    (git normalises mode on add anyway, but keeps gosec quiet). gosec #91.
  - `internal/claw3d/migrate.go` backup dir `MkdirAll` 0o755 → 0o750.
    gosec #89.
### Deferred
- Workflow least-privilege `permissions:` blocks for
  `.github/workflows/chaos.yml` and `.github/workflows/e2e-real-webview.yml`
  (CodeQL #78, #79) are staged but not shipped in 0.4.3 — pushing workflow
  changes via the release bot requires the `workflow` OAuth scope which
  the current token lacks. Scheduled for a separate PR once the scope is
  granted; change is trivial (3 lines × 2 files adding `permissions:
  contents: read`).

## [0.4.2] - 2026-04-17

Security + CI hygiene hotfix on top of 0.4.1. No user-visible feature changes;
the Go backend version variable also catches up from 0.4.0 (which was never
bumped when 0.4.1 shipped the desktop bits).

### Security
- **Approval classifier wired into code-execution path.** Level-1 / Level-2
  patterns (`approval.Classify`) now gate `internal/tools/code_execution.go`
  `shellCommand` branches via the SSE `approval_required` round-trip, closing
  the gap where LLM-supplied shell strings reached `exec.Command` without
  passing through the approval UI. `#nosec G204` with rationale added at the
  two call sites (gosec's taint analysis can't follow the approval hop).
- **Skill guard hardened.** 30+ regex patterns across 6 categories (exec, fs,
  net, creds, obfuscation, prompt_injection); proposals with `severity=block`
  findings are rejected before touching disk.
- **42 review findings addressed** across the PR #9 security review, plus a
  CodeQL finding (missing `Secure` attribute on the session cookie).

### Fixed
- **Go backend version string.** `internal/version.Version` was still `0.4.0`
  in 0.4.1 (desktop shipped 0.4.1 in `package.json` / `Cargo.toml` /
  `tauri.conf.json`, but `pan-agent version` reported 0.4.0). Now 0.4.2
  everywhere.
- **CI Lint green.** golangci-lint + staticcheck: deleted ~65 LOC of
  scaffolded-but-unwired code in `internal/claw3d/` (`presenceCoalescer`,
  unused `clientCount`), passed `context.TODO()` into `dispatch` in
  `adapter_test.go`, removed now-unused `restoreFile` helper.
- **Rustfmt green.** `cargo fmt --all --check` now passes on
  `desktop/src-tauri/src/main.rs` (sidecar spawn chain + `eprintln!` blocks
  were drifting against rustfmt since the 0.4.1 sidecar wiring landed).

## [0.4.1] - 2026-04-16

Hotfix for two ship-blocking regressions in 0.4.0, both in the "works-only-if-
Setup-already-ran" class. On a clean install nothing chat-shaped worked: the
Go sidecar never started, and even when started manually, model selection
never persisted to disk.

### Fixed
- **Sidecar never spawned.** `desktop/src-tauri/src/main.rs` now spawns the
  `pan-agent` sidecar in `tauri::Builder::setup()` via `tauri-plugin-shell`'s
  `ShellExt::sidecar()`, with `PAN_AGENT_PARENT_PID` set to the Tauri PID so
  `internal/parentwatch` activates for graceful shutdown when the Tauri
  parent exits. Tauri's `externalBin` config only *bundles* a binary — you
  still have to `spawn()` it explicitly, and 0.4.0's `main.rs` had only the
  `plugin()` / `run()` pair. Net effect in 0.4.0: app installed cleanly, UI
  launched, every `localhost:8642` fetch failed, Setup wizard could not
  proceed.
- **Sidecar lifecycle symmetric with parent.** `RunEvent::ExitRequested`
  handler calls `child.kill()` on the stored `CommandChild` so the sidecar
  is terminated on normal app quit. Together with `parentwatch` in the
  reverse direction (sidecar self-terminates when Tauri is SIGKILLed) this
  gives a symmetric parent ⇄ child lifecycle.
- **Sidecar logs surfaced.** Stdout/stderr from the sidecar are streamed to
  the Tauri process's stderr with a `[pan-agent]` prefix, so crash traces
  show up in `Console.app` / `journalctl` instead of vanishing.
- **PUT /v1/config silently dropped model changes on fresh installs.**
  `internal/config/models.go#SetModelConfig` was early-returning `nil` when
  `config.yaml` didn't exist, so the write looked successful on the wire
  (`{"status":"ok"}`) but nothing hit disk. Next GET returned the empty
  default. Fixed by materialising a minimal `config.yaml` (provider /
  default / base_url / streaming) on the IsNotExist path, with the same
  UI→CLI provider name mapping (regolo → custom) as the update path.
  Covered by two new tests in `internal/config/models_test.go`.
- **Partial PUT /v1/config clobbered baseUrl to empty.** The Settings
  screen has a debounced auto-save that fires on every state change
  including transient half-loaded states, and was PUTing payloads like
  `{provider:"", model:"X", baseUrl:""}` during hydration. 0.4.0's
  handler treated every field as "replace", blanking baseUrl on disk and
  resetting `s.llmClient.BaseURL=""` — the same
  `unsupported protocol scheme ""` chat error reappeared on the second or
  third message of a session. `handleConfigPut` now merges the incoming
  model body against the current on-disk config: empty strings preserve
  the existing value rather than clearing it. UI-side auto-save racing is
  a separate follow-up, but the backend invariant now prevents the class
  of regression regardless of which screen sends a partial payload.
- **Sidebar didn't respond to window resize.** Fixed width 230 px with
  `flex-shrink: 0`, no overflow handling, and a brand logo that filled the
  full sidebar width via Tailwind 4's preflight `img { height: auto }`
  overriding the `<img height={30}>` attribute. Net effect: on a 480 px
  window the sidebar ate 48% of the viewport; on a 500 px-tall window the
  logo and first few nav items consumed the whole rail with the rest
  clipped. Fixed in `desktop/src/assets/main.css`:
  - `.sidebar-nav` gets `flex: 1 1 auto; min-height: 0; overflow-y: auto`
    so long nav lists scroll inside the rail instead of clipping.
  - `.sidebar-brand img` clamps to `width: clamp(40px, 12vh, 160px)` so
    the logo scales with viewport height instead of stretching to fit
    the 230 px sidebar width.
  - `@media (max-width: 640px)` collapses the sidebar to a 64 px
    icon-only rail — hides brand name, nav labels, and footer text while
    keeping Lucide icons visible. Gives the main content area room to
    breathe down to ~400 px total window width.

### Known gap
- CI does not smoke-test the packaged `.app` / `.dmg` on any platform. The
  weekly `e2e-real-webview.yml` job is the only thing that would have caught
  the sidecar regression, and it excludes macOS (no upstream WKWebView
  WebDriver). A proper fix — a minimal "launch packaged app, poll
  `/v1/health`, PUT+GET `/v1/config`, kill" step in `release.yml` — is
  filed as a follow-up for 0.5.0.

### Known gap
- CI does not smoke-test the packaged `.app` / `.dmg` on any platform. The
  weekly `e2e-real-webview.yml` job is the only thing that would have caught
  this, and it excludes macOS (no upstream WKWebView WebDriver). A proper
  fix — a minimal "launch packaged app, poll `/v1/health`, kill" step in
  `release.yml` — is filed as a follow-up for 0.5.0.

## [0.4.0] - 2026-04-15

Claw3D Office embedded natively in pan-agent. The Node sidecar from 0.3.x is
replaced by a Go adapter + static bundle served by the gateway on port 8642.
End-to-end milestones M1–M6 land in this release. See `docs/migration-guide.md`
for the 0.3.x → 0.4.0 upgrade path and `docs/protocol.md` for the frozen
WebSocket contract.

### Added
- **Embedded Claw3D Office** — pre-built Next.js bundle served via `go:embed`
  under `/office/*`; no Node runtime required on the end-user machine.
- **Go adapter** at `internal/claw3d/adapter_server.go` implementing the full
  26-method × 4-event Claw3D WebSocket protocol v3 (ported from the upstream
  `hermes-gateway-adapter.js` reference). Frozen at 0.4.0; see `docs/protocol.md`.
- **Runtime engine toggle** — `office.engine: go|node` config key with
  drain-and-restart via `GET/POST /v1/office/engine`. Legacy Node sidecar path
  remains as a fallback.
- **Migration importer** — `pan-agent migrate-office` CLI ingests existing
  `~/.hermes/clawd3d-history.json` into pan-agent's SQLite. `--dry-run`,
  `--force`, idempotent on identical mtime.
- **Auth polish on `/office/ws`** — per-IP token bucket (burst 20, refill
  5/sec), 3-failure lockout for 30 seconds, optional `office.strict_origin`
  for empty-Origin rejection.
- **WebView2 fallback flow** — WebGL2 probe in `main.tsx` + Go handler at
  `POST /v1/office/fallback-detected` + `FallbackBanner` component.
  7-day `office.browser_fallback_until` window with system-browser open via
  `@tauri-apps/plugin-shell`.
- **CSP observability** — `POST /v1/office/csp-report` collector writes to
  `AgentHome/csp-violations.log` (hard-capped at 10 MB). Viewable via
  `pan-agent doctor --csp-violations`.
- **Chaos tests** — `//go:build chaos` tagged suite under `internal/claw3d/`
  with 2 scenarios (adapter kill, parent-process exit) + cross-platform Go
  helper binary. Weekly CI via `.github/workflows/chaos.yml`.
- **Real-webview E2E matrix** — WebdriverIO v7 + tauri-driver on Windows +
  Linux, 5 specs covering the `/office/*` surface. Weekly cron +
  `workflow_dispatch`. See `.github/workflows/e2e-real-webview.yml`.
- **Doctor extensions** — `pan-agent doctor` gains `--json`,
  `--csp-violations`, `--switch-engine=go|node`, `--deprecated-usage`.
  Adds PID file status check and CSP violations log summary. Gateway writes
  a PID file at `AgentHome/pan-agent.pid` on successful bind.
- **Vendor-sync scheduled workflow** — `.github/workflows/vendor-sync.yml`
  runs weekly, rebases upstream Claw3D patches, rebuilds the bundle, opens a
  draft PR. Accompanied by `CODEOWNERS` coverage and a PR template at
  `.github/PULL_REQUEST_TEMPLATE/vendor_sync.md`.
- **SBOM generation in `release.yml`** — `cyclonedx-gomod` for Go and
  `license-checker-rseidelsohn` for Node, attached as release artifacts.
  Copyleft gate fails the build on unallowlisted AGPL/GPL-3 hits via
  `sbom/allowlist.txt`.
- **Documentation set** — `docs/protocol.md` (frozen WebSocket contract),
  `docs/runbook.md` (operator playbook + rollback + WebView2 manual test),
  `docs/migration-guide.md` (0.3.x → 0.4.0), `docs/bench-ws-2026-Q2.md`
  (placeholder for the deferred gorilla-vs-coder WebSocket bench).
- **SQLite schema** — 5 new tables under `state.db`: `office_agents`,
  `office_sessions`, `office_messages`, `office_cron`, `office_audit`. The
  `office_messages.content_hash` column is backfilled via a one-shot
  migration and indexed (NOT unique — legitimate duplicates are valid).

### Changed
- **`/api/gateway/ws` → `/office/ws`** (breaking for direct WS consumers;
  see §7 of `docs/migration-guide.md`).
- **Rate-limit + lockout wiring** — `sessionStore.mu` now guards both the
  live session map and the lockout map under a single mutex. No public API
  change, but load-bearing for the 3-fail lockout invariant.
- **Doctor subcommand** — flag-based argument parsing via `flag.NewFlagSet`;
  existing behavior preserved as the default code path.

### Deprecated (removal in 0.5.0)
- `PAN_OFFICE_ENGINE` environment variable — use `office.engine` instead.
- `/v1/office/setup|start|stop|logs` legacy lifecycle endpoints — now
  no-ops; retained for one minor-version window.

### Known limitations
- **Windows code-signing deferred.** Installers ship unsigned; users see a
  SmartScreen warning. Acquisition is a 0.5.0 item. See `docs/runbook.md` §11.
- **tauri-driver matrix excludes macOS** — WKWebView has no upstream
  WebDriver. Pending `danielraffel/tauri-webdriver` maturity.
- **WebSocket library benchmark deferred to 0.5.0** — see
  `docs/bench-ws-2026-Q2.md` for the decision record.

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

[0.4.0]: https://github.com/Euraika-Labs/pan-agent/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/Euraika-Labs/pan-agent/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/Euraika-Labs/pan-agent/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/Euraika-Labs/pan-agent/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/Euraika-Labs/pan-agent/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Euraika-Labs/pan-agent/releases/tag/v0.1.0
