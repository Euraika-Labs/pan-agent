# Top 10 Things Every User Should Know

This note is the cheat sheet. If you only read one note, read this one.

## 1. The HTTP server is localhost-only and unauthenticated

Pan-Agent binds to `127.0.0.1:8642`. Any local process can call it. There is no API key check. This is intentional for a single-user desktop app. Do not bind to `0.0.0.0` or expose the port externally.

## 2. The desktop app is just a client

The Tauri webview is one of several possible clients of the Go HTTP API. You can run `pan-agent serve` headless on a server, or call the API from `curl`, or build your own UI. The agent works exactly the same way.

## 3. Profiles are isolated agent environments

Each profile has its own `.env`, `config.yaml`, `MEMORY.md`, `SOUL.md`, and skills. Switching profiles changes which configuration the agent uses. The "default" profile lives at `<AgentHome>/`. Named profiles live at `<AgentHome>/profiles/<name>/`.

## 4. Dangerous tools require approval

Tools that can modify your system (terminal, filesystem, code_execution, browser) trigger an approval modal in the UI. You can approve or deny each tool call. The approval system uses 103 regex patterns to also classify commands as Catastrophic and block them by default.

## 5. PC control tools are platform-specific but cross-platform

Keyboard, mouse, and window_manager work on Windows, macOS, and Linux — but they use different OS APIs underneath. If you run on FreeBSD, the tools are invisible to the LLM (they don't register).

## 6. Linux PC control needs X11

The Linux implementations use X11 XTest and EWMH. They work under XWayland (GNOME/KDE default). They do not work under pure Wayland without XWayland.

## 7. macOS window manipulation needs Accessibility permission

Listing windows works without permission. Move, resize, and close use AppleScript and require granting Accessibility permission to Pan Desktop in System Preferences → Privacy & Security → Accessibility.

## 8. Bot conversations bypass the approval system

When you chat with the agent via Telegram/Discord/Slack, all tool calls are auto-approved. There is no interactive approval UI on a chat platform. Use `TELEGRAM_ALLOWED_USERS` to restrict who can talk to your bot.

## 9. Setup wizard is state-driven, not flag-driven

The wizard appears when no LLM provider API key is found AND no custom base_url is configured. There is no "setup complete" flag. Edit `.env` to add a key and the wizard goes away. Delete all keys to bring it back.

## 10. Auto-update is signed and uses GitHub Releases

The desktop app checks `https://github.com/Euraika-Labs/pan-agent/releases/latest/download/latest.json` for updates. Updates are signed with Ed25519 and verified before install. The signing private key lives in GitHub Actions secrets, never in the repo.

## Bonus: things that look like bugs but are not

- The `agentVersion` field in `GET /v1/config` is always `null`. This is a leftover from the pan-desktop era when the agent was a separate Python process. Pan-Agent IS the agent — there is no separate version to report.
- The Office (Claw3D) screen requires Node.js installed on your machine. The first time you visit Office, it clones a separate repo and runs `npm install`.
- The `pan-agent` CLI process and the desktop app's sidecar process are independent. If you want both, you'll have two binaries running, both fighting for `:8642`.

## Read next
- [[01 - Service Architecture]]
- [[01 - Chat]]
- [[05 - Security Model]]
