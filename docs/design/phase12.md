# Phase 12 — Trust-First Desktop Automation

**Status**: PROPOSED (drafted 2026-04-17, awaiting owner approval)
**Prior phase**: Phase 11 (self-healing skill system) shipped in v0.3.1
**Stabilization track since 0.3.1**: v0.4.0 (Claw3D embed), v0.4.1 (sidecar spawn hotfix), v0.4.2-0.4.4 (security hardening)

## TL;DR

Phase 12 turns pan-agent from "can control a PC" into "can be trusted to control a PC". Five workstreams, ~6-7 dev-weeks for one maintainer, sliced across v0.5.0 through v0.8.0. The design principle — arrived at through a 3-round multi-model debate — is **ship capability and the recovery layer together, or users will hit the trust ceiling before the trust floor exists.**

## Design principle

> *"Trust > reach. Browser persistence plus vision plus autonomous task running without rollback is how you get irreversible file edits, sent emails, bad purchases, and silent account mutations. Users try one real SaaS workflow, watch the agent almost do the right thing, and retreat to read-only."*
>
> — Codex, round 3 of the Phase 12 debate

The round-2 consensus was **capability-first**: browser persistence → vision → task runner. Round 3 reversed that: three independent models converged on "undo/rollback was wrongly deferred." Phase 12 lands the capability wave and the recovery wave **together**, in that order within each release.

## The five workstreams

### 1. Browser persistence + per-task cost budget (~1 week)

**Why together**: browser persistence unlocks SaaS access — the single feature where effort-to-impact ratio is most extreme. Cost budgets cap the runaway-vision-loop failure mode that a persistent, authenticated agent makes genuinely dangerous (one screenshot-every-2-seconds GPT-4o loop = $30 / 15 min).

**Files touched**:
- `internal/tools/browser_*.go` — patch `rod.Launcher.UserDataDir()` to `<DataDir>/browser-profile/`. **Chromium version pinned** (see decision D1): override `rod.Launcher.Bin()` to a specific downloaded Chromium build so profile dirs stay stable across pan-agent releases. Monthly script bumps the pin + runs a smoke-test profile load before release.
- `internal/storage/` — add `token_budget_used`, `token_budget_cap`, `cost_used_usd`, `cost_cap_usd` columns to sessions (or a new task-budgets table if #4 lands in the same release).
- `internal/gateway/chat.go` — enforce caps in the SSE tool-call loop; emit `budget.exceeded` event and halt.
- `desktop/src/screens/` — show per-session cost inline; budget-exceeded banner.

**Competitive parity**: Manus/Devin have persistent sessions; Cursor shows per-conversation cost. Pan-agent has neither.

**Constraint check**: ✅ Pure Go, no CGo, no new runtime. Browser profile stays in `<DataDir>/`, no cloud sync.

### 2. Action journal + rollback layer (2-3 weeks) — the moat

**The differentiation bet**. No competitor pairs local-first with true post-hoc reversibility. Manus/Devin work in remote VMs; Claude Desktop has no persistence; OpenInterpreter's `--safe_mode` is git-based but only for code. Pan-agent can own this space.

**What "action journal" means**: every mutating tool call produces a receipt with before-state, after-state, intent, actor, timestamp, and reversal status. Receipts are the substrate for:
- `/v1/undo/last` — reverse the last N actions within a session
- UI timeline of "what did the agent do today"
- Input to future features (task runner resume, audit export, policy review)

**Architecture — new package `internal/recovery/`**:
```
internal/recovery/
  journal.go       — append-only SQLite log: action_receipts table
  snapshot.go      — pre-execution fs snapshots to <DataDir>/recovery/<session-id>/
  reversers.go     — per-tool reverse() registry (fs, shell, browser-form)
  endpoints.go     — /v1/recovery/{list,undo,diff}
```

**Reversal semantics by tool family**:
- **Filesystem writes/deletes**: snapshot affected paths before write → `os.CopyFS` (Go 1.23+) — *reversible lane*
- **Shell commands**: capture stdout/stderr/exit-code receipt; for destructive commands (detected via approval-classifier patterns), require snapshot of likely-affected paths inferred from the command string — *reversible lane when snapshot exists; audit-only otherwise*
- **Browser form submissions**: capture the form payload + target URL as a "soft receipt" — *audit-only lane* (cannot reverse a sent POST)
- **Email send / calendar create / payment submissions**: receipt captures full payload + destination — *audit-only lane*, grayed in UI
- **File downloads**: receipt includes path + hash — *reversible lane* (reversal = delete)

**Desktop UI — two-lane layout** (decision D2): the History screen splits receipts into two swim-lanes:
- **Reversible actions** — with per-action "Undo" button, grouped by task
- **Audit receipts** — read-only, grayed, with full payload inspection but no undo control. Preserves the forensic trail for "did the agent send that email?" without falsely implying reversibility.

**Constraint check**: ✅ Pure Go, SQLite-native, no CGo. Uses `modernc.org/sqlite` already present.

### 3. Vision with provider-passthrough abstraction (~1 week)

**The reframe from round 3**. The round-2 plan had a naive base64-image-url pipeline. Round 3 surfaced two critiques:
- **Human-Mimicry Fallacy** (Gemini) — "computer use" ≠ "click pixels." Often `osascript` or Win32 UIAutomation or AT-SPI beats vision for SaaS apps.
- **Provider-shift** (Sonnet) — Anthropic has shipped computer-use APIs; Google is shipping; OpenAI is close. Custom coordinate-extraction pipelines will be wrong in 6 months.

**Solution**: interaction hierarchy with automatic fallback:
```
internal/tools/interact/
  direct_api.go        — AppleScript/osascript (macOS), PowerShell+UIAutomation (Win), AT-SPI (Linux)
  accessibility.go     — system accessibility tree query
  vision.go            — two backends:
                         a) base64 image_url (legacy, current-gen Claude/GPT-4o)
                         b) provider computer_use passthrough (Anthropic computer-use tool etc.)
  coordinate.go        — last-resort pixel click (today's behavior)
  router.go            — picks highest layer that can plausibly handle the request
```

**Agent prompting**: the skill injection layer (`internal/gateway/chat.go`) explains the hierarchy to the LLM so it reaches for the right layer first.

**Direct-API cookbook** (decision D3): ~20 handcrafted scripts ship as first-class skills via the existing Phase 11 skill system, letting reviewer/curator agents refine them over time based on usage telemetry. LLMs can still synthesize novel scripts for edge cases, but the curated set covers the 80% path (Mail.app send, Calendar.app create, Finder move, Safari URL-open, Notes append on macOS; equivalent PowerShell+UIAutomation on Windows; dbus-send snippets on Linux). Pure LLM synthesis of AppleScript is famously unreliable — the cookbook ensures deterministic behavior where it matters.

**Resize pipeline** (Sonnet's gotcha): all screenshots resized to ≤1024px wide before encode. Added to the vision.go backend-a path.

**Constraint check**: ✅ Pure Go (osascript/powershell are shell-outs), no CGo. Provider computer_use is just a different tool-call shape in the existing LLM client.

### 4. Durable task runner (2-3 weeks) — the spine

Now has #2 to depend on, so unattended execution has a rollback surface.

**Schema**:
```sql
CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  plan_json TEXT,        -- LLM-produced plan artifact
  status TEXT,           -- queued|running|paused|succeeded|failed|cancelled
  session_id TEXT,
  created_at INTEGER,
  token_budget_cap INT,
  cost_cap_usd REAL,
  ...
);
CREATE TABLE task_events (
  id INTEGER PRIMARY KEY,
  task_id TEXT,
  kind TEXT,             -- tool_call|approval|journal_receipt|artifact|cost|error
  payload_json TEXT,
  created_at INTEGER
);
```

**Endpoints**: `POST /v1/tasks`, `GET /v1/tasks`, `GET /v1/tasks/{id}`, `GET /v1/tasks/{id}/events` (SSE), `POST /v1/tasks/{id}/pause`, `POST /v1/tasks/{id}/resume`, `POST /v1/tasks/{id}/cancel`.

**Migration path for `cron/jobs.json`**: becomes a thin runner that creates tasks instead of owning its own execution. Preserves existing cron jobs; they transparently gain history + cost tracking.

**Desktop UI**: new "Tasks" screen (replaces/augments existing "History"); each task row links to the journal receipts from workstream #2.

**Constraint check**: ✅ Pure Go, SQLite. No new deps.

### 5. macOS permission onboarding wizard (2-3 days) — ship-gate

Not a feature. A bounce fix. Every macOS user hits a 3-prompt wall (Accessibility, Screen Recording, Automation) before the agent takes a screenshot. Today: manual grant required. Goal: detect missing perms and deep-link to System Settings panes.

**Files touched**:
- `desktop/src-tauri/src/permissions.rs` — new Rust module. Probe TCC status via Rust `security-framework` (MIT-licensed crate) where possible; otherwise shell out to `tccutil` status checks.
- `desktop/src/screens/Setup/Permissions.tsx` — new step in the Setup wizard that gates progression on perm grants and links to `x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility`.

**Constraint check**: ✅ Adds one Rust crate (`security-framework`, MIT). No runtime change.

## Release slicing

| Release | Contents | Target |
|---|---|---|
| **v0.4.5** (hotfix) | Prototype only: `rod.Launcher.UserDataDir()` — ship it this weekend as a 2-day patch. Not gated by Phase 12 planning. | Immediate |
| **v0.5.0** | Workstream #1 (cost budgets) + workstream #5 (macOS onboarding) | ~1 week after start |
| **v0.6.0** | Workstream #2 (action journal + rollback) | ~3 weeks after v0.5.0 |
| **v0.7.0** | Workstream #3 (vision w/ passthrough) | ~1 week after v0.6.0 |
| **v0.8.0** | Workstream #4 (task runner, end of Phase 12) | ~3 weeks after v0.7.0 |

Total Phase 12 runway: ~8 weeks elapsed, ~6-7 dev-weeks of actual work.

## Explicitly deferred to Phase 13

- **RAG / semantic file indexing** — needs a 1-day sqlite-vec spike first; lands on top of the task runner's event stream
- **Semantic system awareness** (clipboard history, active window polling, recent-files watcher) — adds per-platform complexity; belongs after the direct-API layer (#3) exists
- **Voice I/O** — whisper.cpp is CGo; revisit only if a pure-Go STT emerges
- **Multi-agent orchestration** — Phase 11's 10-turn budget already strains single-agent runs
- **Plugin / skill marketplace** — workstream #2's rollback layer is prerequisite (3rd-party tools without rollback = unbounded blast radius)

## Explicitly NOT doing

- **Bundled Chromium** — single-binary violation, license complication
- **Cloud sync of sessions/memory** — zero-telemetry violation
- **Windows-Recall-style always-on screen archiving** — privacy nightmare, storage explosion

## Decisions

| # | Decision | Chosen | Rationale |
|---|---|---|---|
| **D1** | Chromium version strategy for workstream #1 | **Pin Chromium, bump monthly via script** | Profile migration across Chromium user-data-dir formats is a rabbit hole (cookies.db schema, localStorage sqlite, session tokens all shift). Pinning freezes that surface. Headless agent contexts expose the Chromium CVE surface less than a primary browser, so monthly bumps are acceptable. |
| **D2** | Action journal reversal UX | **Shown, grayed, in a separate "audit-only" swim-lane** | Filtering unreversible actions out destroys the forensic trail ("did the agent send that email?") — exactly the question driving trust-ceiling retreat. Two swim-lanes keep the Undo button honest while preserving auditability. |
| **D3** | Direct-API cookbook scope for workstream #3 | **Ship ~20 curated scripts as first-class skills; let LLM extend for edge cases** | LLM synthesis of AppleScript is unreliable (models hallucinate from adjacent languages, AppleScript quoting quirks). Shipping the 80% path (Mail, Calendar, Finder, Safari, Notes) as curated skills gives deterministic behavior and a reference library. The Phase 11 reviewer/curator system refines them from usage telemetry. |

## Origin

This plan is the output of a 3-round multi-model debate (4 participants: Gemini, Codex, Sonnet, Claude/Opus). The debate artifacts — context, round transcripts, final synthesis — live at `~/.claude-octopus/debates/pan-agent-20260417/001-pan-agent-phase12-competitive-gap/`. The round-3 attack on the round-2 consensus is the reason undo/rollback is in Phase 12 rather than deferred to Phase 13.
