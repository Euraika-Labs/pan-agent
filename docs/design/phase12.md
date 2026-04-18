# Phase 12 — Trust-First Desktop Automation

**Status**: PROPOSED — v3 incorporates multi-provider Deliver-phase corrections (2026-04-17)
**Prior phase**: Phase 11 (self-healing skill system) shipped in v0.3.1
**Stabilization track since 0.3.1**: v0.4.0 (Claw3D embed), v0.4.1 (sidecar spawn hotfix), v0.4.2-0.4.4 (security hardening)

## TL;DR

Phase 12 turns pan-agent from "can control a PC" into "can be trusted to control a PC". Five workstreams, ~10-11 dev-weeks for one maintainer (revised up from 6-7 in v1 after Deliver-phase validation surfaced platform-specific engineering depth), sliced across v0.5.0 through v0.8.0 with an explicit 1-week float between WS#2 and WS#3. The design principle — arrived at through a 3-round multi-model debate and hardened by a Discover → Define → Develop → Deliver `/embrace` pass — is **ship capability and the recovery layer together, or users will hit the trust ceiling before the trust floor exists.**

## Design principle

> *"Trust > reach. Browser persistence plus vision plus autonomous task running without rollback is how you get irreversible file edits, sent emails, bad purchases, and silent account mutations. Users try one real SaaS workflow, watch the agent almost do the right thing, and retreat to read-only."*
>
> — Codex, round 3 of the Phase 12 debate

## The five workstreams

### 1. Browser persistence + per-task cost budget (~1.5 weeks)

**Why together**: browser persistence unlocks SaaS access — the single feature where effort-to-impact ratio is most extreme. Cost budgets cap the runaway-vision-loop failure mode that a persistent, authenticated agent makes genuinely dangerous (one screenshot-every-2-seconds GPT-4o loop = $30 / 15 min).

**Files touched**:
- `internal/tools/browser_*.go` — patch `rod.Launcher.UserDataDir()` to `<DataDir>/browser-profile/`. Pin Chromium build (D1). Secure-profile hardening:
  - Store a pan-agent browser-profile master key via `internal/secret/keyring` — a pure-Go wrapper that dispatches to:
    - **Windows**: `github.com/danieljoos/wincred` (MIT, pure Go via DPAPI syscalls — no CGo)
    - **macOS**: shell-out to `security` CLI (ships with macOS, no CGo, no C-library linking)
    - **Linux**: `github.com/godbus/dbus/v5` against Secret Service (pure Go, no CGo)
  - Launch Chromium with `--use-encryption-for-profile-data`
  - **Verify at startup** that Chromium profile encryption is backed by the OS keyring, NOT Chromium's hardcoded Safe Storage fallback (R1). Fail-closed with a Setup banner in desktop sessions; log WARN + degrade in CI/headless mode
  - Launch flags: `--disable-extensions --disable-background-networking --disable-component-update` (R2). Opt-in path documented for extension-dependent workflows
  - Monitor `SingletonLock` to refuse concurrent profile hijacking
- `internal/storage/` — add `token_budget_used`, `token_budget_cap`, `cost_used_usd`, `cost_cap_usd` columns (to new `task_budgets` table when WS#4 lands; to `sessions` table meanwhile)
- `internal/gateway/chat.go` — enforce caps in the SSE tool-call loop. Two-stage UX: emit `budget.warning` at **80% consumption** (amber banner, non-blocking) and at 100% transition to `paused` status, preserve state, emit `budget.exceeded`, require user resume/raise-cap/cancel. Terminate semantics removed — tasks are paused, not killed (Devin pause-not-terminate pattern).
- `desktop/src/screens/` — show per-session cost inline as a persistent pill (`cost_used_usd / cost_cap_usd`, dollar amounts, not tokens). Amber banner at 80%, red blocking state at 100% with `[Increase limit] [End session]` CTAs.

**New dependencies**: `github.com/danieljoos/wincred` (Windows) and `github.com/godbus/dbus/v5` (Linux). Both MIT, pure Go, no CGo. macOS uses the system `security` CLI (no dep).

**Constraint check**: ✅ Pure Go on all three platforms, no CGo introduced. Browser profile stays in `<DataDir>/`, no cloud sync.

### 2. Action journal + rollback layer (3-4 weeks) — the moat

**The differentiation bet**. No competitor pairs local-first with true post-hoc reversibility. Manus/Devin work in remote VMs; Claude Desktop has no persistence; OpenInterpreter's `--safe_mode` is git-based but only for code. Pan-agent can own this space.

**Scope clarification**: The action journal covers filesystem edits outside `<DataDir>/browser-profile/`, shell commands, and tool outputs. It **does NOT attempt to snapshot the live browser profile** — SQLite cookies.db + cookies-journal cannot be cloned atomically while Chromium is running, and the result would be unrestorable. Browser-form submissions are always audit-only receipts regardless of snapshot-tier availability.

**What "action journal" means**: every mutating tool call produces a receipt with before-state, after-state, intent, actor, timestamp, reversal status, and (where applicable) a SaaS deep-link for audit-lane actions. Receipts are the substrate for:
- `/v1/recovery/{list,undo,diff}` — reverse the last N actions within a session
- UI timeline of "what did the agent do today"
- Input to future features (task runner resume, audit export, policy review)

**Architecture — new package `internal/recovery/` and shared `internal/secret/`**:
```
internal/secret/
  keyring.go           — pure-Go keyring wrapper (platform dispatch)
  redaction.go         — HMAC-SHA256 deterministic masking + Presidio-ported regex classifiers
                         Used by recovery journal AND task_events AND LLM-context sanitization points

internal/recovery/
  journal.go            — append-only SQLite log: action_receipts table
  snapshot.go           — platform-aware tiered snapshot interface (D4) with capability-probing
  snapshot_darwin.go    — shell-out to `cp -c` (macOS native APFS clone flag)
  snapshot_linux.go     — shell-out to `cp --reflink=always` (btrfs/xfs/bcachefs)
  snapshot_copy.go      — plain os.CopyFS fallback (capped)
  reversers.go          — per-tool reverse() registry (fs, shell) — no browser
  endpoints.go          — /v1/recovery/{list,undo,diff}
  reaper.go             — heartbeat watcher (shared with WS#4)
```

**Snapshot strategy — tiered CoW with capability probing (D4)**:
The tier is NOT determined by OS name — it's determined by probing the destination filesystem at runtime. APFS-over-SMB, btrfs across subvolumes, RO snapshots, and `nodatacow` files all break naive OS-name matching.

1. **Probe step** — attempt a 1-byte clone/reflink in the destination's parent directory. Verify success. Cache result by `(device_id, mount_id)` for subsequent snapshots on the same mount.
2. **Tier 1 (CoW)** — if probe succeeds: `cp -c` on macOS (APFS) or `cp --reflink=always` on Linux (btrfs/xfs/bcachefs). O(1) per file.
3. **Tier 2 (plain copy fallback)** — `os.CopyFS` with hard cap at **50 MB / 500 files**; above cap, action is demoted to audit-only with `snapshot_tier='audit_only'` and a UI warning.
4. **Cross-device operations and RO snapshots**: always tier 2 or audit-only.

**Secret masking (D5) — moved to shared `internal/secret/`**:
- HMAC-SHA256 deterministic masking with a per-profile key stored in the OS keyring. Preserves cross-action correlation (same API key in steps 3 and 7 shows as the same masked token) without leaking the value
- Presidio-compatible regex patterns ported to Go for credentials, tokens, PII, API keys
- **Applied at THREE write points** to prevent secondary leakage:
  1. `action_receipts.redacted_payload` (this workstream)
  2. `tasks.plan_json` (WS#4)
  3. `task_events.payload_json` (WS#4)
- Redacting secrets from LLM prompt history before it reaches the provider is still Phase 13 scope; this covers the storage-side exposure.

**Reversal semantics by tool family**:
- **Filesystem writes/deletes (outside browser profile)**: tiered CoW snapshot before write — *reversible lane*. Receipt includes `snapshot_tier` field so UI can distinguish O(1) clones from slow copies.
- **Shell commands**: capture stdout/stderr/exit-code receipt; for destructive commands (detected via approval-classifier patterns), attempt CoW snapshot of likely-affected paths — *reversible lane when snapshot succeeds; audit-only otherwise*
- **Browser form submissions**: always *audit-only lane* — no snapshot of live profile. Receipt captures form payload + target URL + form-field inventory.
- **Email send / calendar create / payment submissions**: receipt captures full payload + destination + provider object ID + SaaS deep-link (Gmail sent URL, Stripe charge admin URL, calendar event deep-link) — *audit-only lane with remediation link*. Link captured during the action, not reconstructed after.
- **File downloads**: receipt includes path + hash — *reversible lane* (reversal = delete)

**`action_receipts` schema**:
```sql
CREATE TABLE action_receipts (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  event_id INTEGER,                 -- FK to task_events(id) when produced by a running task
  kind TEXT NOT NULL,               -- 'fs_write' | 'fs_delete' | 'shell' | 'browser_form' | ...
  snapshot_tier TEXT NOT NULL,      -- 'cow' | 'copyfs' | 'audit_only'
  reversal_status TEXT NOT NULL,    -- 'reversible' | 'audit_only' | 'reversed_externally' | 'irrecoverable'
  redacted_payload TEXT,            -- HMAC-masked copy for display
  saas_deep_link TEXT,              -- provider-side object URL or undo surface
  created_at INTEGER NOT NULL,
  FOREIGN KEY (task_id) REFERENCES tasks(id),
  FOREIGN KEY (event_id) REFERENCES task_events(id)
);
CREATE INDEX idx_action_receipts_task_created ON action_receipts(task_id, created_at);
```

**SQLite runtime requirements**:
- `PRAGMA journal_mode=WAL` mandatory (2MB screenshot writes during UI reads produce `SQLITE_BUSY` without WAL)
- Main writer goroutine for receipts/events, **separate lightweight writer for heartbeats** (C3: prevents reaper races when main writer is busy)
- `SetMaxOpenConns(1)` for the main writer, prepared statements, batched inserts per tool step
- No per-field indexes on hot-path columns; index asynchronously on `(task_id, created_at)` only

**Desktop UI — two-lane layout** (D2): the History screen splits receipts into two swim-lanes:
- **Reversible actions** — per-action "Undo" button, grouped by task, collapse completed undos. Never reorder post-revert: show "Reverted at 14:32" stamp instead of moving to audit lane.
- **Audit receipts** — read-only, grayed, with full payload inspection, SaaS deep-link button where available ("Open in Gmail", "View in Stripe"), and "Action recorded — requires manual reversal in [App]" label for actions without a remediation URL

**Constraint check**: ✅ Pure Go (shell-outs to `cp`, `security`, `dbus` where applicable), no CGo. SQLite via existing `modernc.org/sqlite`.

### 3. Vision with provider-passthrough abstraction (~1 week)

**The reframe from round 3**. The round-2 plan had a naive base64-image-url pipeline. Round 3 surfaced two critiques:
- **Human-Mimicry Fallacy** (Gemini) — "computer use" ≠ "click pixels." Often `osascript` or Win32 UIAutomation or AT-SPI beats vision for SaaS apps.
- **Provider-shift** (Sonnet) — Anthropic has shipped computer-use APIs; Google is shipping; OpenAI is close. Custom coordinate-extraction pipelines will be wrong in 6 months.

**Solution**: interaction hierarchy with single-tool LLM surface and explicit internal routing:

```
internal/tools/interact/
  direct_api.go        — AppleScript/osascript (macOS), PowerShell+UIAutomation (Win), AT-SPI (Linux)
  accessibility.go     — ARIA-YAML observation (Playwright locator.ariaSnapshot() pattern)
                         NOT the deprecated accessibility.snapshot() API
  vision.go            — two backends:
                         a) base64 image_url (legacy, current-gen Claude/GPT-4o) with resize to ≤1024px
                         b) provider computer_use passthrough (Anthropic computer-use tool etc.)
  coordinate.go        — last-resort pixel click
  router.go            — internal-only: selects {layer, confidence, reason} from tool availability +
                         platform capability checks. Exposed to LLM ONLY as one canonical `interact`
                         tool that takes intent+constraints and returns the chosen layer/result.
                         Per-layer tools are internal; the LLM never sees "click pixel" vs "osascript"
                         as separate tool names, so the LLM cannot disagree with the router (I4).
```

**LLM surface**: ONE tool — `interact(intent, constraints)`. The agent never picks a layer directly; the router does. The skill-injection layer explains what `interact` can do but is documentation only, not routing.

**Direct-API cookbook** (D3): ~20 handcrafted scripts ship as first-class skills via the existing Phase 11 skill system (Mail.app send, Calendar.app create, Finder move, Safari URL-open, Notes append on macOS; PowerShell+UIAutomation equivalents on Windows; dbus-send snippets on Linux). Reviewer/curator agents refine them from usage telemetry. LLMs extend for edge cases.

**Prior-art read requirement before kickoff**:
- [lavague-ai/LaVague](https://github.com/lavague-ai/LaVague) — Action Engine NL-to-Selenium/Playwright pattern
- [theredsix/cerebellum](https://github.com/theredsix/cerebellum) — Graph-search browser agent, small enough to read end-to-end
- [OpenInterpreter](https://github.com/OpenInterpreter/open-interpreter) Computer API — primitive set reference
- [Playwright ARIA snapshots](https://playwright.dev/docs/aria-snapshots) — YAML observation format

**Resize pipeline** (Sonnet's gotcha): all screenshots resized to ≤1024px wide before encode. Added to the `vision.go` backend-a path.

**Constraint check**: ✅ Pure Go (osascript/powershell are shell-outs), no CGo. Provider computer_use is just a different tool-call shape in the existing LLM client.

### 4. Durable task runner (~3 weeks) — the spine

Now has #2 to depend on, so unattended execution has a rollback surface.

**Schema** (revised with Deliver-phase invariants):
```sql
CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  plan_json TEXT,                  -- HMAC-redacted via internal/secret (I1)
  status TEXT NOT NULL,            -- queued|running|paused|zombie|succeeded|failed|cancelled
  session_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_heartbeat_at INTEGER,       -- updated every ~10s by running task (R3)
  next_plan_step_index INTEGER,    -- step memoization (renamed from resume_from_step_id for clarity — I2)
  token_budget_cap INTEGER,
  cost_cap_usd REAL
);
CREATE INDEX idx_tasks_session_created ON tasks(session_id, created_at);

CREATE TABLE task_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  step_id TEXT NOT NULL,           -- stable identifier per plan step
  attempt INTEGER NOT NULL,        -- retry attempt counter
  sequence INTEGER NOT NULL,       -- monotonic per-task ordering
  kind TEXT NOT NULL,              -- tool_call|approval|journal_receipt|artifact|cost|error|
                                   -- heartbeat|step_completed
  payload_json TEXT,               -- HMAC-redacted via internal/secret (I1)
  created_at INTEGER NOT NULL,
  FOREIGN KEY (task_id) REFERENCES tasks(id),
  UNIQUE (task_id, step_id, kind, attempt)
);
CREATE INDEX idx_task_events_task_seq ON task_events(task_id, sequence);
```

**Reaper goroutine** (C3-hardened):
- Dedicated heartbeat write path separate from the main writer queue (so a blocked main writer can't starve heartbeats and produce false zombies)
- Compare-and-swap transition: `UPDATE tasks SET status='zombie' WHERE id=? AND status='running' AND last_heartbeat_at < ?` — won't re-transition a task already moved to `paused`/`succeeded`/`cancelled`
- Uses monotonic process time for in-process liveness decisions; wall-clock time only for the persisted `last_heartbeat_at` value
- Runs at 10s cadence with a 60s stale threshold

**Step memoization**: `task_events` entries with `kind='step_completed'` let a resumed task skip already-done steps. `next_plan_step_index` on the task row points at the next step to execute. `(task_id, step_id, kind, attempt)` uniqueness prevents re-executing a completed step during resume.

**Endpoints**: `POST /v1/tasks`, `GET /v1/tasks`, `GET /v1/tasks/{id}`, `GET /v1/tasks/{id}/events` (SSE), `POST /v1/tasks/{id}/pause`, `POST /v1/tasks/{id}/resume`, `POST /v1/tasks/{id}/cancel`.

**Budget-exceeded semantics**: transition to `paused`. State preserved. Notification emitted. Task waits for user action. Devin ACU pattern.

**Migration path for `cron/jobs.json`**: becomes a thin runner that creates tasks instead of owning its own execution. Preserves existing cron jobs; they transparently gain history + cost tracking + rollback.

**Desktop UI — Tasks screen**:
- **Header row per task**: task name, status badge, total cost, duration, timestamp + cost-over-time spark-line (SVG, no deps, Datadog pattern)
- **Expanded task view**: reverse-chronological event stream, Linear-style — each event shows tool name, verb, target, timestamp, cost delta. Tool calls that produced journal receipts show a receipt icon and link to the WS#2 swim-lane view
- **Filter bar**: "Show reversible only" / "Show audit only" / "Show errors"
- **Day grouping**: standard activity-log convention

**Constraint check**: ✅ Pure Go, SQLite. No new deps beyond WS#2's `internal/secret`.

### 5. macOS permission onboarding wizard (~3 days) — ship-gate

Not a feature. A bounce fix. Every macOS user hits a 3-prompt wall (Accessibility, Screen Recording, Automation) before the agent takes a screenshot. Today: manual grant required.

**Files touched**:
- `desktop/src-tauri/src/permissions.rs` — probe TCC status via the public APIs only (I5). The `TCC.db` query fallback from v2 is removed — on macOS 14+ it's SIP-protected and unreliable even with Full Disk Access.
  - **Accessibility**: `AXIsProcessTrustedWithOptions(nil)` returns trusted vs not-trusted; to distinguish "never prompted" from "previously denied", attempt a no-op `AXUIElementCopyAttributeValue` and inspect the result
  - **Screen Recording**: `CGPreflightScreenCaptureAccess()` + `CGRequestScreenCaptureAccess()` — reliably distinguishes granted/denied/not-determined
  - **Automation**: attempt an osascript no-op and inspect error code -1743 (= denied)
- `desktop/src/screens/Setup/Permissions.tsx` — Setup wizard step. Deep-link to `x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility`. **Poll every 1s** during grant flow; flip to green-check immediately when the user toggles the switch in System Settings (Raycast pattern). When previously denied, change CTA label from "Grant Access" → **"Open Settings (previously denied)"**.

**Scope downgrade** (I5): v0.5.0 delivers "reliably detect granted vs not-granted; best-effort classify denied vs never-prompted." Exact MDM pre-grant/pre-block classification is NOT a v0.5.0 gate. MDM detection is best-effort via `profiles -P` shell-out, documented as not fully reliable, surfaced in a diagnostics export rather than in the wizard step.

**Flow pattern**:
- **Accessibility + Screen Recording** (core capabilities) — **block-until-granted** non-dismissable panel (Rewind pattern). Wizard's "Finish" button gated on grants.
- **Automation** (optional per-target-app) — **contextual prompt** on first use (CleanShot pattern). Agent works without it; just can't use direct-API scripts in WS#3.

**Constraint check**: ✅ Adds Rust `security-framework` crate (MIT). No runtime change.

## Release slicing

| Release | Contents | Target |
|---|---|---|
| **v0.4.5** (hotfix) | `rod.Launcher.UserDataDir()` plumbing with **ephemeral-only profile** — profile is cleared on agent exit. No persistent auth, no keyring, no extension flags yet. Scoped explicitly to prove the plumbing without shipping the trust-ceiling problem the round-3 debate rejected (C4). | Immediate |
| **v0.5.0** | WS#1 full (persistent profile + keyring verification + extension flags + SingletonLock + 80/100 budget UX) + WS#5 full (TCC detection without TCC.db + 1s polling + block-until-granted) + DB migration harness + WAL mode baseline | ~1.5 weeks after start |
| **v0.6.0** | WS#2 full (action journal + capability-probed tiered CoW + `internal/secret` HMAC redaction + Presidio classifiers + SaaS deep-links + two-lane History UI) | ~4 weeks after v0.5.0 |
| **BUFFER** | 1-week float for WS#2 overrun risk (I6). WS#2 is the critical path — tiered CoW capability probing, redaction, and two-lane UI are all platform-specific engineering. If WS#2 ships on time, use the buffer to start WS#3 early; if it overruns, WS#7 and WS#8 slip together rather than silently compressing. | 1 week |
| **v0.7.0** | WS#3 (vision w/ ARIA-YAML + single canonical `interact` tool + confidence-scored internal router + direct-API cookbook skills) | ~1 week after buffer |
| **v0.8.0** | WS#4 (task runner + C3-hardened reaper + step memoization + pause-not-terminate budgets + Tasks UI) | ~3 weeks after v0.7.0 |

**Total Phase 12 runway: ~10.5-11 weeks elapsed** (was 8 in v1, 9.5-10 in v2). Delta from v2 is the explicit 1-week buffer between WS#2 and WS#3 (critical-path protection).

## Explicitly deferred to Phase 13

- **RAG / semantic file indexing** — needs a 1-day sqlite-vec spike first; lands on top of the task runner's event stream
- **Semantic system awareness** (clipboard history, active window polling, recent-files watcher) — adds per-platform complexity; belongs after the direct-API layer (#3) exists
- **Voice I/O** — whisper.cpp is CGo; revisit only if a pure-Go STT emerges
- **Multi-agent orchestration** — Phase 11's 10-turn budget already strains single-agent runs
- **Plugin / skill marketplace** — workstream #2's rollback layer is prerequisite
- **Exhaustive SaaS deep-link library** — v0.6.0 covers Gmail + Stripe + Google Calendar
- **LLM prompt-history secret redaction** — HMAC masking covers storage paths (receipts + task_events + plan_json); pre-provider prompt sanitization is a separate pipeline change
- **Full MDM profile introspection** — best-effort `profiles -P` in diagnostics export only
- **Content-defined dedup (restic/kopia/borg-grade)** — tiered CoW + audit-only degradation is sufficient

## Explicitly NOT doing

- **Bundled Chromium** — single-binary violation, license complication
- **Cloud sync of sessions/memory** — zero-telemetry violation
- **Windows-Recall-style always-on screen archiving** — privacy nightmare, storage explosion
- **Snapshotting the live browser profile** (C2) — Chromium SQLite files cannot be cloned atomically while the browser is running; rollback would be corrupt. Browser-form receipts are always audit-only.

## Decisions

| # | Decision | Chosen | Rationale |
|---|---|---|---|
| **D1** | Chromium version strategy for workstream #1 | **Pin Chromium, bump monthly via script** | Profile migration across Chromium user-data-dir formats is a rabbit hole. Pinning freezes that surface. Monthly bumps are acceptable for headless agent contexts. |
| **D2** | Action journal reversal UX | **Shown, grayed, in a separate "audit-only" swim-lane** | Filtering unreversible actions destroys the forensic trail. Two swim-lanes keep the Undo button honest while preserving auditability. |
| **D3** | Direct-API cookbook scope for workstream #3 | **Ship ~20 curated scripts as first-class skills; let LLM extend for edge cases** | LLM synthesis of AppleScript is unreliable. Shipping the 80% path deterministically with a curated set gives reference material the Phase 11 reviewer/curator refines. |
| **D4** | Snapshot strategy for reversible actions | **Tiered CoW with runtime capability probing**: `cp -c` (macOS APFS) or `cp --reflink=always` (Linux btrfs/xfs/bcachefs) when probe succeeds; plain `os.CopyFS` with 50MB/500-file cap otherwise; audit-only above cap or for cross-device / RO-snapshot / non-CoW volumes | Plain `os.CopyFS` on large workspaces is unusably slow. Native CoW is O(1) on supported filesystems, but OS name doesn't predict support (APFS-over-SMB, btrfs RO snapshots both fail). Capability probe cached by `(device_id, mount_id)` is the honest path. Shell-out to `cp` avoids CGo. |
| **D5** | Secret masking | **HMAC-SHA256 deterministic masking in `internal/secret/` applied to `action_receipts.redacted_payload`, `tasks.plan_json`, AND `task_events.payload_json`; Presidio-compatible regex for detection; per-profile key stored via platform keyring** | Masking only receipts leaves a secondary leak in task-runner storage (I1). Deterministic hash preserves correlation without exposing values. Keyring dispatch stays pure-Go via `wincred` / `security` CLI / dbus. |

## Risk-acceptance register

Risks explicitly NOT addressed in Phase 12, with justification:

| Risk | Decision | Justification |
|---|---|---|
| **modernc SQLite 1.17-1.78x slower than mattn** | **Accept** | Switching to mattn/go-sqlite3 reintroduces CGo. Mitigate with WAL + single main writer + separate heartbeat writer + no per-field hot-path indexes. |
| **Chromium hardcoded-password fallback in CI/headless** | **Accept for CI, enforce for desktop** | Headless CI has no desktop threat model. Verification fails-closed only in interactive desktop sessions. |
| **Exhaustive SaaS deep-link library** | **Partial — cover Gmail/Stripe/Google Calendar in v0.6.0** | Uncovered services show raw payload inspection + "no undo link available" rather than a broken link. |
| **Service worker persistence after agent exit** | **Mitigate + accept residual** | `--disable-background-networking` stops most SW background work. Going further breaks legitimate SPAs. Does NOT persist across Chromium restart. |
| **Secrets in LLM prompt history** | **Phase 13** | HMAC masking covers all storage paths (receipts + task_events + plan_json) as of D5. Pre-provider prompt sanitization is separate pipeline work. |
| **Corporate MDM full profile introspection** | **Accept for v0.5.0** | Basic granted/not-granted detection via public APIs lands. MDM pre-block classification is best-effort via diagnostics export only. |
| **Live browser profile rollback** | **Accept — excluded from CoW scope (C2)** | SQLite multi-file atomicity cannot be preserved during Chromium writes. Browser-form receipts are always audit-only. |
| **Restic/Kopia/Borg-grade content-defined dedup** | **Phase 13** | Tiered CoW + audit-only degradation is sufficient for v0.6.0. |

## Open questions still owed a decision

These surfaced in Define and remain unanswered. Block v0.5.0 kickoff.

1. **Schema migration for existing v0.3.x users**: automated migration vs fresh DB vs opt-in export? Has a migration harness been tested against a real user DB?
2. **Keychain/Keyring rotation recovery**: when the user resets their macOS login password, the Keychain key rotates and browser-profile encryption fails. Offer profile reset? Ephemeral mode? Prompt for export before rotation? Silent data loss is not acceptable.
3. **CoW fallback on ExFAT / network volumes**: warn at Setup wizard if workspace is on a non-CoW filesystem? Per-action receipt-lane downgrade? Block the task runner entirely on such volumes?
4. **Redaction false-positive unmask affordance**: users will see legitimate content masked as secrets. Provide "reveal original" in receipt detail? How does that not re-introduce leakage in screen-share contexts?
5. **MDM-managed macOS scope**: accept v0.5.0 shipping with "basic detect + diagnostics-export for MDM" or push full support to v1.0?

## Origin

This plan is v3. Revision history:
- **v1** — Output of a 3-round multi-model debate (Gemini + Codex + Sonnet + Claude/Opus). Round 3 attacked the v1 consensus and promoted undo/rollback from Phase 13 to Phase 12 #2.
- **v2** — Discover → Define pass added 8 risks (R1-R8), 2 new Decisions (D4 tiered-CoW, D5 HMAC masking), revised slicing +1.5 weeks.
- **v3 (this doc)** — Deliver-phase validation caught 4 critical + 6 important issues in v2:
  - **C1** corrected: keyring + APFS cloning dep choices reworked to actually-pure-Go paths (wincred / security CLI / dbus + shell-out to `cp -c`)
  - **C2** corrected: browser profile excluded from CoW scope; browser-form receipts always audit-only
  - **C3** corrected: reaper uses separate heartbeat writer + CAS transition + monotonic process time
  - **C4** corrected: v0.4.5 scoped to ephemeral-only profiles
  - **I1** corrected: `internal/secret` masking applied to tasks.plan_json and task_events.payload_json, not just receipts
  - **I2** corrected: WS#4 schema gains step_id / attempt / sequence + UNIQUE + FKs
  - **I3** corrected: tiered CoW uses runtime capability probing cached by device/mount id
  - **I4** corrected: LLM sees ONE canonical `interact` tool; router is internal-only
  - **I5** corrected: TCC.db fallback dropped; public macOS APIs only
  - **I6** corrected: explicit 1-week buffer between v0.6.0 and v0.7.0

Embrace artefacts live at:
- `~/.claude-octopus/debates/pan-agent-20260417/001-pan-agent-phase12-competitive-gap/` (original 3-round debate)
- `~/.claude-octopus/results/probe-synthesis-20260417-225054.md` (Discover)
- `~/.claude-octopus/results/grasp-consensus-20260417-225054.md` (Define)
- `~/.claude-octopus/results/tangle-validation-20260417-225054.md` (Develop)
- `~/.claude-octopus/results/delivery-20260417-225054.md` (Deliver — lists C1-C4 / I1-I6)
