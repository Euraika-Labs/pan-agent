# pan-agent 0.4.0 Runbook

Operator playbook for the pan-agent desktop/headless deployment. Covers
startup, shutdown, health checks, rollback, and the known manual test
procedures that can't be automated (WebView2 fallback, CSP dogfood).

---

## 1. Starting the agent

### Desktop app (Tauri shell)

Launch `Pan Desktop` from the Start Menu / Applications. The Tauri
frontend on `http://localhost:5173` (dev) or `tauri://localhost` (prod)
drives the Go gateway via `http://localhost:8642`.

### Headless CLI

```sh
pan-agent serve --port 8642 --host 127.0.0.1
```

The default subcommand is `serve`, so `pan-agent` alone also works.
A PID file is written to `AgentHome/pan-agent.pid` after successful
bind; operators can read it to signal the running instance.

### Platform-specific AgentHome paths

| OS | AgentHome |
|---|---|
| Windows | `%LOCALAPPDATA%\pan-agent\` |
| macOS | `~/Library/Application Support/pan-agent/` |
| Linux | `~/.local/share/pan-agent/` |

## 2. Shutdown

The gateway handles `SIGINT` + `SIGTERM` with a 10-second graceful
shutdown:

```sh
# Unix
kill $(cat $AGENT_HOME/pan-agent.pid)

# Windows
taskkill /PID $(cat "$LOCALAPPDATA/pan-agent/pan-agent.pid") /F
```

Tauri-launched sessions use the `parentwatch` module to self-terminate
when the parent Tauri process dies.

## 3. Health checks

```sh
pan-agent doctor
```

Runs 7 checks: AgentHome exists, profile .env readable, API key
present, SQLite opens, config file present, PID file status, CSP
violations log summary.

Machine-readable output:

```sh
pan-agent doctor --json
```

## 4. CSP violations log

The gateway writes `/v1/office/csp-report` POST bodies to
`AgentHome/csp-violations.log` (hard-capped at 10 MB). Dump the tail:

```sh
pan-agent doctor --csp-violations
```

During a 0.4.0 dogfood window, this log should be **empty** at the end
of 5 business days. Any entry is a signal that the CSP policy in
`desktop/src-tauri/tauri.conf.json` + `desktop/index.html` blocked a
legitimate request and needs adjustment.

## 5. Engine toggle (runtime swap)

Switch between the embedded Go adapter and the legacy Node sidecar:

```sh
pan-agent doctor --switch-engine=go     # embedded (recommended)
pan-agent doctor --switch-engine=node   # legacy sidecar fallback
```

The doctor POSTs to `/v1/office/engine`. If the gateway isn't running,
it writes `office.engine` to `config.yaml` directly and the next
launch picks it up. Engine swaps drain in-flight /office/* requests
with a 10-second budget before the teardown.

## 6. Rollback from 0.4.0 → 0.3.x behavior

If 0.4.0 breaks and you need to revert to the legacy Node sidecar
without downgrading the binary:

```yaml
# ~/.config/pan-agent/default/config.yaml
office:
  engine: node
```

Restart pan-agent. The gateway will proxy /office/* to the Node
sidecar on the configured `node_port` (default 3000). Existing
Claw3D installations at `~/.hermes/clawd3d-history.json` remain
untouched; the migration importer only runs when you explicitly
invoke it.

Full binary downgrade:

```sh
pan-agent --version  # confirm current version
# Uninstall via OS package manager, reinstall 0.3.x from GitHub Releases
```

## 7. Migration importer

One-shot import of `~/.hermes/clawd3d-history.json` into pan-agent's
SQLite:

```sh
pan-agent migrate-office --dry-run    # preview row counts
pan-agent migrate-office              # commit; moves source to backup
pan-agent migrate-office --force      # re-import (duplicates possible)
```

Exit codes: 0=ok, 1=source missing (not an error), 2=parse error,
3=DB error. Idempotent: re-running against the same file (same mtime)
is a free skip.

## 8. WebView2 manual test (Windows only)

Automated Playwright tests run on GitHub Chromium; they do NOT exercise
WebView2. Manual procedure before each release:

1. Install pan-agent on a Windows 10/11 machine with hardware
   acceleration available.
2. Launch Pan Desktop. Navigate to Office tab. Confirm Claw3D iframe
   renders the 3D scene (not a black rectangle).
3. On a VM or a machine with GPU acceleration blocked (Group Policy
   `DisableHWAcceleration=1` or similar): launch Pan Desktop and
   confirm the WebGL2 fallback flow triggers:
   - Splash screen reads "3D Rendering Unavailable"
   - System browser opens to `http://localhost:8642/office/`
   - Pan Desktop window shows `FallbackBanner` with "Open in browser"
     and "Try again" actions
   - `config.yaml` now has `office.browser_fallback_until` set to
     ~7 days in the future

Re-run the check 8 days later; the probe should re-fire and the user
gets another chance to confirm.

## 9. Rate-limit lockout recovery

If `pan-agent doctor` reports `auth.failure` audit rows in the state
DB, and a client is seeing 429 responses, the sessionStore rate
limiter (M5-C1) is doing its job. To unblock a specific IP during
development:

```sh
# Restart the gateway — sessionStore is in-memory, lockouts clear
kill $(cat $AGENT_HOME/pan-agent.pid)
pan-agent serve --port 8642
```

Lockouts reset naturally after 30 seconds without manual intervention.
The restart trick is only for the impatient.

## 10. Vendor-sync (automated)

`.github/workflows/vendor-sync.yml` runs every Monday at 06:00 UTC. It
checks for upstream Claw3D drift, rebases the patch set, rebuilds the
embedded bundle, and opens a draft PR. Human review is required before
merge — see `.github/PULL_REQUEST_TEMPLATE/vendor_sync.md`.

If the workflow fails, the job uploads logs as an artifact. Common
failure modes:

| Symptom | Fix |
|---|---|
| Patch fails to apply | Upstream diff introduced a conflict; manually rebase in a clone, `git format-patch` the result, commit to `internal/claw3d/vendor/patches/` |
| `go test ./...` fails after bundle rebuild | Contract drift; check the `hello-ok` payload against `docs/protocol.md` |
| Bundle size delta > 10% | Investigate the upstream changelog; may be intentional feature addition |

## 11. Known limitations for 0.4.0

- **Windows code-signing is deferred.** Installers ship unsigned.
  Users see SmartScreen warnings; document says "click More info →
  Run anyway". Cert acquisition is a 0.5.0 item.
- **macOS installers are notarized** via the Tauri build action's
  existing cert. Gatekeeper passes cleanly.
- **Linux `.deb` + `.AppImage`** are unsigned. GPG detach-signing is
  a 0.5.0 item.
- **tauri-driver E2E matrix** covers Windows + Linux only. macOS
  WKWebView has no upstream WebDriver; M5 Phase 1 dropped it pending
  `danielraffel/tauri-webdriver` maturity.
