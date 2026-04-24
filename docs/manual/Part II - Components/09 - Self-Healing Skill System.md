# Self-Healing Skill System

Phase 11 (shipped in v0.3.0, stabilised across the 0.4.x line) turns `<ProfileSkillsDir>/` from a static, user-curated folder into a workspace the agent itself proposes into, a reviewer agent curates, and a curator agent refines over time. This chapter covers the data model, the two agent loops, and how chat integration threads it together.

> **Status:** Phase 11 is complete and the current production surface. The next phase (Phase 12 — trust-first desktop automation, design in `docs/design/phase12.md`) is in early foundation: WS1 (`internal/secret/`) and WS2 (`internal/recovery/`) have landed as unreleased code on `main` but are not yet wired into tool execution. They are independent of the skill system described here.

## Why

Hermes-agent has a working "create a skill mid-task" pattern — but it's `skill_manage` writing directly to the active catalog, with only a security guard between agent output and active state. That's fine for a single-user tool but loses track of *why* a skill was created, which ones are actually useful, and whether they should be refined / merged / archived as usage accumulates.

Pan-Agent keeps hermes's six-action `skill_manage` tool (the data plane) and layers three extra pieces on top:

1. **Proposal queue** — `skill_manage` writes to `_proposed/<uuid>/`, not the active catalog. A reviewer agent decides what actually lands.
2. **Curator agent** — runs on demand (or a slow cron), reads active-skill usage stats, and proposes refinements/merges/splits/archives to the same queue.
3. **History + rollback** — every mutation to an active skill snapshots the prior version so changes are reversible.

None of this is in hermes. The security guard, atomic writes, and trust tiers are kept verbatim.

## Filesystem layout

```
<ProfileSkillsDir>/
├── <category>/<name>/              ← active, LLM-visible
│   ├── SKILL.md
│   └── _metadata.json              session_id, created_at, usage_count, intent, source
├── _proposed/<uuid>/               ← awaiting reviewer
│   ├── SKILL.md
│   ├── _metadata.json
│   └── split_children/             ← for curator split proposals
├── _rejected/<uuid>/               ← audit trail, preserves metadata
├── _merged/<uuid>/                 ← curator merge losers (parent_ids cross-reference)
├── _archived/<uuid>/               ← DeleteActiveSkill destination
└── _history/<category>/<name>/     ← rollback snapshots
    └── SKILL.<timestamp_ms>.md
```

All five underscore-prefixed directories are **excluded** from `ListInstalled` (the source feed for `GET /v1/skills` and the LLM-facing skills inventory). Only directories under true category names are exposed.

## Path containment

Every helper that constructs a path from agent-supplied `category` / `name` / `id` funnels through:

- `resolveActiveDir(profile, category, name)` — validates both names against the strict regex (`^[a-z0-9][a-z0-9._-]*$`, ≤64 chars), joins into `<profile>/skills/<category>/<name>`, then confirms via `filepath.Rel` that the result is strictly inside `ProfileSkillsDir`. Rejects `..`, slashes, Windows-reserved chars.
- `resolveProposalDir(profile, id)` — validates the UUID-shape id, same containment check against `_proposed/`.
- `resolveHistoryDir(profile, category, name)` — same containment check against `_history/`.
- `splitAndResolveActiveID(profile, "<category>/<name>")` — combines split + resolve for tool-supplied `<cat>/<name>` IDs.

This matches the sanitiser pattern CodeQL's `go/path-injection` query recognises. gosec's `G304` doesn't track the taint correctly through `filepath.Rel` guards, which is why the `lint.yml` gosec job excludes G304 with explicit rationale.

## Proposal metadata

`ProposalMetadata` (`internal/skills/metadata.go`) is serialised as `_metadata.json` alongside every SKILL.md under `_proposed/`, `_rejected/`, `_merged/`, and (once promoted) the active directory:

```go
type ProposalMetadata struct {
    ID           string   // uuid.New().String()
    Name         string
    Category     string
    Description  string
    TrustTier    string   // "agent-created" | "trusted" | "community" | "builtin"
    CreatedBy    string   // session id that produced it
    CreatedAt    int64    // millis
    Source       string   // "agent" | "curator" | "reviewer" | "user"
    UsageCount   int
    LastUsedAt   int64
    Status       string   // "proposed" | "active" | "rejected" | "merged" | "archived"
    RejectReason string
    ParentIDs    []string // merged proposals cross-reference survivor

    // Curator-only fields — empty for plain "create" proposals
    Intent            string   // "" | "refine" | "merge" | "split" | "archive" | "recategorize"
    IntentTargets     []string // active-skill ids the intent acts on
    IntentNewCategory string   // destination for "recategorize"
    IntentReason      string   // curator's justification
}
```

The `Intent` field is what lets the reviewer's approve action know whether to promote a SKILL.md (`create`/`refine`/`merge`) or perform a non-content side-effect (`archive`/`recategorize`/`split`). See "Reviewer approve — intent dispatch" below.

## Guard scanner

`internal/skills/guard.go` + `guard_patterns.go` ship 30+ patterns across 6 categories:

| Category | Examples |
|---|---|
| `exec` | `rm -rf /`, `mkfs.* /dev/`, `dd of=/dev/`, bash forkbomb, powershell `-EncodedCommand`, `eval(input())` |
| `fs` | path traversal (`../` repeated), `shutil.rmtree('/')`, wildcard `os.remove`, `chmod 000` |
| `net` | `curl \| bash` / `wget \| sh`, reverse-shell `nc -e/-l`, private-IP URLs, `socket.connect`, base64-fetch-and-exec |
| `creds` | private key PEM headers, AWS access keys (`AKIA…`), OpenAI `sk-…`, GitHub `ghp_…`, Slack `xox[baprs]-…` |
| `obfuscation` | `base64.decode` followed by `exec()`, hex-escape blobs, `chr()` concat chains, `String.fromCharCode` chains |
| `prompt_injection` | "ignore previous instructions", "disregard the system prompt", "developer mode enabled", `<system>`-style tag injection, zero-width Unicode chars |

Each pattern has `severity: "block"` or `"warn"`. The scan runs against every proposal on write and every reviewer-refined rewrite on approve. Any `block` finding aborts the operation and deletes the proposal directory (atomic — the SKILL.md never lands under `_proposed/` if blocked).

Findings are surfaced up to the reviewer agent in the `skill_review` tool's `list` / `get` output so it can reject proactively.

## Reviewer agent

Gateway entry point: `runReviewerAgent(ctx, profile)` → `SkillAgentReport`. Called via `POST /v1/skills/reviewer/run` (synchronous, bounded at 10 turns).

Flow:

1. Short-circuit if `ListProposals()` returns empty — no LLM call, no tokens spent.
2. Render the active-skills inventory + proposal queue (with metadata, guard findings, curator intent) into a single user message.
3. Build the LLM conversation with the embedded `reviewer.md` persona as the system prompt + the queue message as the user turn.
4. Expose **only** the `skill_review` tool (list/get/approve/reject/merge). Other tools are not offered.
5. Stream the response through `runSkillAgentLoop`: collect tool calls, dispatch them, append results, re-ask. Hard cap at 10 turns.
6. Return a `SkillAgentReport` with turn count, tool-call count, final assistant reply, and any error.

### Reviewer approve — intent dispatch

`skill_review(action="approve", proposal_id=...)` does different things depending on the proposal's `Intent`:

| Intent | What approve does |
|---|---|
| `` *(create)* / `refine` / `merge` | `PromoteProposal` — writes SKILL.md under `<category>/<name>/`, snapshots prior version to `_history/` if overwriting, drops the proposal dir. `merge` additionally calls `ApplyCuratorIntent` to archive the loser skills. |
| `archive` | No SKILL.md promotion. `ApplyCuratorIntent` moves the target skill to `_archived/<uuid>/`. Proposal parked under `_rejected/` with reason `applied: archive — <note>`. |
| `recategorize` | No SKILL.md promotion. `ApplyCuratorIntent` renames the active dir from `<old_category>/<name>/` to `<new_category>/<name>/`. Proposal parked similarly. |
| `split` | `ApplyCuratorIntent` reads `split_children/` (pre-written when the proposal was created), materialises each child as a new active skill, then archives the source skill. |

## Curator agent

Gateway entry point: `runCuratorAgent(ctx, profile)` → `SkillAgentReport`. Called via `POST /v1/skills/curator/run`.

Unlike the reviewer (who gates new proposals at the door), the curator **re-arranges the active library** based on usage data. It does not write to active state directly — its actions produce proposals that the reviewer approves on the next cycle.

Tool: `skill_curator` with six actions:

| Action | Effect |
|---|---|
| `list_active_with_usage` | Returns active skills joined with `SkillUsageStats` (total_count, success_rate_pct, last_used_at). Read-only. |
| `propose_refinement` | Writes a proposal with `Intent=refine`, target = one active skill, body = curator-supplied replacement SKILL.md. |
| `propose_merge` | Writes a proposal with `Intent=merge`, targets = 2+ active skills (first is the survivor), body = consolidated SKILL.md. |
| `propose_split` | Writes a proposal with `Intent=split`, target = one source skill, body = an index markdown; each child's SKILL.md goes to `split_children/<idx>_<cat>_<name>.md`. |
| `propose_archive` | Writes a proposal with `Intent=archive` (empty body). |
| `propose_recategorize` | Writes a proposal with `Intent=recategorize`, `IntentNewCategory=<new>`. |

The `curator.md` persona directs heuristics: archive skills with zero usage in 30+ days, refine (don't archive) skills with low success rate on repeated use, merge when two skills overlap heavily, don't touch `builtin`/`trusted` trust tiers.

## Usage tracking

`storage.SkillUsage` table (migrated automatically by `storage.Open`):

```sql
CREATE TABLE skill_usage (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT    NOT NULL REFERENCES sessions(id),
    skill_id     TEXT    NOT NULL,           -- "<category>/<name>"
    message_id   INTEGER,                    -- nullable
    used_at      INTEGER NOT NULL,           -- millis
    outcome      TEXT    DEFAULT 'unknown',  -- success | error | abandoned | unknown
    context_hint TEXT                        -- tool name for now
);
CREATE INDEX skill_usage_skill_idx   ON skill_usage(skill_id);
CREATE INDEX skill_usage_session_idx ON skill_usage(session_id);
CREATE INDEX skill_usage_used_at_idx ON skill_usage(used_at DESC);
```

Rows are inserted by `logSkillToolUsage` in `gateway/chat.go` after every dispatched `skill_view` or `skill_manage` tool call. `skill_id` is extracted from the tool-call JSON args (handling both `skill_view(name="cat/name")` and `skill_manage(action="create", category=, name=)`).

## Chat integration

Two hooks in `gateway/chat.go`:

1. **Skills-inventory injection.** `buildMessagesWithSkills(systemPrompt, skillsInventoryMessage(profile), req.Messages)` prepends a synthesised user message listing every active skill with its description. Placed *before* the conversation history rather than in the system prompt — this preserves the LLM provider's prompt cache across turns (cache boundary is stable; inventory only changes when skills are installed/removed/promoted).
2. **Tool-call logging.** After `dispatchTool` returns, `logSkillToolUsage` inserts a row when the tool name is `skill_view` or `skill_manage` and the call referenced a well-formed skill id. Best-effort — failures are logged but never bubble up.

## Failure modes and non-goals

- **LLM hits turn cap without termination.** Common with smaller models that loop on `list`/`get` without deciding. Not a bug — a model-quality tuning concern. The report captures `turns=10, error="hit 10-turn cap"` so the caller can retry with a stronger model.
- **VirusTotal / SmartScreen / Defender.** Out of scope here — see the Defender section in the root README.
- **No proposal auto-approval.** Even with the "Option 3 auto-promote-on-reuse" mechanism mentioned in the consensus design, current code does not automatically approve. Every promotion requires a reviewer action (tool call or HTTP `approve`).
- **Unpromoted proposals are ignored by the LLM.** `ListInstalled` excludes `_proposed/`, so the chat skills-inventory injection never mentions pending proposals — the main agent only sees active skills, by design.
