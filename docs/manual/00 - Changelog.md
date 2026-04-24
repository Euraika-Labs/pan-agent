# Changelog

This is the manual's narrative changelog. The authoritative, Keep-a-Changelog-formatted entries live in the repo root `CHANGELOG.md` — the summaries below track that file with a lighter tone suitable for readers of the manual. When the two diverge, the root file wins.

## Unreleased — post-0.4.4 stabilisation on `main`

Targeted at v0.5.0. The next cut will also wire up Phase 12 WS2 into tool execution (see `docs/design/phase12.md`).

- **Phase 12 foundation landed (unreleased):**
  - **WS1 — `internal/secret/`** — OS keyring wrapper + HMAC-based redaction layer for approval prompts and any future journal entries. Scaffolded, not yet wired.
  - **WS2 — `internal/recovery/`** — action journal + filesystem/registry/browser snapshots + reverser interface + `/v1/recovery/{list,undo,diff}` endpoints. Routes reachable on `main`; journal still empty unless populated in tests.
- **Docs:** New top-level `AGENTS.md` (contributor guide). `CLAUDE.md` refreshed for v0.4.4 + the Phase 12 foundation (Go 1.25.7, 56 routes, descriptions for `internal/recovery/`, `internal/secret/`, `internal/parentwatch/`, `internal/paths/`).
- **Desktop:** `desktop/tsconfig.json` dropped the deprecated `baseUrl` entry; path mappings resolve relative to the tsconfig dir via `moduleResolution: "bundler"`. Unblocks TypeScript 6.0 compatibility.
- **CI: `golangci-lint` green on `main`.** `gofmt -w` pass on nine files in `internal/recovery/` and `internal/secret/` that landed unformatted in PRs #15–#16 plus cleanup of residual `staticcheck`/`unused` findings. Dependabot PRs now skip the Tauri signing job (`github.actor != 'dependabot[bot]'`) since bot PRs can't read `TAURI_SIGNING_PRIVATE_KEY` — .deb/.rpm/.AppImage/.msi/.dmg still compile cleanly.
- **Security:** Cleared CodeQL `go/path-injection` alerts #81–#85, #95 on `internal/claw3d/migrate.go` by restructuring `sanitizeMigrationPath` results into local `cleanSource`/`cleanBackup` variables used directly at every `os.*` sink (identical runtime behaviour; legible to static analysis). Dismissed as won't-fix — the migrate-office CLI runs on the user's own machine against their own `.hermes` data and canonicalisation is the intent, not a jail. Dependabot alert #3 (LOW, `rand` crate unsound with custom logger): bumped 0.8.5 → 0.8.6 in `desktop/src-tauri/Cargo.lock`; advisory-only for us.
- **Dependencies:** TypeScript 5.9.3 → 6.0.3 (PR #18). GitHub Actions bumps (PR #19): checkout v4→v6, setup-go v5→v6, setup-node v4→v6, upload-artifact v4→v7, peter-evans/create-pull-request v6→v8. Go modules (PR #21): `jezek/xgb` 1.3.0→1.3.1, `slack-go/slack` 0.22→0.23, `golang.org/x/sys` 0.42→0.43, `golang.org/x/text` 0.32→0.36 (Unicode 17), `modernc.org/sqlite` 1.48.2→1.49.1 (SQLite 3.53.0).

## 2026-04-17 — v0.4.4 workflow permissions follow-through

Completes the 0.4.3 security-scanner pass. No runtime behaviour change — ships only the two workflow-permission blocks that were deferred from 0.4.3 because the release bot token lacked the `workflow` OAuth scope. Added `permissions: contents: read` to `.github/workflows/chaos.yml` and `.github/workflows/e2e-real-webview.yml`. Closes CodeQL `actions/missing-workflow-permissions` #78 and #79 (MEDIUM).

## 2026-04-17 — v0.4.3 security scanner follow-up

Security scanner follow-up on 0.4.2. Clears 11 outstanding alerts on `main` from CodeQL (5 HIGH + 2 MEDIUM) and gosec (4 errors). No behaviour changes, no new features. Highlights: `internal/claw3d/migrate.go` gains `sanitizeMigrationPath` (`filepath.Clean` → `filepath.Abs`) applied before every `os.Stat` / `os.ReadFile` / `os.MkdirAll` / `os.Rename` call to close CodeQL `go/path-injection` #81–#85 (HIGH); tighter gosec-flagged perms (`writePidFile` 0o644→0o600, `office_csp.go` violations log 0o644→0o600, `sha-stamp/main.go` generated `sha.go` 0o644→0o600, migrate.go backup `MkdirAll` 0o755→0o750). The two workflow-permission blocks (CodeQL #78, #79) were staged but deferred to 0.4.4 — the release bot token didn't have the `workflow` OAuth scope to push workflow YAML.

## 2026-04-17 — v0.4.2 approval wiring + CI hygiene

Security + CI hygiene hotfix on top of 0.4.1. No user-visible feature changes; the Go backend version variable also catches up from 0.4.0 (`internal/version.Version` had drifted — `pan-agent version` reported 0.4.0 even on 0.4.1).

- **Approval classifier wired into code-execution path.** Level-1/Level-2 patterns (`approval.Classify`) now gate `internal/tools/code_execution.go`'s `shellCommand` branches via the SSE `approval_required` round-trip, closing the gap where LLM-supplied shell strings reached `exec.Command` without passing through the approval UI. `#nosec G204` with rationale added at the two call sites.
- **Skill guard hardened.** 30+ regex patterns across six categories (exec/fs/net/creds/obfuscation/prompt_injection); proposals with `severity=block` findings are rejected before touching disk.
- **42 review findings** addressed across the PR #9 security review, plus a CodeQL missing-`Secure`-attribute session-cookie fix.
- **CI green.** `golangci-lint` + `staticcheck` — deleted ~65 LOC of scaffolded-but-unwired code in `internal/claw3d/` (`presenceCoalescer`, unused `clientCount`), passed `context.TODO()` into `dispatch` in `adapter_test.go`, removed now-unused `restoreFile` helper. `cargo fmt --all --check` now passes on `desktop/src-tauri/src/main.rs` (sidecar spawn chain + `eprintln!` blocks had been drifting against rustfmt since the 0.4.1 sidecar wiring).

## 2026-04-16 — v0.4.1 sidecar-spawn + model-persist hotfix

Hotfix for two ship-blocking regressions in 0.4.0, both in the "works-only-if-Setup-already-ran" class. On a clean install nothing chat-shaped worked: the Go sidecar never started, and even when started manually, model selection never persisted to disk.

- **Sidecar never spawned (fixed).** `desktop/src-tauri/src/main.rs` now spawns the `pan-agent` sidecar in `tauri::Builder::setup()` via `tauri-plugin-shell`'s `ShellExt::sidecar()`, with `PAN_AGENT_PARENT_PID` set to the Tauri PID so `internal/parentwatch` activates for graceful shutdown when the parent exits. Tauri's `externalBin` config only *bundles* a binary — you still have to `spawn()` it explicitly. `RunEvent::ExitRequested` calls `child.kill()` so sidecar lifecycle is symmetric with the parent. Stdout/stderr from the sidecar are streamed to Tauri's stderr with a `[pan-agent]` prefix.
- **`PUT /v1/config` silently dropped model changes on fresh installs (fixed).** `internal/config/models.go#SetModelConfig` was early-returning `nil` when `config.yaml` didn't exist, so writes looked successful on the wire but nothing hit disk. Fixed by materialising a minimal `config.yaml` on the `IsNotExist` path, with the same UI→CLI provider-name mapping (regolo → custom) as the update path.
- **Partial `PUT /v1/config` clobbered `baseUrl` to empty (fixed).** Settings-screen debounced auto-save was PUTing payloads like `{provider:"", model:"X", baseUrl:""}` during hydration; 0.4.0's handler treated every field as "replace", blanking `baseUrl` on disk and resetting `s.llmClient.BaseURL=""`. `handleConfigPut` now merges against the current on-disk config: empty strings preserve the existing value rather than clearing it.
- **Sidebar didn't respond to window resize (fixed).** Fixed-width 230 px rail with a logo that filled it via Tailwind 4 preflight now gets `flex: 1 1 auto; min-height: 0; overflow-y: auto` on `.sidebar-nav` and `width: clamp(40px, 12vh, 160px)` on the brand logo; a `@media (max-width: 640px)` rule collapses to a 64 px icon-only rail.
- **Known gap (filed for 0.5.0):** CI does not smoke-test the packaged `.app` / `.dmg` on any platform. The weekly `e2e-real-webview.yml` is the only job that would have caught the sidecar regression, and it excludes macOS (no upstream WKWebView WebDriver).

## 2026-04-15 — v0.4.0 Claw3D Office embedded natively

Claw3D Office embedded natively in pan-agent. The Node sidecar from 0.3.x is replaced by a Go adapter + static bundle served by the gateway on port 8642. End-to-end milestones M1–M6 land in this release. See `docs/migration-guide.md` for the 0.3.x → 0.4.0 upgrade path and `docs/protocol.md` for the frozen WebSocket contract.

- **Embedded Claw3D Office** — pre-built Next.js bundle served via `go:embed` under `/office/*`; no Node runtime required on the end-user machine.
- **Go adapter** at `internal/claw3d/adapter_server.go` implementing the full 26-method × 4-event Claw3D WebSocket protocol v3 (ported from the upstream `hermes-gateway-adapter.js` reference). Frozen at 0.4.0; see `docs/protocol.md`.
- **Runtime engine toggle** — `office.engine: go|node` config key with drain-and-restart via `GET/POST /v1/office/engine`. Legacy Node sidecar path remains as a fallback.
- **Migration importer** — `pan-agent migrate-office` CLI ingests existing `~/.hermes/clawd3d-history.json` into pan-agent's SQLite. `--dry-run`, `--force`, idempotent on identical mtime.
- **Auth polish on `/office/ws`** — per-IP token bucket (burst 20, refill 5/sec), 3-failure lockout for 30 seconds, optional `office.strict_origin` for empty-Origin rejection.
- **WebView2 fallback flow** — WebGL2 probe in `main.tsx` + Go handler at `POST /v1/office/fallback-detected` + `FallbackBanner` component. 7-day `office.browser_fallback_until` window with system-browser open via `@tauri-apps/plugin-shell`.
- **CSP observability** — `POST /v1/office/csp-report` collector writes to `AgentHome/csp-violations.log` (hard-capped at 10 MB). Viewable via `pan-agent doctor --csp-violations`.
- **Chaos tests** — `//go:build chaos` tagged suite under `internal/claw3d/` with two scenarios (adapter kill, parent-process exit) + cross-platform Go helper binary. Weekly CI via `.github/workflows/chaos.yml`.
- **Real-webview E2E matrix** — WebdriverIO v7 + tauri-driver on Windows + Linux, five specs covering the `/office/*` surface. Weekly cron + `workflow_dispatch`. See `.github/workflows/e2e-real-webview.yml`.
- **Doctor extensions** — `pan-agent doctor` gains `--json`, `--csp-violations`, `--switch-engine=go|node`, `--deprecated-usage`. Adds PID file status check and CSP violations log summary. Gateway writes a PID file at `AgentHome/pan-agent.pid` on successful bind.
- **Vendor-sync scheduled workflow** — `.github/workflows/vendor-sync.yml` runs weekly, rebases upstream Claw3D patches, rebuilds the bundle, opens a draft PR. Accompanied by `CODEOWNERS` coverage and a PR template.
- **SBOM generation in `release.yml`** — `cyclonedx-gomod` for Go and `license-checker-rseidelsohn` for Node, attached as release artifacts. Copyleft gate fails the build on unallowlisted AGPL/GPL-3 hits via `sbom/allowlist.txt`.
- **New docs** — `docs/protocol.md` (frozen WebSocket contract), `docs/runbook.md` (operator playbook + rollback + WebView2 manual test), `docs/migration-guide.md` (0.3.x → 0.4.0), `docs/bench-ws-2026-Q2.md` (placeholder for the deferred gorilla-vs-coder WebSocket bench).
- **SQLite schema** — five new tables under `state.db`: `office_agents`, `office_sessions`, `office_messages`, `office_cron`, `office_audit`. `office_messages.content_hash` backfilled via a one-shot migration and indexed (NOT unique — legitimate duplicates are valid).
- **Breaking:** `/api/gateway/ws` → `/office/ws` for direct WS consumers; see §7 of `docs/migration-guide.md`.
- **Deprecated (removal in 0.5.0):** `PAN_OFFICE_ENGINE` env var (use `office.engine` instead). `/v1/office/setup|start|stop|logs` legacy lifecycle endpoints — now no-ops; retained for one minor-version window.
- **Known limitations:** Installers ship unsigned, so Windows users see a SmartScreen warning — code-signing acquisition is a 0.5.0 item (see `docs/runbook.md` §11). `tauri-driver` matrix excludes macOS — WKWebView has no upstream WebDriver, pending `danielraffel/tauri-webdriver` maturity. WebSocket library benchmark deferred to 0.5.0 — see `docs/bench-ws-2026-Q2.md` for the decision record.

## 2026-04-14 — v0.3.1 bug fix + CI hardening

Patch release. Fixes a Phase 11 regression caught during a manual end-to-end smoke test: `walkSkillsDir` was enumerating `_proposed/`, `_archived/`, `_history/`, `_merged/`, `_rejected/` as if they were regular skill categories, leaking UUID-named "skills" into `/v1/skills` and the LLM-facing skills-inventory injection in `chat.go`. Fixed by excluding underscore-prefixed dirs in the walker with a regression test.

Also closes the chronic gosec false-positive noise in GitHub Code Scanning: 43 alerts dismissed with rationale (G104/G204/G304/G703/G704), 3 real perm-tightening fixes in `filesystem.go` (0o644→0o600 on files, 0o755→0o750 on dirs), 2 `// #nosec` with rationale (G117 Tavily request body, G122 Walk TOCTOU threat model), and `lint.yml`'s gosec job now excludes the noise rules going forward with an explicit rationale block. From 62 open alerts → 0.

Release workflow now publishes SHA256 hashes, a Windows Defender false-positive note, and VirusTotal analysis links in every release body, and auto-opens a tracking issue with the manual WDSI submission checklist. README's "Windows SmartScreen" section expanded into a fuller "Windows SmartScreen & Defender" section.

`.golangci.yml` migrated to v2 schema with a lean linter set; `lint.yml` clippy job builds the Go sidecar before linting (mirrors the `ci.yml` tauri pattern); gosec job got `security-events: write` for SARIF upload. Removed the workflow-driven CodeQL config that conflicted with the repo's default-setup CodeQL.

## 2026-04-14 — v0.3.0 Phase 11 self-healing skill system

Major release shipping the reviewer + curator agent loops on top of the hermes-parity skill manager. Agents can now propose new/edited skills mid-task, have them reviewed (approved / refined / merged / rejected), and have an independent curator agent re-arrange the active library over time based on real usage data.

**Phase 11 — self-healing:**
- **Proposal queue** at `<ProfileSkillsDir>/_proposed/<uuid>/`. Main agent's `skill_manage(action=create|edit|...)` writes here rather than mutating active state. Each proposal carries `ProposalMetadata` (UUID, trust tier, source, status, intent, intent targets, intent new category, intent reason) and the SKILL.md body, plus a guard-scan result.
- **Guard scanner** with 30+ regex patterns across 6 categories (exec, fs, net, creds, obfuscation, prompt_injection). Blocks proposals with `severity=block` findings before they reach disk.
- **History snapshots** at `_history/<category>/<name>/SKILL.<timestamp_ms>.md`, reversible in both directions (rollbacks snapshot the current version too). New endpoints `GET /v1/skills/history/{category}/{name}` + `POST .../rollback`.
- **Atomic writes** (temp file + rename) everywhere in the skill manager, with rollback of the proposal dir on guard-blocked content.
- **Reviewer agent** — bundled `reviewer.md` persona, `skill_review` tool (list/get/approve/reject/merge), `runReviewerAgent` bounded 10-turn LLM loop, endpoint `POST /v1/skills/reviewer/run`. Approve is intent-aware so curator-originated proposals trigger `ApplyCuratorIntent` for the right side-effect (archive losers, materialise split children, rename recategorised dir).
- **Curator agent** — bundled `curator.md` persona, `skill_curator` tool (list_active_with_usage + 5 propose actions), `runCuratorAgent`, endpoint `POST /v1/skills/curator/run`. Curator writes into the same proposal queue the reviewer consumes, with intent metadata (`refine`/`merge`/`split`/`archive`/`recategorize`).
- **Usage tracking** in a new SQLite table `skill_usage` with indexes by skill, session, and time. `storage.DB.LogSkillUsage / ListSkillUsage / GetSkillUsageStats`. Endpoints `GET /v1/skills/usage/{category}/{name}` + `.../stats`.
- **Chat integration**: skills inventory injected as a stable user message at a cache-friendly boundary (not in the system prompt — preserves the Anthropic prompt cache across turns). Tool calls on `skill_view` / `skill_manage` are logged to `SkillUsage` for curator input.
- **10 new HTTP endpoints** under `/v1/skills/` bringing the total to 50.
- **Path containment** uniformly enforced by `resolveActiveDir` / `resolveProposalDir` / `resolveHistoryDir` helpers using `filepath.Rel`. Same sanitiser pattern CodeQL recognises (zero open `go/path-injection` alerts).
- **22 new unit tests** across `internal/skills/` covering proposal lifecycle, history+rollback, guard blocking, curator intents + apply, path-traversal rejection at every entry.

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
