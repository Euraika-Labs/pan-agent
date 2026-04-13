# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately:

**Email:** bert@euraika.net

Please include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

You will receive an acknowledgment within 48 hours. We aim to provide a fix or mitigation within 7 days for critical issues.

## Security Model

Pan-Agent runs a local HTTP API server on `127.0.0.1:8642`. Key security considerations:

- **Localhost-only binding:** The API server does not accept connections from external networks.
- **CORS:** Only `http://localhost:5173` (Vite dev) and `tauri://localhost` (production app) are allowed origins.
- **API keys:** Stored in plaintext `.env` files with `0600` permissions. No encryption at rest.
- **Approval system:** 103 regex patterns classify commands as Safe, Dangerous, or Catastrophic before execution.
- **No authentication:** The API has no auth tokens. Any local process can call it. This is by design for a single-user desktop app.

## Scope

The following are in scope for security reports:
- Path traversal in profile names or file operations
- Command injection via tool execution
- API key exposure beyond localhost
- Approval system bypasses
- XSS in the Tauri webview

The following are out of scope:
- Local privilege escalation (if an attacker has local code execution, they can read `.env` directly)
- Denial of service against the local API
