# Office (Claw3D)

The Office screen embeds Claw3D / pan-office, an Office-like document editing experience inside Pan-Agent.

## What it is

Pan-office is a separate React/Next.js application from the main Pan-Agent project. It provides:
- Word-style document editing
- Spreadsheet editing
- Presentation editing
- AI-assisted writing

Pan-Agent runs pan-office as a managed subprocess and embeds it in the Office screen via an iframe.

## First-time setup

The first time you visit the Office screen:

1. Pan-Agent checks if `<AgentHome>/pan-office/` exists.
2. If not, it offers to clone the upstream pan-office repository.
3. Click "Setup". This runs `git clone` and `npm install` (streams progress as newline-delimited JSON).
4. Once setup completes, click Start.

The setup takes a few minutes (npm install of Next.js and dependencies). Live progress is streamed to the UI.

## Start / stop

After setup:

- **Start** → spawns two processes: the Next.js dev server (`npm run dev`) and the adapter script (`npm run hermes-adapter`, legacy name from the predecessor).
- **Stop** → kills both processes.
- **Status** → shows whether each process is running.

## Requirements

- **Node.js 22+** must be installed and in PATH.
- **Git** for the initial clone.
- ~500 MB disk space for the cloned repo + node_modules.

If Node isn't found, the setup fails with a clear error.

## Configuration

The pan-office adapter reads from `<AgentHome>/pan-office/.env`. Currently the adapter listens on a port that defaults from the upstream config. Check the Office screen's status panel for the actual URL.

## Why "Claw3D"?

Internal codename. The product is branded as pan-office externally. The agent and storybook still use the older Claw3D name in some places — gradual rename in progress.

## Operator rule
Pan-office is a separate project with its own release cycle. Pan-Agent integrates it as a subprocess but doesn't bundle its source. Updating pan-office requires a manual `git pull` inside `<AgentHome>/pan-office/`.

## Read next
- [[02 - Tools Catalog]]
- [[04 - Configuration Reference]]
