# Skills

Skills are installable capability bundles that extend the agent's system prompt with specialized instructions.

## What a skill looks like

A skill is a directory containing a `SKILL.md` file with frontmatter and instructions:

```markdown
---
name: code-reviewer
description: Performs thorough code reviews focused on bugs, security, and convention violations.
---

# Code Reviewer

When the user asks for a code review:

1. Read the target file completely before commenting.
2. Focus on high-confidence issues only — bugs, security, convention violations.
3. Skip style preferences unless explicitly requested.
4. Group findings by severity: BUG > SECURITY > MAINTAINABILITY.
5. Include code references with line numbers.
```

The frontmatter defines the skill's identity. The body is appended to the agent's system prompt when the skill is active.

## Skill categories

Skills are organized by category. Common categories:
- `coding/` — code review, refactoring, testing
- `writing/` — drafting, editing, summarization
- `research/` — multi-source synthesis, fact-checking
- `analysis/` — data exploration, market analysis

The category is the first directory level under `skills/`.

## Filesystem layout

```
<AgentHome>/skills/                    ← installed skills (per-profile)
├── coding/
│   ├── code-reviewer/
│   │   └── SKILL.md
│   └── refactor-helper/
│       └── SKILL.md
└── writing/
    └── tech-blogger/
        └── SKILL.md

<install-dir>/skills/                  ← bundled skills (ship with binary)
├── coding/
│   └── ...
```

`<install-dir>` is the directory containing the `pan-agent` executable.

## The Skills screen

Lists installed + bundled skills with:
- Name + description
- Source (Installed / Bundled)
- Install / Uninstall button

## Install a skill

The skill registry concept is forward-looking — there's no central skill repo yet. To install a skill, manually drop a directory into `<AgentHome>/skills/<category>/<name>/` containing `SKILL.md`.

A future release will add `POST /v1/skills/install` that fetches from a registry URL.

## Uninstall

Skills screen → Uninstall button removes the directory from `<AgentHome>/skills/`. Bundled skills cannot be uninstalled (they live next to the binary).

## How skills are used

When the agent starts a chat, the backend:

1. Reads `SOUL.md` (persona).
2. Lists all installed + bundled skills in the active profile.
3. Concatenates skill contents into the system prompt.

The agent doesn't choose to "use" a skill — all installed skills are always active. Skills act as additional guidance baked into the persona.

## Skills vs persona

| Persona (SOUL.md) | Skills (SKILL.md per dir) |
|---|---|
| One per profile | Many per profile |
| Always active | Always active when installed |
| Edit in UI | Manage as files |
| Identity, tone, base behavior | Specialized capabilities |

If a skill grows into the agent's identity, move its contents to the persona and uninstall the skill.

## Per-profile skills

Each profile has its own `skills/` directory. Skills installed in one profile don't appear in another.

Bundled skills appear in all profiles.

## Operator rule
Be careful with skills that override default agent behavior — installing two skills with conflicting instructions ("be terse" vs "be thorough") leads to unpredictable agent behavior.

## Read next
- [[02 - Tools Catalog]]
- [[05 - Memory and Persona]]
