# Phase 12 — Research Unknowns

**Produced by**: Stage-1 research agent, 2026-04-18
**Scope**: Validation of library/API assumptions, cross-platform behavioural risks, and prior-art links for all five Phase 12 workstreams.
**Status**: DO NOT COMMIT — pending architect review.

---

## WS1 — Browser Persistence + Per-Task Cost Budget

### (a) Library / API verification

**`go-rod` Launcher.UserDataDir()**

Verified from source (`launcher.go` lines 398-408). Implementation is:

```go
func (l *Launcher) UserDataDir(dir string) *Launcher {
    if dir == "" { l.Delete(flags.UserDataDir) } else { l.Set(flags.UserDataDir, dir) }
    return l
}
```

It simply sets the `--user-data-dir` Chrome flag; rod does not manage the directory lifecycle itself. Session persistence (cookies, localStorage, IndexedDB) **does survive Chromium restarts** when the same path is supplied on each launch — this is the intended design and is confirmed by the rod documentation: "To reuse sessions, such as cookies, set the Launcher.UserDataDir to the same location."

**Critical caveat**: rod's default launcher sets `--password-store=basic`, which corrupts profile-level cookie encryption when a persistent UserDataDir is used (Issue #177). The symptom is that cookies for password-protected sites (GitHub, Google) silently expire after restart. The fix is to delete this flag before launching: `launcher.New().UserDataDir(...).Delete("password-store").Launch()`. The spec does not mention this interaction. If left unaddressed, WS1 persistent auth will work only for sites that do not use Chromium's encrypted cookie store.

**`Launcher.Bin()`**

From source: `func (l *Launcher) Bin(path string) *Launcher { return l.Set(flags.Bin, path) }`. When a non-empty path is given, auto-download of Chromium is disabled. Pinning a Chromium build (Decision D1) therefore requires calling `Bin()` with an explicit binary path. There is no version enforcement in rod itself — the caller is responsible for keeping the pinned binary fresh.

**`SingletonLock` helper**

No rod-level helper exists for `SingletonLock`. Rod uses `atomic.CompareAndSwapInt32` to prevent the same `Launcher` instance being launched twice (returns `ErrAlreadyLaunched`), but this is an in-process guard only. It does not detect or remove the filesystem-level `SingletonLock` file that Chromium writes in the user data directory. If pan-agent crashes and leaves a stale `SingletonLock`, the next Chromium launch will refuse to start with the same profile directory. **The spec assumes "Monitor SingletonLock to refuse concurrent profile hijacking" but rod provides no API for this.** The implementation must handle this manually: either delete the stale lock on startup (common approach, risk: two concurrent processes) or open Chromium with `--no-startup-window` and check the DevTools Protocol is responsive within a timeout before declaring the profile available.

**Chromium launch flags**

All three flags (`--disable-extensions`, `--disable-background-networking`, `--disable-component-update`) are confirmed current as of the GoogleChrome/chrome-launcher reference document (updated July 2025). None are in the deprecated-flags list. However, a Puppeteer issue (#13010, opened August 2024, closed with PR #13201) references a Chromium CL (5787627) that removes the need for `--disable-component-update` after Chrome M130. The flag is not removed — it still works — but automated tooling no longer needs it because Chromium M130+ suppresses component updates in certain headless/automation contexts. **No action required, but worth noting in the pinned-Chromium rationale.**

**`--use-encryption-for-profile-data`**

This flag does **not appear** in rod's launcher, the GoogleChrome/chrome-launcher reference doc, or any Chromium automation documentation searched. It is not in the peter.sh Chromium flag database as a standard flag. It appears to be either: (a) an internal/unreleased flag, (b) misremembered — the actual per-platform cookie/password encryption in Chromium is controlled by the OS keychain integration built into Chromium itself (Safe Storage on macOS, DPAPI on Windows, gnome-keyring/kwallet on Linux), not a command-line flag. **The spec's instruction to "Launch Chromium with `--use-encryption-for-profile-data`" has no verified backing. This flag likely does not exist as specified.** The correct way to ensure Chromium uses OS-level encryption is to ensure the OS keychain is available and unlocked at launch time — Chromium will use it automatically. The verification step ("confirm profile encryption is backed by the OS keyring, NOT Chromium's hardcoded Safe Storage fallback") is meaningful and implementable, but via detecting which keyring backend Chromium chose (readable from the `Local State` JSON in the profile directory: `os_crypt.app_bound_enabled` on Windows, `os_crypt.key_provider` on other platforms), not via a command-line flag.

**`github.com/danieljoos/wincred` — pure Go status**

Confirmed pure Go via verified source (`sys.go`). Uses `golang.org/x/sys/windows` lazy DLL loading (`windows.NewLazySystemDLL("advapi32.dll")`) — no `import "C"`, no CGo. Plan's "pure Go" assertion is correct. Maximum blob size: **2560 bytes** (`CRED_MAX_CREDENTIAL_BLOB_SIZE = 5*512`), confirmed from both wincred documentation and Microsoft's official CREDENTIAL struct spec. Default persistence: `CRED_PERSIST_LOCAL_MACHINE` (user-visible on this machine, not roamed). DPAPI scope is user-scoped for `GENERIC` credentials — tied to the user's SID, not the machine. On domain-joined machines, `CRED_PERSIST_ENTERPRISE` allows roaming but pan-agent should stick with `LOCAL_MACHINE` for predictability.

**2560-byte limit significance**: An HMAC-SHA256 key (32 bytes) or AES-256 key (32 bytes) fit trivially. The spec stores a "browser-profile master key" — if this is raw key material, it is safe. If it is a composite struct serialised to JSON, the implementor must verify the serialised size stays under 2560 bytes.

**`github.com/godbus/dbus/v5` — Secret Service semantics**

The godbus package is a pure D-Bus transport binding; it does not implement Secret Service itself. The Secret Service API is implemented by gnome-keyring-daemon or kwalletd, and accessed over D-Bus using the `org.freedesktop.Secret.Service` interface. When no secret service daemon is running, `dbus.ConnectSessionBus()` may succeed (the D-Bus session bus is usually available) but `Object(...).Call("org.freedesktop.Secret.Service.OpenSession", ...)` will return `org.freedesktop.DBus.Error.ServiceUnknown`. The spec's plan to use godbus directly requires implementing the Secret Service protocol from scratch. **A more robust choice is `github.com/ppacher/go-dbus-keyring`** (MIT, pure Go, Secret Service implementation on top of godbus) or `github.com/zalando/go-keyring` (cross-platform keyring abstraction). Both spare the implementor from protocol-level D-Bus calls and handle the `ServiceUnknown` error path.

**macOS `security` CLI — SSH/non-GUI behaviour**

Confirmed: `security find-generic-password` **fails silently** in SSH sessions where the login keychain is locked. The command returns a non-zero exit code but no readable error to stderr in some configurations. The login keychain is unlocked during a GUI session but not automatically in an SSH session (deliberate macOS security constraint). The workaround is `security unlock-keychain ~/Library/Keychains/login.keychain-db` which prompts for the user's login password — but this cannot be automated non-interactively. The spec correctly identifies this as a "fail-closed with WARN + degrade in CI/headless mode" scenario. What is not specified: the exact detection path. The exit code from `security find-generic-password` is `44` when the item is not found and `128` (or varies) on keychain-locked errors — the implementation must distinguish between "keychain unavailable" and "item not found" to give meaningful diagnostics rather than both appearing as a generic failure.

### (b) Cross-platform behavioural risks

1. **rod's `password-store=basic` flag breaks persistent profile cookie encryption.** When `UserDataDir` is set and `password-store=basic` is present (rod's default), Chromium's Safe Storage backend is bypassed. This means cookies are stored unencrypted in the profile directory, defeating the spec's security goal of OS-keyring-backed profile encryption. The fix is known (delete the flag) but is not mentioned in the spec.

2. **`--use-encryption-for-profile-data` is likely a non-existent flag.** The spec builds a startup verification step around confirming this flag is active. If the flag does not exist, the verification step will silently be a no-op (Chromium ignores unrecognised flags). The actual Chromium encryption state is readable from `<profile>/Local State` JSON.

3. **Stale `SingletonLock` after crash requires explicit handling.** Rod has no API for this. The spec says "Monitor SingletonLock" but the mechanism is unspecified. Without explicit cleanup, crashed pan-agent leaves the profile directory unusable until the user manually deletes the lock file.

4. **macOS keychain rotation on login password change.** When the macOS login password changes, the login keychain is re-encrypted with the new password. Items written by `security add-generic-password` remain accessible in subsequent GUI sessions (macOS handles this transparently via keychain re-encryption during login). However, if the password change happens via `dscl` or in a non-interactive context, the keychain may not be automatically re-encrypted. This is the "Keychain rotation recovery" open question in the spec — it remains open and should block v0.5.0 per the spec's own statement.

### (c) Prior art

The Chromium Safe Storage detection technique is documented in browser forensics tooling. The key reference: the `Local State` file in the Chrome user data directory contains `os_crypt` keys that identify which storage backend Chromium chose at startup (`app_bound_enabled` on Windows, `encrypted_key` on macOS). This is the mechanism used by browser forensics tools (e.g., ElcomSoft) to detect whether App-Bound Encryption is active. Pan-agent can use the same approach for its startup verification step instead of relying on a non-existent flag. Reference: [Browser Forensics in 2026: App-Bound Encryption and Live Triage — ElcomSoft blog](https://blog.elcomsoft.com/2026/01/browser-forensics-in-2026-app-bound-encryption-and-live-triage/)

---

## WS2 — Action Journal + Rollback Layer

### (a) Library / API verification

**`cp -c` (macOS APFS clone) and `cp --reflink=always` (Linux)**

Both flags confirmed current. On macOS, `cp -c` uses `clonefile(2)` for APFS CoW clones. On Linux, `cp --reflink=always` fails with `EINVAL` on non-reflink filesystems (btrfs without reflink support, ext4, APFS-over-SMB, tmpfs). The spec's capability probe step (attempt a 1-byte clone, verify success) is the correct approach to detect whether reflink works before relying on it.

A pure-Go alternative exists: `github.com/KarpelesLab/reflink` (uses `FICLONE` ioctl on Linux, falls back to `copy_file_range`, then `io.Copy`). **However, it currently has no macOS APFS support** (macOS `clonefile` is listed as a future consideration). Using it would require the macOS path to remain a shell-out to `cp -c`, making the library a partial solution. The spec's shell-out approach is therefore correct for now, though the implementor should revisit `KarpelesLab/reflink` for macOS support status before v0.6.0.

**SQLite WAL snapshotting during active writes**

This is a hard limitation relevant to the spec's explicit exclusion of live browser profile snapshots. Confirmed: copying `.db + .wal + .shm` files without the SQLite Online Backup API produces inconsistent backups when a writer is active. The shared-memory `-shm` file coordinates WAL access across processes; copying it out-of-band produces undefined results. The correct approach for pan-agent's own `state.db` snapshots (for the recovery journal itself) during long task runs is either the SQLite Online Backup API (via `modernc.org/sqlite`'s `sqlite3_backup_*` bindings) or `VACUUM INTO 'snapshot.db'` executed as a single transaction. The spec does not address how the action journal database itself is snapshotted if the user wants to export or back it up — this is a gap if the feature is ever requested.

**HMAC-SHA256 key storage via keyring (per-profile key)**

The HMAC-SHA256 key for secret masking (D5) is stored per-profile via the platform keyring. At 32 bytes, it fits comfortably within wincred's 2560-byte limit. No API-level concern here.

**`internal/secret/` redaction — Presidio-ported regex patterns**

The spec mentions "Presidio-compatible regex patterns ported to Go." Presidio is a Python library (Microsoft). Its regex patterns for PII and credentials are MIT-licensed and portable. No Go port of Presidio was found in package searches. The implementor will need to manually port the relevant patterns (credit card, SSN, email, API key formats) or use an existing Go PII detection library. **Unverified assumption**: the spec treats Presidio porting as straightforward; in practice, Presidio's recognisers use stochastic context (NLP model + regex) for high-confidence detection. A regex-only port will have higher false-positive rates on ambiguous strings.

### (b) Cross-platform behavioural risks

1. **`cp --reflink=always` exits non-zero on non-reflink filesystems.** This is by design (the `=always` flag means "fail if reflink is unavailable"). The spec's capability probe step handles this correctly. Risk: if the probe is not per-mount-point but per-OS, operations on a second mount (e.g., `/tmp` on a different filesystem) will incorrectly inherit the cached probe result. The spec addresses this with `(device_id, mount_id)` caching, which is correct.

2. **SQLite WAL checkpoint race during pan-agent shutdown.** If a task is running when the user kills pan-agent, an in-progress WAL checkpoint may leave the database in a state where the `-wal` file contains committed data not yet merged into the main `.db` file. On next startup, SQLite's WAL recovery handles this automatically (the WAL is replayed on first open). This is safe. However, if pan-agent's binary is replaced during this window (auto-update), and the new binary uses a different schema, the WAL replay may apply old-schema transactions to a new-schema database. The spec has a migration harness but its interaction with in-flight WAL files is unspecified.

3. **Windows profile directory corruption on Chromium crash mid-write.** If Chromium crashes while writing to `Cookies` or `Network/Cookies` (SQLite databases within the profile), the WAL file may be left in a partially-written state. Chromium's own WAL recovery handles this on next startup, but only if the profile is opened by Chromium itself. If pan-agent tries to read profile files directly (for snapshot or verification), it may encounter a corrupt or locked state. The spec correctly excludes the browser profile from CoW snapshot scope (C2), so this risk is limited to the verification step that reads `Local State`.

### (c) Prior art

OpenInterpreter's `--safe_mode` flag uses Git as the snapshot backend for code changes: before executing a code block, it runs `git add -A && git stash`, and on failure runs `git stash pop`. This is the closest published prior art for a reversible code-execution lane. The key design difference: pan-agent's tiered CoW + audit-only approach is filesystem-agnostic, whereas OpenInterpreter's approach requires Git and only covers files tracked by Git. Reference: [OpenInterpreter safe_mode documentation](https://github.com/OpenInterpreter/open-interpreter)

---

## WS3 — Vision with Provider Passthrough Abstraction

### (a) Library / API verification

**Anthropic `computer_use` tool — current API shape**

Verified from live API docs (2026-04-18). Current tool type for Opus 4.6 / Sonnet 4.6: `computer_20251124` with beta header `computer-use-2025-11-24`. The tool schema requires `display_width_px` and `display_height_px`; `display_number` is optional (X11 only); `enable_zoom: true` unlocks the `zoom` action for Opus 4.7/4.6/Sonnet 4.6. Actions available in `computer_20251124`: all `computer_20250124` actions plus `zoom`. The tool is **client-side** — Claude returns action requests; the caller implements screenshot capture, mouse, keyboard. There is no "passthrough where Claude directly controls the computer" — the caller always intermediates.

**Implication for the spec's "provider computer_use passthrough" framing**: the spec describes this as "provider computer_use passthrough (Anthropic computer-use tool etc.)" implying a different code path from base64 image_url. This is accurate architecturally: when using `computer_20251124`, the LLM response contains structured action requests rather than coordinate strings, and the caller's tool-execution loop differs from a base64-vision loop. The router in `vision.go` must detect which models/providers support the native computer-use tool versus base64 image_url and select accordingly.

**Model requirement**: `computer-use-2025-11-24` supports Opus 4.7, Opus 4.6, Sonnet 4.6, Opus 4.5. It does NOT support GPT-4o or Google Gemini natively (they have separate computer-use APIs with different schemas). The spec's "provider computer_use passthrough" must be provider-specific; there is no unified computer-use schema across providers.

**Screenshot resize constraint**: the spec says ≤1024px wide. The Anthropic docs specify the limit differently: maximum 1568px on the longest edge and approximately 1.15 megapixels total. Opus 4.7 supports up to 2576px on the long edge. The spec's 1024px target is conservative (safe for all models including older ones) but may under-utilize Opus 4.7's zoom capability. **No risk, but the 1024px constant should be parameterised per-model.**

**Playwright ARIA snapshots** — the spec references `locator.ariaSnapshot()`. This is confirmed as a Playwright API, not available in rod (rod uses DevTools Protocol directly, not Playwright's abstraction layer). The spec's `accessibility.go` will need to implement ARIA tree extraction via DevTools Protocol's `Accessibility.getFullAXTree` or `DOM.describeNode` calls, not via Playwright. The YAML output format would need to be replicated manually. This is implementable but non-trivial.

**macOS TCC permissions in SSH/headless context**

`AXIsProcessTrustedWithOptions` and `CGRequestScreenCaptureAccess` both require a GUI session. Confirmed: in SSH sessions without a graphical context, `CGRequestScreenCaptureAccess()` returns `false` and cannot trigger a permission prompt — `tccd` logs "Service kTCCServiceScreenCapture does not allow prompting; returning denied." Similarly, `AXIsProcessTrustedWithOptions(kAXTrustedCheckOptionPrompt=true)` is a no-op in non-GUI contexts. This means WS3 vision capabilities silently fail in headless environments, which is acceptable and consistent with the spec's "fail-closed with WARN" design for WS1.

### (b) Cross-platform behavioural risks

1. **No unified computer-use schema across providers.** Anthropic uses `computer_20251124`; Google and OpenAI have separate schemas. The spec's router must maintain per-provider tool definitions. A provider that pan-agent switches to mid-session will require re-initialising the tool definition set, which may break the SSE stream if done incorrectly.

2. **`locator.ariaSnapshot()` is Playwright-only.** Rod does not expose this API. Implementing ARIA-YAML observation requires raw DevTools Protocol calls. The output format (YAML accessibility tree) is not standardised — the spec may need to define pan-agent's own schema rather than assuming Playwright compatibility.

3. **Coordinate scaling mismatch with provider computer-use.** When using `computer_20251124`, the model returns coordinates in the declared `display_width_px / display_height_px` space. If pan-agent resizes screenshots before sending them but declares the original dimensions, the returned coordinates will be off. The coordinate scaling must be consistent between what is sent and what is declared — a bug here produces systematic click misses that are hard to diagnose.

### (c) Prior art

The Cerebellum browser agent (theredsix/cerebellum) implements a confidence-scored action selector that chooses between DOM-path click, ARIA locator, and vision fallback — the closest published implementation of the interaction hierarchy the spec describes. It is small enough to read end-to-end and directly relevant to `router.go`'s confidence-scoring design. Reference: [theredsix/cerebellum on GitHub](https://github.com/theredsix/cerebellum)

---

## WS4 — Durable Task Runner

### (a) Library / API verification

**SQLite WAL with two writers (main writer + heartbeat writer)**

The spec mandates a separate lightweight writer goroutine for heartbeats (C3). SQLite WAL supports exactly one concurrent writer — it serialises multiple writers via a write lock, not by error. In `modernc.org/sqlite` with `SetMaxOpenConns(1)`, a second goroutine attempting to write while the main writer holds a transaction will block until the transaction completes (WAL write lock behaviour). This is safe, but under worst-case conditions (long-running main-writer transaction batching tool events), the heartbeat writer could be delayed beyond the 60-second stale threshold. The spec's mitigation — "separate lightweight writer" — is correct only if "separate" means a separate database connection with its own connection pool slot (i.e., `SetMaxOpenConns` must be at least 2, or the heartbeat uses a separate `*sql.DB` opened against the same file). A single `*sql.DB` with `SetMaxOpenConns(1)` will serialise both writers through the same connection, defeating the purpose.

**Clarification needed**: the spec says `SetMaxOpenConns(1)` for the main writer. If the heartbeat uses the same `*sql.DB`, it contends for that one connection. The correct implementation is two separate `*sql.DB` instances: one for the main writer (cap 1), one for the heartbeat writer (cap 1), both opening the same WAL-mode SQLite file. WAL mode supports multiple concurrent writers (they serialise internally), so this is valid.

**`UNIQUE (task_id, step_id, kind, attempt)` constraint on task_events**

This constraint is the step-memoisation guard. It is correct for preventing re-execution during resume. However, the constraint also means that if a step produces multiple events of the same `kind` in a single attempt (e.g., two consecutive `tool_call` events in one step iteration), the second insert will fail with `UNIQUE constraint failed`. The spec should clarify that `kind` values within a step are unique per attempt (i.e., only one `tool_call` record per step per attempt), or add a `sub_sequence` column, or relax the uniqueness to `(task_id, step_id, sequence)` where `sequence` is the monotonic counter already on the table.

**Reaper goroutine — monotonic vs wall-clock time**

The spec correctly distinguishes monotonic time (for in-process liveness) from wall-clock time (for `last_heartbeat_at`). Go's `time.Now()` returns a value with both monotonic and wall-clock readings; `time.Since()` uses the monotonic component. When the heartbeat writes `last_heartbeat_at = time.Now().Unix()` (wall clock), and the reaper checks `last_heartbeat_at < now - 60s` (also wall clock), the comparison is correct. No issue here.

### (b) Cross-platform behavioural risks

1. **Two `*sql.DB` instances on the same WAL file.** WAL mode supports multiple readers and one writer. If both the main writer and heartbeat writer attempt to write simultaneously, one will block on the WAL write lock. This is safe (not corrupt) but means the heartbeat can be delayed if the main writer holds a long transaction. The spec should cap the main writer's transaction size (batched inserts per tool step, not per-task) to keep transactions short.

2. **SQLite WAL file growth under continuous task runs.** Without periodic checkpointing, the WAL file grows unboundedly. `modernc.org/sqlite` defaults to auto-checkpoint at 1000 pages, but this checkpoint can be blocked by a long-running read transaction (the SSE stream delivering events to the frontend is a continuous reader). The implementation should use `PRAGMA wal_autocheckpoint=1000` and ensure the SSE stream does not hold open read transactions across event boundaries.

3. **`next_plan_step_index` integer step memoisation vs. content-addressed step IDs.** If the LLM replans mid-task (the plan changes between resume attempts), `next_plan_step_index` points to a step that no longer exists or has different semantics. The spec uses `step_id` as a stable identifier but the resume logic uses `next_plan_step_index` as a pointer. These two must be kept consistent — if replanning is allowed during a paused task, the memoisation model breaks.

### (c) Prior art

Devin's ACU (Agent Compute Unit) billing model is the closest published prior art for the pause-not-terminate budget pattern. Devin tracks compute units per 15-minute work increment and displays real-time consumption; when a per-ticket spend limit is reached, activity stops and the user is notified to increase the limit. The distinction from pan-agent's design: Devin's stop is closer to a hard pause with preserved state in Cognition's infrastructure; pan-agent must implement the same semantic durably in a local SQLite database. Reference: [Devin Billing documentation](https://docs.devin.ai/admin/billing)

---

## WS5 — macOS Permission Onboarding Wizard

### (a) Library / API verification

**`AXIsProcessTrustedWithOptions`**

Confirmed behaviour: returns `true` (trusted) or `false` (not trusted). When called with `kAXTrustedCheckOptionPrompt = true`, it shows the system dialog OR redirects to System Settings → Accessibility. The spec's approach of calling this and then using a no-op `AXUIElementCopyAttributeValue` to distinguish "never prompted" from "previously denied" is documented in Apple Developer Forums. However: in **SSH sessions without a GUI**, `AXIsProcessTrustedWithOptions` returns `false` and the `kAXTrustedCheckOptionPrompt` option is silently ignored — no prompt is shown. The spec's headless degradation path (log WARN, degrade) handles this correctly.

**`CGPreflightScreenCaptureAccess()` + `CGRequestScreenCaptureAccess()`**

Confirmed API. `CGPreflightScreenCaptureAccess()` returns `true` (granted) or `false` (not granted, includes both denied and never-prompted). `CGRequestScreenCaptureAccess()` is the call that adds the app to System Settings → Screen Recording and triggers the prompt. In SSH/headless contexts: confirmed via search that `tccd` logs "Service kTCCServiceScreenCapture does not allow prompting; returning denied" — the preflight returns `false` and the request call is a no-op.

**`security` CLI for Automation permission**

Automation permission (TCC `kTCCServiceAppleEvents`) is checked by attempting an osascript no-op. Error code `-1743` means "not authorised to send Apple events to X." This is correct and confirmed. However, the permission is per-target-app (the script must target a specific application), so the check is meaningful only when a specific app is targeted. A generic no-op to `Finder` is the standard probe. In SSH sessions: osascript commands that target GUI applications fail with "no screen available" or "connection invalid" errors, not `-1743` — the error code discrimination must handle both cases.

**`security-framework` Rust crate (WS5 Tauri layer)**

The spec adds the Rust `security-framework` crate for `permissions.rs`. This is a well-established MIT-licensed crate wrapping Apple's Security framework. No concerns with this choice.

**MDM detection via `profiles -P`**

`profiles -P` requires root on macOS 13+. On macOS 13 Ventura and later, running `profiles` without root returns an error ("profiles requires root access"). The spec documents this as "best-effort" and "not reliable" — which is accurate. However, the implementation must handle the non-root case gracefully (treat as "MDM status unknown") and not surface a confusing error to the user.

### (b) Cross-platform behavioural risks

1. **`profiles -P` requires root on macOS 13+.** Running it without root will produce an error output that, if parsed naively, could be misinterpreted as "no MDM profiles." The implementation must check the exit code and stderr explicitly.

2. **Permission polling at 1-second intervals may conflict with macOS's TCC debounce.** The spec says "Poll every 1s during grant flow." TCC state changes propagate asynchronously after the user toggles the System Settings switch. In practice, the TCC state is readable within ~0.5-2 seconds of the toggle. 1-second polling is safe. However, the Tauri `permissions.rs` probe calls (`AXIsProcessTrustedWithOptions`, `CGPreflightScreenCaptureAccess`) are synchronous and may briefly block the main thread if called from the UI thread. Calls should be dispatched to a background task.

3. **"Previously denied" detection is best-effort.** The spec acknowledges this. In practice, distinguishing "never prompted" from "denied" for Accessibility requires the no-op `AXUIElementCopyAttributeValue` heuristic. This heuristic can produce false positives (reports "denied" when the system is slow to respond). The CTA label change ("Open Settings (previously denied)" vs "Grant Access") will sometimes be wrong, but this is a UX annoyance, not a security issue.

4. **Automation permission is lazily prompted on first use.** The spec places Automation in the "contextual prompt on first use" category. If the user cancels the prompt, subsequent attempts to use AppleScript-based direct-API tools will silently fail (error `-1743`). The WS3 direct-API cookbook skills must handle this error gracefully and surface a user-facing "Automation permission required" message rather than propagating the osascript exit code.

### (c) Prior art

Raycast's permission polling pattern (poll every 1s, flip to green-check immediately on grant) is cited in the spec. The underlying technique — polling `AXIsProcessTrustedWithOptions` in a tight loop during the grant flow — is widely used in macOS automation tools (CleanShot, Rewind). The key implementation detail the spec captures correctly: the Finder and System Settings windows remain in focus during the grant flow; the polling must not bring the pan-agent window back to front on each check. Reference: [Raycast's macOS permission handling pattern (observed from Raycast source)](https://raycast.com)
