# Tauri Desktop Frontend

The desktop app is a Tauri v2 shell wrapping a React 19 + Vite 7 frontend.

## Quick reference

| Item | Value |
|---|---|
| Tauri version | v2 |
| React | 19.2 |
| Vite | 7.2 |
| TypeScript | 6.0.3 (bumped via Dependabot PR #18; `baseUrl` dropped from `tsconfig.json` — TS 6.0 makes it a hard error and path mappings anchor on the tsconfig dir under `moduleResolution: "bundler"`) |
| Tailwind CSS | 4.2 |
| Bundle targets | NSIS, MSI, DMG, DEB, AppImage |

## Layout

```
desktop/
├── src/                          ← React frontend
│   ├── App.tsx                   ← entry component
│   ├── api.ts                    ← fetchJSON + streamSSE helpers
│   ├── constants.ts              ← provider list, settings sections, gateway platforms
│   ├── main.tsx                  ← React mount + theme bootstrap
│   ├── assets/
│   │   ├── icon.png
│   │   └── main.css              ← navy blue theme
│   └── screens/                  ← 15 screens
│       ├── Layout/Layout.tsx     ← sidebar + view router + first-run detection
│       ├── Setup/Setup.tsx       ← onboarding wizard
│       ├── Chat/Chat.tsx
│       ├── Sessions/, Profiles/, Settings/, Models/,
│       ├── Memory/, Soul/, Skills/, Tools/, Schedules/,
│       ├── Gateway/, Search/, Office/
└── src-tauri/                    ← Tauri/Rust shell
    ├── Cargo.toml                ← Rust deps (tauri, plugin-shell, plugin-updater)
    ├── src/main.rs               ← plugin registration
    ├── tauri.conf.json           ← bundle config, updater endpoint, CSP
    ├── capabilities/default.json ← ACL permissions
    ├── icons/                    ← platform icons
    └── binaries/                 ← Go sidecar (built by CI, target-triple suffix)
```

## Screens

| Screen | Purpose |
|---|---|
| Layout | Sidebar navigation + first-run detection. Routes to other screens. |
| Setup | Onboarding wizard (6 provider cards, API key entry, local presets). |
| Chat | Streaming chat with the agent. Tool call + approval modal handling. |
| Sessions | List past sessions, resume any of them. |
| Profiles | Create/switch/delete profiles. |
| Models | View model library, sync from provider, set active. |
| Memory | View/edit MEMORY.md entries. |
| Soul | View/edit SOUL.md persona. Reset to default. |
| Skills | List installed + bundled skills. Install/uninstall. |
| Tools | Toggle tools on/off. |
| Schedules | Create cron jobs. |
| Gateway | Toggle Telegram/Discord/Slack platforms. Enter tokens. Start/stop bots. |
| Settings | LLM API keys, model config, credential pool, theme, version info. |
| Search | Full-text search across past sessions (FTS5). |
| Office | Claw3D / pan-office integration. |

## API client

`desktop/src/api.ts` provides two helpers:

- **`fetchJSON<T>(path, options?): Promise<T>`** — standard JSON fetch against `VITE_API_BASE` (defaults to `http://localhost:8642`). Throws on non-2xx with the response body text.
- **`streamSSE(path, body, onEvent): () => void`** — opens an SSE POST stream. Returns a stop function. Each `data: <json>` line is parsed and passed to `onEvent`.

No third-party HTTP library. No state management library beyond React's own `useState` / `useEffect`.

## First-run detection

`Layout.tsx` runs a `useEffect` on mount:

1. Calls `GET /v1/config`.
2. Checks if any of `OPENROUTER_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `REGOLO_API_KEY` are set in `env`.
3. If none, AND `model.baseUrl` is empty or `model.provider !== "custom"`, sets `setupRequired = true`.
4. With exponential backoff retry (up to 5 attempts) for the backend startup race.

If `setupRequired === true`, render `<Setup onComplete={...} />` instead of the normal layout.

## Sidecar process

Tauri's `externalBin: ["binaries/pan-agent"]` config tells Tauri to bundle the Go binary in the installer. At runtime, `tauri-plugin-shell` spawns the binary as a child process.

The binary name must include the target triple:
- `pan-agent-x86_64-pc-windows-msvc.exe`
- `pan-agent-aarch64-apple-darwin`
- `pan-agent-x86_64-unknown-linux-gnu`

CI builds the Go binary with the right name before invoking `tauri build`.

## CSP

`tauri.conf.json` `app.security.csp`:

```
default-src 'self';
connect-src 'self' http://localhost:8642;
style-src 'self' 'unsafe-inline';
img-src 'self' data:;
script-src 'self'
```

This restricts fetch to localhost:8642 and the app's own origin. Inline styles are allowed (Tailwind needs them); inline scripts are not.

## Capabilities

`capabilities/default.json` grants the frontend access to:
- `core:default` (windows, events)
- `shell:allow-spawn`, `shell:allow-execute`, `shell:allow-open` (sidecar + external links)
- `updater:default` (check + download + install updates)

Without these, plugin calls from the frontend are silently denied at runtime.

## Read next
- [[03 - LLM Client and Providers]]
- [[03 - Auto-Update System]]
- [[02 - Build and Release Pipeline]]
