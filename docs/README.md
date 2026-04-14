# Pan-Agent Documentation

This directory contains the comprehensive Pan-Agent manual.

## Reading the manual

Start at [`manual/00 - Table of Contents.md`](./manual/00%20-%20Table%20of%20Contents.md) for the canonical reading order, or [`manual/Part I - Foundations/00 - Start Here/00 - Pan-Agent Home.md`](./manual/Part%20I%20-%20Foundations/00%20-%20Start%20Here/00%20-%20Pan-Agent%20Home.md) for the entry hub.

## Manual structure

```
manual/
├── 00 - Table of Contents.md
├── 00 - Changelog.md
├── 00 - HTTP API Reference.md
├── Part I - Foundations/
│   ├── 00 - Start Here/         (Home, Quick Start, Reading Guide)
│   ├── 01 - Platform Overview/   (System Overview, How It Fits, Top 10)
│   └── 02 - Architecture/        (Service, HTTP API, Cross-Platform Tools, Storage)
├── Part II - Components/
│   ├── 01 - Go Backend
│   ├── 02 - Tauri Desktop Frontend
│   ├── 03 - LLM Client and Providers
│   ├── 04 - Tool Registry
│   ├── 05 - Approval System
│   ├── 06 - Storage Layer
│   ├── 07 - Profile System
│   └── 08 - Messaging Gateway Bots
├── Part III - Operations/
│   ├── 00 - Troubleshooting Index
│   ├── 01 - Installation and First Run
│   ├── 02 - Build and Release Pipeline
│   ├── 03 - Auto-Update System
│   ├── 04 - Configuration Reference
│   ├── 05 - Security Model
│   └── 0X - Issues runbooks (Setup, Gateway, PC Control, Build/CI)
└── Part IV - User Guide/
    ├── 01 - Chat
    ├── 02 - Tools Catalog
    ├── 03 - Profiles
    ├── 04 - Models and Providers
    ├── 05 - Memory and Persona
    ├── 06 - Skills
    ├── 07 - Schedules
    └── 08 - Office Claw3D
```

39 markdown documents organized in the same style as the Hermes V3 Prod manual in the Euraika Obsidian vault.

## Conventions

- **Wikilinks** like `[[01 - System Overview]]` work in Obsidian; on GitHub they render as plain text.
- **Mermaid diagrams** are used throughout for architecture and sequence diagrams.
- **Numbered prefixes** (`00`, `01`, `02`...) define reading order within each folder.
- **"Operator rule"** callouts in bold highlight load-bearing facts.
- **"Read next"** sections at the bottom of each note suggest the next document to read.

## Sync with Obsidian

This manual is also maintained in the Euraika Obsidian vault at `Euraika/Pan-Agent/`. The two locations are kept in sync — when the manual is updated, both should be updated.

For the best reading experience, open in Obsidian (wikilinks, graph view, search work natively).
