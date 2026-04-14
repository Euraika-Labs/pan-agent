# Security Model

This note describes Pan-Agent's threat model, what it protects against, and what it explicitly does not.

## Threat model

Pan-Agent is a **single-user desktop application**. It runs on the user's own machine. There is no multi-tenant deployment. There is no network exposure beyond messaging gateway bots (which are themselves outbound-only WebSocket / long polling connections).

### In scope

- An LLM that the user converses with is given access to dangerous tools. The system must prevent silent execution of destructive operations.
- The user runs untrusted skills from a public skill registry. The system must not let a malicious skill escalate beyond its declared capabilities.
- Path traversal in user-supplied profile names must not let an attacker delete files outside `<AgentHome>/profiles/`.
- The Tauri webview must not be able to make arbitrary network calls (CSP).
- Auto-update artifacts must be cryptographically verified before installation.

### Out of scope

- An attacker with arbitrary local code execution on the user's machine. They can read `.env` directly, kill the agent, and install their own. The localhost API is not the attack surface.
- Multi-user isolation. Pan-Agent assumes one user per OS user account.
- Defending against the LLM provider. If OpenAI/Anthropic/etc. wanted to exfiltrate user prompts, they could.
- DDoS against the local API. The server has no rate limiting beyond what the OS scheduler provides.

## Protections in place

### CORS allowlist

`internal/gateway/middleware.go` only sets `Access-Control-Allow-Origin` for these origins:
- `http://localhost:5173` (Vite dev server)
- `tauri://localhost` (production Tauri webview)

This prevents random websites from calling the API even if the user navigates to a malicious page.

### Localhost binding

`http.Server` binds to `127.0.0.1:8642` only. There is no `0.0.0.0` mode. Users who want remote access must set up their own SSH tunnel or VPN.

### CSP

The Tauri webview enforces:
```
default-src 'self';
connect-src 'self' http://localhost:8642;
script-src 'self'
```

`script-src 'self'` blocks inline scripts and external scripts. `connect-src` restricts fetches to localhost. This protects against XSS in LLM responses (which are rendered as markdown).

### Approval system

103 regex patterns classify shell commands as Safe, Dangerous, or Catastrophic. Catastrophic commands are blocked by default. Dangerous commands trigger an interactive approval modal. See [[05 - Approval System]].

### Profile name validation

Both `CreateProfile` and `DeleteProfile` validate names against `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`. Without this, `DELETE /v1/config/profiles/../etc` would call `os.RemoveAll(<AgentHome>/profiles/../etc)` which `filepath.Clean` resolves to `<AgentHome>/etc` — outside the profiles directory.

### Update signing

The Tauri updater verifies Ed25519 signatures on update artifacts before installation. The public key is baked into the installed binary. The private key lives in GitHub Actions secrets and never touches the repo.

### File permissions

| File | Mode |
|---|---|
| `.env`, `auth.json` | `0600` (Unix) / restricted ACL (Windows) |
| Directories | `0700` |

## What is NOT protected

### No API authentication

Any local process can call `localhost:8642`. There is no API key check. Browser extensions, malware, or other apps on the user's machine can hit any endpoint.

This is a deliberate design choice. Adding an auth token would not stop a local attacker (they could read the token from anywhere the agent stores it). It would only add friction for legitimate use (CLI scripts, the Tauri app, future integrations).

### Plaintext API keys

`.env` stores keys in plain text with `0600` permissions. There is no encryption at rest. Disk encryption is the user's responsibility.

### Bot auto-approval

Telegram/Discord/Slack bots auto-approve all tool calls. There is no SSE stream attached to a chat platform message for an interactive approval modal. Use `TELEGRAM_ALLOWED_USERS` to restrict who can talk to your bot. Discord and Slack rely on bot channel permissions.

### LLM prompt injection

The agent runs whatever the LLM tells it to (subject to the approval system). A maliciously-crafted user message or memory entry could potentially convince the LLM to run unexpected commands. The approval system catches catastrophic commands but not subtle exfiltration via web_search or browser tools.

This is a fundamental limitation of agentic AI systems and is not Pan-Agent specific.

## Reporting vulnerabilities

Email: `bert@euraika.net`

Please include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

You will receive an acknowledgment within 48 hours. Critical issues get a fix or mitigation within 7 days.

See [SECURITY.md](https://github.com/Euraika-Labs/pan-agent/blob/main/SECURITY.md) in the repo for the canonical disclosure policy.

## Operator rule
Treat the localhost binding as the security boundary. Do not bind to other interfaces. Do not put pan-agent behind a reverse proxy. Do not "harden" it by adding API key auth and calling it secure — the localhost binding is what makes it safe.

## Read next
- [[05 - Approval System]]
- [[03 - Auto-Update System]]
