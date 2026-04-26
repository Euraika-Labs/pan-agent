# Phase 13 — Memory, Marketplace & SaaS Integration

**Status**: PLANNED — design phase, no implementation yet (2026-04-26).
v0.6.0 just shipped, closing Phase 12; this document scopes the
v0.7.0 → v1.0.0 arc plus the claude-flow orchestration that drives it.

## TL;DR

Phase 13 turns the Phase 12 substrate into user-facing capability. The
agent gains:

1. **Durable memory across sessions** (RAG over the action-journal +
   message corpus via `sqlite-vec`).
2. **Real SaaS integrations** (Gmail / Stripe / Google Calendar tools
   that produce reversible-or-audited receipts).
3. **A skill marketplace** where reviewer/curator-vetted skills can be
   installed without re-implementing the recovery + journal contracts.
4. **A Tasks UI** that closes the v0.5.0 task-runner story user-side.

Plus three smaller polish workstreams (ARIA-YAML cookbook, expanded
deep-links, prompt-history redaction).

## Design principle

**Build on the substrate; don't expand it.** Every Phase 13 workstream
must consume Phase 12 primitives — action journal, recovery,
redaction, per-session budgets, durable task runner, skill curator —
rather than introduce parallel mechanisms. If a slot can't be expressed
in those primitives without bending them, it gets explicitly deferred
to Phase 14, not retro-fitted.

The corollary: the work is more integration than invention. claude-flow
orchestration runs across the whole phase precisely because no single
workstream is greenfield.

---

## The eight workstreams

Numbering re-uses the slot letters from the v0.6.0 retrospective
(A–G); H (Voice I/O) and I (Multi-agent orchestration) are deferred
out of Phase 13 entirely (see "Explicitly NOT doing").

### WS#13.A — Tasks UI

**Scope**: surface `/v1/tasks` in the desktop app. Backend shipped in
v0.5.0 (`internal/taskrunner/` + 7 endpoints); no UI exists today.

**Contracts to consume**:
- `GET /v1/tasks?session_id=…` — list (already in `desktop/src/api.ts`).
- `GET /v1/tasks/{id}` — one row.
- `GET /v1/tasks/{id}/events` — event stream.
- `POST /v1/tasks/{id}/{pause,resume,cancel}` — state transitions.

**New components** (mirrors WS#2 History pattern from v0.6.0):
- `desktop/src/screens/Tasks/Tasks.tsx` — list grouped by session.
- `desktop/src/components/tasks/TaskRow.tsx` — name, status badge,
  cost-vs-cap pill, last-heartbeat timestamp.
- `desktop/src/components/tasks/TaskDetail.tsx` — events drawer.
- Reuse `<CostSparkline>` from `desktop/src/components/history/`.
- Reuse `<UndoConfirmDialog>` pattern for cancel.

**Scope**: ~3-5 person-days. One frontend engineer.

### WS#13.B — RAG / `sqlite-vec` memory

**Scope**: agent gains durable memory across sessions. Vector index
over message bodies + receipt payloads + skill content.

**Spike (Day 1)** before committing the design:
- Build `sqlite-vec` against `modernc.org/sqlite` (must work without
  CGo — pure-Go is a hard project constraint per CLAUDE.md "no CGo, no
  C compiler needed"). Confirm the loadable extension path works.
- Benchmark embedding 10k messages on commodity laptops.
- If pure-Go path doesn't work: fall back to a pure-Go HNSW
  implementation (e.g. `github.com/coder/hnsw`).

**Embedding model**: local — `all-MiniLM-L6-v2` (384-dim) via
`onnxruntime`-go bindings, OR remote — provider's embedding endpoint.
Decision deferred to D5 below.

**Contracts**:
- New `internal/memory/rag/` package with `Embed(text) []float32`,
  `Index(id, vec, meta)`, `Search(query, k) []Result`.
- Hooks into the existing `task_events` stream — every
  `kind=step_completed` event becomes a candidate index entry.
- Hooks into `internal/storage/messages` — assistant messages indexed
  on write.
- New endpoint `GET /v1/memory/search?q=…` returning top-k.
- Chat loop injects top-3 retrieval hits into the system message at
  the cache boundary (same pattern WS#11 used for active skills).

**Scope**: 1-day spike + ~1 wk implementation. Two engineers (backend
+ embedding-model integration).

### WS#13.C — Plugin / skill marketplace

**Scope**: third parties can publish skills; pan-agent users can
install vetted skills without leaving the desktop app.

**Was blocked on**: WS#2 rollback layer (Phase 12). Now unblocked —
every marketplace skill produces ordinary `KindFSWrite` /
`KindBrowserForm` / `KindSaaSAPI` receipts, so a malicious or buggy
skill is rolled back through the same journal as a built-in skill.

**Substrate**:
- Phase 11 reviewer + curator agents already validate skill content
  against the 30+ guard rules in `internal/skills/guard.go`.
- Phase 12 recovery journal records every action regardless of source.
- Per-session cost cap (Phase 12 WS#1) gates skill cost blast radius.

**New surface**:
- `internal/marketplace/` package with index fetcher, signature
  verifier, and install pipeline (download → guard → reviewer
  approval → install).
- Marketplace index hosted at `pan-agent.github.io/skills/index.json`
  (GitHub Pages — no central server). Per-skill manifest signed with
  minisign matching the existing release-binary keypair.
- `desktop/src/screens/Marketplace/Marketplace.tsx` — browse,
  install, review-pending-vs-installed lanes.
- `POST /v1/skills/install` — fetch + verify + queue reviewer.
- Existing reviewer-agent loop (`runReviewerAgent` in
  `internal/gateway/skill_agents.go`) gates installation.

**Scope**: ~2-3 wk. One full-stack engineer + one security-review
pass.

### WS#13.D — SaaS tools (Gmail / Stripe / Google Calendar)

**Scope**: real OAuth-backed tools producing `KindSaaSAPI` receipts.
Substrate (`internal/saaslinks/` URL builders + `SaaSAPIReverser`)
already in v0.6.0.

**Per-tool work**:
- OAuth 2.0 client (PKCE flow for desktop) — credentials stored in
  the Phase 12 keyring, not in `.env`.
- HTTP client wrapping the provider's SDK or REST API.
- Receipt construction: every state-changing call produces a
  `KindSaaSAPI` receipt with `SaaSURL` populated via the
  `internal/saaslinks` builder for that provider.
- Approval gating: any state-changing call (`messages.send`,
  `charges.create`, `events.delete`) goes through
  `internal/approval/` with level 1 (Dangerous).
- Per-tool tests against the provider sandbox.

**Sequencing**:
1. **Gmail first** — most prompt-driven (read/summarise/reply), the
   biggest user-visible win, and `messages.send` reversibility is
   well-understood (un-send window).
2. **Stripe second** — refund flow is API-driven (cleaner reversal
   than Gmail's un-send window).
3. **Google Calendar third** — event create/move/delete maps cleanly
   to receipts.

**Scope**: ~2 wk per provider. One backend engineer per tool, but the
OAuth + receipt-construction patterns are shared so engineer #2 only
pays half.

### WS#13.E — ARIA-YAML cookbook + skills

**Scope**: WS#3 frontend leftover from Phase 12. The `interact` tool
(v0.5.0 backend) routes between direct-API and vision; the direct-API
side needs an accessibility-tree scrape (ARIA-YAML format) and a
starter cookbook of skills that drive native apps without vision.

**Contracts**:
- `internal/tools/interact/` already has the router + safeAppName
  guard.
- New `internal/tools/aria/` — platform-specific accessibility scrape
  (macOS via `AXUIElementCopyAttributeValue`, Windows via UIA, Linux
  via AT-SPI). Returns YAML.
- Cookbook of starter skills: "summarize active Slack channel",
  "create reminder in Reminders.app", "open the file under cursor in
  VS Code".

**Scope**: ~1 wk. Builds on existing `interact` + skill-system
infrastructure.

### WS#13.F — Slack / Notion / Jira deep-links

**Scope**: trivial extension of `internal/saaslinks/` from v0.6.0 —
add three more URL builders matching the same regex-validated pattern.

**Functions to add**:
- `Slack(workspace, channelID, threadTS string) (string, bool)`
- `Notion(databaseOrPageID string) (string, bool)`
- `Jira(host, issueKey string) (string, bool)`

**Scope**: ~1 day. Lands as a single PR, tests included.

### WS#13.G — LLM prompt-history secret redaction

**Scope**: the Phase 12 HMAC redaction (`internal/secret/`) covers
storage paths only — receipts, task events, plan_json. Prompts sent
to the LLM provider currently go un-redacted. WS#13.G adds a
provider-side redaction pipeline.

**Approach**:
- New `internal/secret/llm.go` — `RedactForLLM(messages []Message,
  policy Policy) []Message`. Reuses the existing Presidio recognizers
  but with a different output policy (keep the content human-readable
  for the LLM rather than HMAC-replacing).
- Wire into `internal/llm/client.go` as a pre-send transformer.
- Round-trip test: redacted-prompt → response → un-redact mappings
  in the response if necessary.

**Scope**: ~1 wk. One backend engineer.

### WS#13.H (deferred)

**Voice I/O** — pushed out of Phase 13. Blocked on a pure-Go STT
implementation; whisper.cpp is CGo and breaks the project's no-CGo
constraint. Revisit when (if) a pure-Go STT emerges.

### WS#13.I (deferred)

**Multi-agent orchestration** — pushed out of Phase 13. Phase 11's
10-turn budget already strains single-agent runs; multi-agent needs a
budget redesign first, which is out of scope.

---

## Release slicing

| Release | Contents | Target | Total elapsed |
|---|---|---|---|
| **v0.7.0** | WS#13.A (Tasks UI) + WS#13.E (ARIA-YAML cookbook) + WS#13.F (Slack/Notion/Jira deep-links) — small frontends + sealed-contract expansions | **2-3 wk** | wk 3 |
| **v0.8.0** | WS#13.B (RAG / sqlite-vec) — highest-impact feature, gives the agent memory | **4-5 wk** | wk 8 |
| **v0.9.0** | WS#13.C (Marketplace) — force multiplier; reviewer/curator + recovery substrate exists | **5-6 wk** | wk 14 |
| **v1.0.0** | WS#13.D Gmail tool — first real SaaS integration; opens the audit-lane usability story | **4 wk** | wk 18 |
| **v1.0.x** | WS#13.D Stripe + GCal tools, WS#13.G prompt redaction | **2-3 wk per** | wk 24+ |

**Total Phase 13 runway**: ~18-24 weeks elapsed for the full arc;
v0.7.0 is shippable in ~3 weeks of focused work.

**Buffer policy** (carried over from Phase 12 I6): one-week float
between v0.8.0 and v0.9.0 because RAG quality + index migration are
the critical-path risks. If RAG ships on time, the buffer absorbs into
early marketplace work; if it slips, the marketplace slips with it
rather than getting silently compressed.

---

## claude-flow orchestration

Phase 13 is sized so that running every workstream serially through
one engineer would be ~5 months. Parallelising via claude-flow drops
that to the 18-24-week arc above.

### Swarm topology

```javascript
mcp__claude-flow__swarm_init({
  topology: "hierarchical",
  strategy: "specialized",
  maxAgents: 10,
  config: {
    name: "phase13-orchestration",
    goal: "Sequence v0.7.0 → v1.0.0",
    namespace: "phase13-plan"
  }
})
```

The hierarchical-mesh pattern (one queen, two coordinators, six
executors, one reviewer) matches the Phase 12 cadence we already know
works. `phase13-plan` is the canonical memory namespace; every WS
keeps its plan + decisions there.

### Per-workstream agent assignment

| WS | Lead agent | Reviewer agent | Memory key |
|---|---|---|---|
| 13.A Tasks UI | `frontend-architect` | `code-review-swarm` | `phase13-plan/ws-A` |
| 13.B RAG | `backend-dev` + `ml-developer` | `security-engineer` (vector hallucination + redaction guard) | `phase13-plan/ws-B` |
| 13.C Marketplace | `system-architect` | `security-engineer` (signature chain + supply-chain) | `phase13-plan/ws-C` |
| 13.D SaaS tools | `backend-dev` (per provider) | `code-review-swarm` + `octo:droids:octo-security-auditor` (OAuth flow) | `phase13-plan/ws-D-{gmail,stripe,gcal}` |
| 13.E ARIA-YAML | `mobile-dev` (cross-platform native) | `tester` | `phase13-plan/ws-E` |
| 13.F Deep-links | `backend-dev` | `tester` | `phase13-plan/ws-F` |
| 13.G Prompt redaction | `backend-dev` | `security-engineer` | `phase13-plan/ws-G` |

Lead agents run via the `Agent` tool with the matching `subagent_type`.
The claude-flow MCP swarm tracks state + memory but does not itself
spawn the worker Claude instances — that pattern is inherited from
Phase 12 and continues to work.

### Memory contracts (cross-WS handoffs)

Each WS writes its own design + decisions under `phase13-plan/ws-X`,
plus three shared keys for cross-WS coordination:

- `phase13-plan/contracts/budget` — how much each WS can spend (cost
  cap interplay with WS#1 budgets).
- `phase13-plan/contracts/journal` — how each WS produces journal
  receipts (kind, snapshot tier, reverser).
- `phase13-plan/contracts/redaction` — how each WS feeds redacted
  payloads into receipts vs raw payloads to providers (consumed by
  WS#13.G).

Any WS that violates one of these contracts must surface a decision
update before merging. The reviewer agents check this.

### Sequencing dependencies

```
WS#13.A ──┐                         (independent — small UI)
WS#13.E ──┼──→ v0.7.0
WS#13.F ──┘

           ┌── spike (1d) ──┐
WS#13.B ───┤                ├──→ v0.8.0      (depends on Phase 12 task_events)
           └── implement ───┘

WS#13.C ────────────────────────→ v0.9.0      (depends on Phase 11 reviewer + Phase 12 journal)

           ┌── Gmail ─→ v1.0.0
WS#13.D ───┼── Stripe ─→ v1.0.x   (depends on WS#13.B for "what did the agent already say"
           └── GCal  ──→ v1.0.x    context, but not strictly blocking)

WS#13.G ───────────────────→ v1.0.x          (depends on WS#13.D so it has real
                                              prompt-redaction edge cases to test against)
```

### Per-WS workflow template

```javascript
// 1. Plan
mcp__claude-flow__memory_store({
  namespace: "phase13-plan",
  key: "ws-X/plan",
  value: { scope, contracts, files, scope_days, agent_assignments }
})

// 2. Spawn lead via Agent tool with subagent_type matching the table

// 3. As each slice lands, store progress
mcp__claude-flow__memory_store({
  namespace: "phase13-plan",
  key: "ws-X/progress",
  value: { commits_landed, tests_added, decisions_made, blockers }
})

// 4. Reviewer agent runs after each PR opens, results stored at
//    phase13-plan/ws-X/review-{n}
```

Reviewer output drives the merge decision. Phase 12's review pattern
(specialist plan → implement → reviewer pass → fix) carries over.

---

## Decisions

Tracked here so future-us can audit them without re-deriving from PR
discussion. Pattern carries over from Phase 12.

| # | Question | Decision | Rationale |
|---|---|---|---|
| **D1** | Vector store | `sqlite-vec` (with pure-Go HNSW fallback if extension load fails) | Same DB as everything else; single backup story; pure-Go preserved |
| **D2** | Marketplace index hosting | GitHub Pages (`pan-agent.github.io/skills/index.json`) | No central server to operate; signed manifests carry trust |
| **D3** | OAuth credential storage | Phase 12 keyring (per-profile) | Already audited; no new secret store |
| **D4** | Marketplace skill review gate | Phase 11 reviewer agent must pass before install | Reuses existing guard.go + persona contract |
| **D5** | Embedding model | **TBD — pending spike** | Pure-Go ONNX vs remote provider call; spike output drives this |
| **D6** | Prompt-redaction default | Opt-in per-profile (off by default in v1.0.x; on by default in v1.1.x) | Avoid silent prompt mutation; let users see the diff first |
| **D7** | SaaS tool ordering (D first, Stripe second, GCal third) | Per WS#13.D | User-impact ordering, not engineering complexity |
| **D8** | ARIA-YAML format | YAML over JSON | Human-readable in skill bodies; matches existing skill markdown |
| **D9** | Marketplace skill versioning | semver strict; pan-agent core declares min/max compatible versions | Avoid the npm range-spec hell |
| **D10** | Voice I/O / multi-agent | Deferred to Phase 14 | Pure-Go STT + budget redesign are prerequisites |

Open questions (need answers before each WS starts):

- **Q1 (WS#13.B)**: do we re-embed on every launch (slow but always
  fresh) or maintain a watch on `task_events` (fast but trickier
  index maintenance)?
- **Q2 (WS#13.C)**: should marketplace skills run in a stricter
  approval level by default than user-authored skills (e.g. always
  Level 1 even if guard.go would pass them at Level 0)?
- **Q3 (WS#13.D)**: Gmail message-send reversibility — within the
  un-send window we can call `messages.delete`, but after that the
  receipt becomes audit-only. How do we surface that transition to
  the History UI?
- **Q4 (WS#13.G)**: redaction round-trip — when the LLM mentions a
  redacted token in its response, do we un-redact in the displayed
  output or leave the token visible? Privacy vs UX trade-off.

---

## Risk register

| # | Risk | Mitigation |
|---|---|---|
| **I1** | RAG hallucination — agent treats retrieval hit as ground truth and confidently emits stale info | Cosine threshold ≥ 0.75 for inclusion; system message frames hits as "you previously discussed this — verify before using" rather than fact |
| **I2** | Marketplace supply-chain — malicious skill smuggles past reviewer | Two-key signing (per-publisher + pan-agent infra key); reviewer agent runs against guard.go before install; install always requires user confirmation |
| **I3** | OAuth token theft (WS#13.D) | Tokens live in keyring (Phase 12 WS#1); per-tool least-privilege scopes; refresh-token revocation on profile delete |
| **I4** | sqlite-vec extension load fails on user platform | Spike (D1) verifies pure-Go fallback path; CI builds against both backends |
| **I5** | RAG embedding privacy — embeddings leak content via vector similarity attacks | Embeddings stored locally, never shipped to provider; prompt-redaction (WS#13.G) covers the LLM-call side |
| **I6** | WS#13.B critical-path overrun | One-week buffer between v0.8.0 and v0.9.0; if RAG slips, marketplace slips visibly rather than silently |
| **I7** | Gmail un-send-window race (Q3) | UI surfaces "reversible until HH:MM" countdown; once expired, row transitions to audit-only with the deep-link active |
| **I8** | Marketplace skill cost blast radius | Per-session cost cap (Phase 12 WS#1) auto-applies; marketplace skills cannot raise it |
| **I9** | Auto-update across schema migrations | Phase 12 migration pattern (additive columns, idempotent backfill) extends to RAG index column |

---

## Explicitly NOT doing

These come up regularly enough that calling them out keeps scope
debates short.

- **Voice I/O** — slot H in the retro table. Blocked on pure-Go STT.
- **Multi-agent orchestration** — slot I. Needs budget redesign.
- **Plugin sandboxing beyond approval/rollback** — the existing
  approval-level + journal substrate is the trust model; we don't add
  per-skill VMs.
- **Centralized marketplace server** — GitHub Pages + signed
  manifests is enough; we don't run infrastructure.
- **Universal SaaS provider auto-discovery** — Phase 13 hand-picks
  three providers (D); a generic OpenAPI-driven tool generator is
  Phase 14 territory if at all.
- **Prompt-history full transcript export** — redaction (WS#13.G)
  covers the LLM-call path; verbatim transcript export is its own
  privacy surface.
- **Cross-device session sync** — pan-agent stays single-device.
- **GUI workflow builder** — skills are markdown + curator agent;
  no graphical authoring tool.

---

## Origin

Phase 12 wrap-up retrospective on 2026-04-26 produced a slot table
(A–I) listing every deferred / unblocked item. This document
sequences those slots into a release roadmap, closes the design gaps
with explicit decisions D1–D10, and sets up the claude-flow
orchestration that drives parallel execution.

Companion artefacts:

- Phase 12 design: `docs/design/phase12.md`
- v0.6.0 changelog entry: `CHANGELOG.md` `[0.6.0] - 2026-04-26`
- claude-flow swarm bookkeeping: `phase13-orchestration` swarm,
  `phase13-plan` memory namespace.
