# Regolo Model Compatibility Matrix

Empirical results from firing the curator agent (`POST /v1/skills/curator/run`) against every Regolo model candidate. Same fixed inventory (5 active skills: `coding/refactor-go`, `coding/refactor-rust`, `writing/tldr`, `junk/unused-thing`, `research/big-skill`) with zero usage data. Curator loop cap: 10 turns.

Captured 2026-04-14 against Regolo production endpoints, after the gpt-oss tool-call parser fix (commit `d9d1856`).

## Summary

| Rank | Model | Turns | Tool Calls | Proposals | Intent variety | Verdict |
|---|---|---|---|---|---|---|
| 1 | `qwen3.5-122b` | 3 | 6 | 5 | **4 intents** (refine, merge, recategorize, archive) | Best variety; strongest structured output |
| 2 | `minimax-m2.5` | 6 | 5 | 4 | 3 intents (refine, recategorize, archive) | Strong — heavy on refine |
| 3 | `qwen3-coder-next` | 5 | 4 | 3 | 3 intents (merge, recategorize, archive) | Efficient, 3-intent coverage |
| 4 | `mistral-small3.2` | 5 | 5 | 4 | 2 intents (merge, archive; final says `recategorize` too) | Good |
| 5 | `gpt-oss-120b` | 6 | 5 | 4 | 2 intents (merge, archive) | Fixed by parser patch; now working |
| 6 | `mistral-small-4-119b` | 3 | 6 | 5 | 2 intents (refine, archive) | Clean terminations |
| 7 | `Llama-3.3-70B-Instruct` | 3 | 6 | 5 | 1 intent (archive) | Works, low variety |
| 8 | `qwen3.5-9b` | 3 | 6 | 5 | 1 intent (archive) | Works — small but competent |
| 9 | `apertus-70b` | 3 | 7 | 5 | 1 intent (archive) | Works |
| 10 | `gemma4-31b` | 4 | 7 | 5 | 1 intent (archive) | Works |
| 11 | `gpt-oss-20b` | 10 | 10 | 6 | 1 intent (archive, duplicates) | Hits turn cap; makes duplicate proposals |

## Not usable

| Model | Why |
|---|---|
| `claude-3-5-sonnet-20241022` | **Not served by Regolo.** Seeded in `defaultModels` (internal/models/models.go) with `baseURL: ""` so users can point it at Anthropic's API directly with their own key. Only appeared in this matrix because `/v1/models/sync` returns the union of Regolo's catalog and the seed list. |
| `gpt-4o` | Same — seeded for BYO OpenAI keys, not served by Regolo. |
| `kimi-k2-0905` | **Stale seed.** Was hardcoded as a Regolo model but Regolo's catalog contains no `kimi-*` entry (18 models total, all Qwen/Llama/Mistral/gpt-oss/apertus/minimax/gemma family). Removed from `defaultModels` in commit following this matrix test. |
| `Llama-3.1-8B-Instruct` | **Emits tool calls as plain text content** instead of structured `tool_calls` deltas — returns `{"name":"skill_curator","arguments":{"action":"list_active_with_usage"}}` in the assistant's message body. Fundamental tool-use incapability at the 8B parameter count; not fixable on our side. |

## Hardcoded seed list

`internal/models/models.go` ships three `defaultModels` seeded into `models.json` on first run. After this matrix test, the list is:

- **Claude 3.5 Sonnet** (`provider: anthropic`, `baseURL: ""`) — BYO Anthropic key
- **GPT-4o** (`provider: openai`, `baseURL: ""`) — BYO OpenAI key
- **Qwen3.5-122B (Regolo)** (`provider: regolo`, Regolo URL) — strongest Regolo tool-use model
- **gpt-oss-120b (Regolo)** (`provider: regolo`, Regolo URL) — fast + free Regolo tool-use model

Previously the list also contained **Kimi K2** tagged as Regolo, which was a stale entry (Regolo has never served kimi-k2-0905). Removed.

## Observations

- **All curators defaulted heavily to `archive`.** The planted skills have zero usage data, and the curator persona's archival heuristic ("zero usage in 30+ days") reads that as "archive everything". In production, with real usage data, intent distribution should look very different. Not a model-quality issue — a test-data issue.
- **Larger Qwen is genuinely better.** `qwen3.5-122b` produced the most varied intents (4 distinct kinds) with 5 proposals in 3 turns. Strongest combination of efficiency and decision variety observed.
- **`gpt-oss-20b` hits the turn cap.** It repeatedly re-proposes the same archive intents because smaller models struggle to track "I already did this" across turns. Usable but inefficient — prefer 120b where possible.
- **Mistral "3.2" and "4-119b" both work cleanly.** 119b is roughly on par with Llama-3.3-70B on this task.
- **Models not on the plan are cleanly diagnosable.** Our 400-status error path surfaces the "invalid model" message without mangling — no code change needed there.

## Recommended defaults

- **Production (best quality):** `qwen3.5-122b` or `minimax-m2.5` — highest intent variety + clean terminations.
- **Low latency / free tier:** `gpt-oss-120b` — fast, 6 turns, covers merge + archive. Requires the parser fix shipped in `d9d1856`.
- **Capacity fallback:** `Llama-3.3-70B-Instruct` — simple, predictable, low-variety but reliable.
- **Avoid for tool-use:** `Llama-3.1-8B-Instruct` (text-only tool "calls"), `gpt-oss-20b` (turn cap + duplicates).

## Reproducing

```bash
# Plant the fodder
./build/pan-agent.exe serve --port 8642 &
# (plant 5 active skills via SKILL.md writes — see build/matrix.sh)

# Fire the matrix
bash build/matrix.sh   # 15 models, ~5 min of wall time
cat build/matrix_report.md
```

Each run consumes ~3k input + ~800 output tokens per model → roughly $0.10 total against a gpt-4o-class budget; well under $0.01 on Regolo's open-weight models.
