# Reading Guide

Use this note to choose the shortest reading path for your role.

## Roles

### End user (just wants to use the desktop app)
1. [[01 - Quick Start]]
2. [[01 - Chat]]
3. [[03 - Profiles]] (when you want to separate work and personal contexts)
4. [[02 - Tools Catalog]] (to know what the agent can do)
5. [[08 - Messaging Gateway Bots]] (if you want to chat with your agent from Telegram)

### Operator (deploys + maintains for others)
1. [[01 - Quick Start]]
2. [[01 - Installation and First Run]]
3. [[04 - Configuration Reference]]
4. [[05 - Security Model]]
5. [[02 - Build and Release Pipeline]]
6. [[03 - Auto-Update System]]
7. [[00 - Troubleshooting Index]]

### Developer (contributing code)
1. [[01 - System Overview]]
2. [[02 - How It All Fits Together]]
3. [[01 - Service Architecture]]
4. [[01 - Go Backend]] + [[02 - Tauri Desktop Frontend]]
5. [[04 - Tool Registry]] (to understand how to add new tools)
6. [[03 - Cross-Platform Tool Architecture]] (to understand the build-tag pattern)
7. [[02 - Build and Release Pipeline]]

### AI agent author (using Pan-Agent's HTTP API from another tool)
1. [[01 - Quick Start]]
2. [[02 - HTTP API Surface]]
3. [[00 - HTTP API Reference]]
4. [[05 - Approval System]] (so you understand when SSE blocks on user input)

## Conventions used in this manual

- **Wikilinks** like `[[01 - System Overview]]` jump to other notes in this vault.
- **Mermaid diagrams** are used for system context, mindmaps, sequences, and architectural decisions.
- **Tables** are used for reference data: ports, paths, env vars, endpoints, file formats.
- **Code blocks** with explicit language tags (`bash`, `go`, `tsx`, `yaml`, `json`).
- **Operator rules** (single sentence in bold) call out load-bearing facts that surprise people.

## How to navigate this vault

- The [[00 - Table of Contents]] is the canonical reading order.
- The [[00 - Pan-Agent Home]] is the entry hub with a mindmap.
- Each Part folder has its own intro note (Home for Part I).
- Runbooks in Part III start with `[[00 - Troubleshooting Index]]`.

## When this manual is wrong

The source of truth is the code at `C:\src\pan-agent` and the GitHub repo `github.com/Euraika-Labs/pan-agent`. If a doc disagrees with the code, the code wins — file an issue or fix the doc.

## Read next
- [[01 - System Overview]]
- [[01 - Quick Start]]
