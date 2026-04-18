# Phase 12 — `internal/recovery/` architecture

**Status**: DESIGN (stage 1 of the Phase 12 parallel pipeline — 2026-04-18)
**Spec**: `docs/design/phase12.md` v3 (workstream #2 — "the moat")
**Scope**: action journal, pre-execution FS snapshots, per-tool reversers,
and the `/v1/recovery/*` HTTP surface. Pure Go, no CGo, uses existing
`modernc.org/sqlite`.

This package is the differentiation bet of Phase 12: local-first desktop
automation paired with true post-hoc reversibility. It owns
`action_receipts` (append-only journal), filesystem snapshots outside
`<DataDir>/browser-profile/`, and the reverser registry. WS#4 (task runner)
writes into the journal; WS#1 (browser persistence) deliberately does NOT
snapshot its profile — the rationale and the user-visible gate for that
decision is R1 below.

---

## R1 — Keyring verification mechanism (CRITICAL)

Phase 12 spec WS#1 mandates: *"Verify at startup that Chromium profile
encryption is backed by the OS keyring, NOT Chromium's hardcoded Safe
Storage fallback."* The mechanism must minimize false positives because a
false positive gates desktop sessions behind a Setup banner the user
cannot resolve.

### Options evaluated

#### Option 1 — Canary probe (keyring → Chromium Local State correlation)

Write a known credential to the OS keyring, inspect Chromium's
`Local State` JSON for an `os_crypt.encrypted_key` entry, correlate.

- **Detects**: that Chromium has *ever* successfully called the OS keyring.
- **Fails on**: Local State is written lazily — a fresh profile that has
  never stored a credential has no `encrypted_key` field at all, so the
  probe returns "unknown" for every first-run user. Works only after
  Chromium has itself written a secret, creating a chicken-and-egg.
- **False positive rate**: high (every first run).
- **Verdict**: rejected. Useless for first-launch, which is exactly when we
  need to gate.

#### Option 2 — Parse `chrome-key-data` sentinel file

Some Chromium versions on Linux drop a `chrome-key-data` marker when the
hardcoded fallback is used.

- **Detects**: fallback on Linux *some* Chromium versions.
- **Fails on**: no equivalent on Windows (DPAPI uses user-SID-derived key,
  no sentinel file) or macOS (Safe Storage is stored in Keychain either
  way — the difference is "is the Keychain accessible" vs "did Chromium
  fall back to in-binary"). The file is also version-dependent — we pin
  Chromium, but Chromium patches this surface silently.
- **Verdict**: rejected. Platform-fragmented, version-fragile, requires
  running Chromium first.

#### Option 3 — Empirical launch with stderr/DBus signal inspection

Run Chromium once at first run and parse stderr or DBus signals for a
fallback indicator string.

- **Detects**: the actual runtime behaviour Chromium takes.
- **Fails on**: requires full Chromium download + launch at first-run
  *before* the Setup wizard finishes — users on slow connections see a
  90-second stall. stderr strings are localized and version-drift. DBus
  signals are Linux-only. The launch itself writes to the user-data-dir,
  polluting the check with state we then have to throw away.
- **Verdict**: rejected. Expensive, localized-stderr-fragile, side-effects
  on user-data-dir.

#### Option 4 — Platform-specific keyring daemon responsiveness probe BEFORE Chromium launch — **CHOSEN**

Chromium automatically uses the OS keyring when available, falling back
silently to a hardcoded key when not. We probe the same daemon Chromium
will call; if the probe succeeds, Chromium's automatic call will also
succeed. We own the probe so we own the signal.

The probe uses the `internal/secret/` package we already need for the
browser-profile master key — it is the same operation we were going to
perform anyway, reused as the verification signal.

- **Windows**: `secret.Set("r1-probe", "pan-agent-keyring-probe")` +
  `secret.Get("r1-probe")` + `secret.Delete("r1-probe")`. A responsive
  Credential Manager round-trips in <5 ms. `ErrKeyringUnavailable` →
  fallback likely. `ErrNotFound` on Get after Set → corruption (shouldn't
  happen, treat as unavailable).
- **macOS**: same three calls, but `/usr/bin/security add-generic-password`
  returns exit code 45 when the Keychain is locked and the user has denied
  the auth prompt; we treat that as `ErrKeyringUnavailable` *and* surface a
  distinct Setup banner: "Unlock login keychain to enable browser sign-ins."
- **Linux**: same three calls via dbus. `ServiceUnknown` →
  `ErrKeyringUnavailable` → fallback likely (no `gnome-keyring-daemon`,
  `kwalletd5`, or similar running). `IsLocked` with unsuccessful unlock →
  same.

### Exact failure mode detected

"The OS keyring daemon is not servicing requests at the moment pan-agent is
about to launch Chromium." Chromium automatically uses the OS keyring when
available and falls back silently to its hardcoded Safe Storage key when
not. The probe is identical to the call Chromium itself makes microseconds
later — if the probe succeeds, Chromium's automatic call will also succeed.
The probe races with Chromium launch (an attacker could kill the daemon
between probe and launch), but the attack model Phase 12 defends against
is "sloppy environment" (CI, headless, fresh VM) not "active tampering
during setup" — the latter is Phase 13.

### False positives considered

| Scenario | Probe says | Ground truth | Mitigation |
|----------|-----------|--------------|------------|
| GNOME/KDE user, daemon healthy, locked but auto-unlock on login | ✅ | ✅ | none needed |
| macOS, iCloud Keychain drifted and asks for password | ❌ | will be ✅ after prompt | retry once after user-visible prompt banner |
| Windows, Credential Manager service stopped | ❌ | ❌ | correct rejection |
| Linux with `keyutils` kernel keyring but no Secret Service | ❌ | ❌ | correct rejection (Chromium does not use kernel keyring) |
| CI runner with no keyring daemon at all | ❌ | ❌ | correct rejection; CI path sets `PAN_AGENT_KEYRING_MODE=degraded` env and logs WARN instead of gating |
| Corporate MDM that silently proxies keyring calls | ✅ | usually ✅ | accept; this is the same path as regular desktop |
| macOS Keychain locked at probe time but user unlocks before Chromium launch | ❌ | ✅ later | acceptable — user re-runs Setup wizard after unlock; 1s poll flow picks it up |

### Setup banner copy

When probe fails in a desktop session (not CI/headless), Setup wizard shows:

> **Browser sign-in is not protected yet.**
>
> pan-agent could not reach the system keyring, so any website logins the
> agent saves in its browser would fall back to a built-in encryption key
> shared with every pan-agent install — not unique to you.
>
> Fix options:
> - **macOS**: unlock your login keychain in *Keychain Access*, then click
>   **Recheck**.
> - **Windows**: ensure the *Credential Manager* service is running, then
>   click **Recheck**.
> - **Linux**: start `gnome-keyring-daemon --start --components=secrets` or
>   equivalent, then click **Recheck**.
>
> You can continue with an **ephemeral browser profile** (cleared on
> restart — no persistent sign-ins) while you fix this.
> [Recheck] [Continue ephemeral] [Open docs]

Key design points:

- Two actionable buttons + one informational. Never a silent "continue
  broken" path.
- "Ephemeral" is explicitly tied back to v0.4.5 — the user downgrades to
  the ephemeral profile mode we already ship, rather than shipping a
  second half-broken persistence path.
- The probe reruns every 1s while the banner is visible (same pattern as
  WS#5 TCC permission polling), so unlocking the Keychain in another
  window flips the state automatically without a click.

---

## Package layout

```
internal/recovery/
  journal.go          — action_receipts table, writer API, reader API
  snapshot.go         — pre-execution FS snapshots under <DataDir>/recovery/<session-id>/
  snapshot_darwin.go  — cp -c (APFS clone) tier-1 implementation
  snapshot_linux.go   — cp --reflink=always (btrfs/xfs/bcachefs) tier-1 implementation
  snapshot_copy.go    — plain os.CopyFS tier-2 implementation, all platforms
  reversers.go        — per-tool reverse() registry (fs, shell, browser-form stub)
  endpoints.go        — /v1/recovery/{list,undo,diff} HTTP handlers
  reaper.go           — shared heartbeat watcher (WS#4 reuses)
  journal_test.go     — table-driven writer/reader tests
  snapshot_test.go    — capability-probe tests + size-cap tests
  reversers_test.go   — per-reverser correctness + fail-closed tests
  endpoints_test.go   — httptest.Server round-trip tests
```

Cross-platform pattern mirrors `internal/tools/` (`_windows.go`, `_darwin.go`,
`_linux.go`, `_stub.go`). No `_windows.go` snapshot tier-1 file because
NTFS does not have a cheap CoW clone primitive usable from a shell-out —
Windows uses the `snapshot_copy.go` fallback directly with a slightly
higher cap (see below).

---

## File 1 — `journal.go`

Append-only SQLite log. Schema per phase12.md:

```sql
CREATE TABLE action_receipts (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  event_id INTEGER,
  kind TEXT NOT NULL,           -- 'fs_write'|'fs_delete'|'shell'|'browser_form'|'saas_api'
  snapshot_tier TEXT NOT NULL,  -- 'cow'|'copyfs'|'audit_only'
  reversal_status TEXT NOT NULL,
  redacted_payload TEXT,
  saas_deep_link TEXT,
  created_at INTEGER NOT NULL,
  FOREIGN KEY (task_id) REFERENCES tasks(id),
  FOREIGN KEY (event_id) REFERENCES task_events(id)
);
CREATE INDEX idx_action_receipts_task_created ON action_receipts(task_id, created_at);
```

Migration is appended to `internal/storage/db.go`'s `migrate()` function as
a new `CREATE TABLE IF NOT EXISTS` block — not a separate migration file,
because storage migrations are idempotent append-only today and
`internal/storage/` is the one owning all schema changes.

### Struct fields

```go
// Journal is the writer + reader facade over action_receipts.
// Safe for concurrent use; SQLite's single-writer pool handles serialization.
type Journal struct {
    db   *sql.DB      // shared with internal/storage.DB
    hmac *secret.Redactor // redacts payloads before write (D5)
    now  func() int64 // injectable clock for tests
}

// Receipt is the in-memory shape tools pass to Record.
type Receipt struct {
    ID             string            // uuid v4; generated here if empty
    TaskID         string            // foreign key — required
    EventID        *int64            // nullable FK to task_events
    Kind           ReceiptKind       // fs_write | fs_delete | shell | browser_form | saas_api
    SnapshotTier   SnapshotTier      // cow | copyfs | audit_only
    ReversalStatus ReversalStatus    // reversible | audit_only | reversed_externally | irrecoverable
    Payload        []byte            // RAW payload — journal redacts before write
    SaaSDeepLink   string            // optional; captured at action time, not reconstructed
}

type ReceiptKind string
const (
    KindFSWrite      ReceiptKind = "fs_write"
    KindFSDelete     ReceiptKind = "fs_delete"
    KindShell        ReceiptKind = "shell"
    KindBrowserForm  ReceiptKind = "browser_form"
    KindSaaSAPI      ReceiptKind = "saas_api"
)

type SnapshotTier string
const (
    TierCoW        SnapshotTier = "cow"
    TierCopyFS     SnapshotTier = "copyfs"
    TierAuditOnly  SnapshotTier = "audit_only"
)

type ReversalStatus string
const (
    StatusReversible        ReversalStatus = "reversible"
    StatusAuditOnly         ReversalStatus = "audit_only"
    StatusReversedExternally ReversalStatus = "reversed_externally"
    StatusIrrecoverable     ReversalStatus = "irrecoverable"
)
```

### Writer API

```go
// Record writes a receipt with the payload HMAC-redacted via internal/secret.
// The raw payload is never persisted — the coder must ensure no code path
// stores r.Payload before redaction.
func (j *Journal) Record(ctx context.Context, r Receipt) error

// Update mutates reversal_status only (reversal flow transitions
// 'reversible' → 'reversed_externally'). Other columns are append-only.
func (j *Journal) UpdateStatus(ctx context.Context, id string, status ReversalStatus) error
```

### Reader API

```go
// List returns receipts for a task, newest first. Payloads returned to the
// caller are already redacted (the way they are stored).
func (j *Journal) List(ctx context.Context, taskID string, limit, offset int) ([]Receipt, error)

// ListSession returns all receipts for all tasks in a session — feeds the
// Desktop History screen's two-lane view.
func (j *Journal) ListSession(ctx context.Context, sessionID string, limit, offset int) ([]Receipt, error)

// Get returns a single receipt by id.
func (j *Journal) Get(ctx context.Context, id string) (Receipt, error)
```

### Sentinel errors

```go
var (
    ErrReceiptNotFound      = errors.New("recovery: receipt not found")
    ErrReceiptAlreadyFinal  = errors.New("recovery: receipt status is final")
    ErrInvalidReceiptKind   = errors.New("recovery: invalid receipt kind")
    ErrUnknownReversalStatus = errors.New("recovery: unknown reversal status")
)
```

Follows `internal/approval/approval.go` pattern exactly.

### Test boundaries (`journal_test.go`)

- `TestRecordRedactsBeforeWrite` — inject a `secret.Redactor` that panics if
  raw payload appears in its output. Write a receipt containing `sk_test_…`.
  Read the row back via raw `db.QueryRow`; assert `redacted_payload` does
  not contain `sk_test_`.
- `TestListOrdering` — insert 20 receipts with staggered `created_at`;
  assert `List` returns newest-first.
- `TestUpdateStatusMonotonic` — cannot move `reversed_externally` back to
  `reversible`; returns `ErrReceiptAlreadyFinal`.
- `TestForeignKeyEnforced` — insert with non-existent `task_id`; expect
  SQLite FK violation (proves `PRAGMA foreign_keys=ON` is live).

---

## File 2 — `snapshot.go`

Pre-execution filesystem snapshots for the reversible lane. Scope per
phase12.md: filesystem edits **outside** `<DataDir>/browser-profile/`.

### Struct fields

```go
// Snapshotter captures files before the agent mutates them.
type Snapshotter struct {
    root       string            // <DataDir>/recovery/
    session    string            // session-id subdir
    probe      *capabilityCache  // cache (device_id, mount_id) → tier
    maxCopyMB  int               // hard cap for tier-2 (default 50)
    maxCopyN   int               // hard cap file count for tier-2 (default 500)
    clock      func() int64
}

// capabilityCache memoizes CoW probe results per (dev, mount). TTL 10 min
// because mounts can change; below-TTL hits avoid the probe entirely.
type capabilityCache struct {
    mu    sync.Mutex
    seen  map[mountKey]probeResult
}

type mountKey struct {
    dev  uint64
    ino  uint64 // inode of the mountpoint — cheap stand-in for mount_id on
                // systems without /proc/self/mountinfo
}

type probeResult struct {
    cowSupported bool
    at           int64
}
```

### API

```go
// Capture snapshots path into <DataDir>/recovery/<session>/<receipt-id>/.
// Returns the receipt-id's snapshot subpath AND the tier used. The tool
// (FS, shell) stores the subpath on the Receipt; Reverse uses it.
func (s *Snapshotter) Capture(ctx context.Context, path, receiptID string) (SnapshotInfo, error)

// CaptureMany is an optimization — one probe for N paths that share a
// mount. Used by shell reversers when approval classifier flags a
// destructive command affecting multiple paths.
func (s *Snapshotter) CaptureMany(ctx context.Context, paths []string, receiptID string) (SnapshotInfo, error)

// Restore copies the snapshot back over the live path. Returns the tier
// used for the restore (usually same as capture).
func (s *Snapshotter) Restore(ctx context.Context, info SnapshotInfo) error

// Purge removes all snapshots older than cutoff. Called by the reaper.
func (s *Snapshotter) Purge(ctx context.Context, cutoff int64) error

type SnapshotInfo struct {
    Tier      SnapshotTier
    ReceiptID string
    Subpath   string       // relative to s.root
    SizeBytes int64        // for UI "you are about to restore 42 MB"
    FileCount int
    DeviceID  uint64       // for Restore sanity check
}
```

### Tiering algorithm

1. `probe(path)` — compute `mountKey(dev, mount_ino)`. Cache lookup.
2. On miss: attempt a 1-byte clone/reflink in the destination's parent dir:
   - darwin: `exec.Command("/bin/cp", "-c", "/tmp/.rprobe-<rand>", "<dest>/.rprobe-<rand>")`
     with `err == nil` && file verified same-inode via `Stat` after.
   - linux: same but `cp --reflink=always`.
   - windows: skip probe, always use tier-2 (no equivalent primitive).
3. On probe success → tier-1 (`cp -c` / `cp --reflink=always`) for Capture.
4. On probe failure or `size > maxCopyMB` or `files > maxCopyN` → tier-2
   fallback (`os.CopyFS`).
5. On tier-2 cap exceeded → return `SnapshotInfo{Tier: TierAuditOnly}`;
   caller records receipt as audit-only.
6. Cross-device detection: if `Stat(src).Dev != Stat(dstParent).Dev` →
   always tier-2 (cannot reflink across devices).

### Sentinel errors

```go
var (
    ErrSnapshotOutsideSandbox = errors.New("recovery: refuse to snapshot browser-profile path")
    ErrSnapshotSizeExceeded   = errors.New("recovery: snapshot exceeds tier-2 size cap (audit-only)")
    ErrSnapshotReadonly       = errors.New("recovery: destination filesystem is read-only")
    ErrSnapshotCrossDevice    = errors.New("recovery: cannot reflink across devices")
)
```

### Hard exclusion — browser profile

Capture's first check:

```go
if insideBrowserProfile(path) {
    return SnapshotInfo{}, ErrSnapshotOutsideSandbox
}
```

`insideBrowserProfile` uses `filepath.Rel` against `paths.BrowserProfileDir()`
and checks the result does not start with `..`. Same defensive pattern as
`internal/skills/paths_internal.go`'s `resolveActiveDir`. This is not
advisory — Capture returns the sentinel and the reverser registry routes
browser-form actions to the audit-only lane regardless.

### Test boundaries (`snapshot_test.go`)

- `TestProbeCache` — two calls against the same path hit the cache; faked
  `exec.Command` runs only once.
- `TestCoWFallback` — simulate `cp -c` exit code 1 on first attempt; assert
  tier-2 path taken; assert cache marks `cowSupported: false` for subsequent
  calls on the same mount within TTL.
- `TestSizeCap` — build a 51 MB fixture; assert `TierAuditOnly` returned.
- `TestCrossDevice` — fake `Stat` returning different `Dev` for src vs
  dst; assert tier-2.
- `TestBrowserProfileRefused` — any path under `paths.BrowserProfileDir()`
  returns `ErrSnapshotOutsideSandbox`.
- `TestRestoreRoundtrip` — write file, Capture, modify file, Restore,
  assert byte equality with original. Runs on each platform that has a
  working tier-1; gated with `t.Skip` on Windows.
- `TestPurge` — create 5 snapshots with staggered ctimes; assert only
  those older than cutoff are removed.

---

## File 3 — `reversers.go`

Per-tool reverse() registry. Three families in v0.6.0: FS, shell,
browser-form.

### Registry API

```go
// Reverser is the contract each tool family implements.
type Reverser interface {
    // Kind reports which Receipt.Kind this reverser handles.
    Kind() ReceiptKind
    // Reverse applies the inverse operation described by the receipt.
    // Must be idempotent — a second call is a no-op that returns nil.
    Reverse(ctx context.Context, r Receipt) (ReverseResult, error)
}

type ReverseResult struct {
    Applied    bool       // true if state was changed, false if already reverted
    NewStatus  ReversalStatus
    Details    string     // human-readable for UI
}

// Registry dispatches by Receipt.Kind. Initialized once at package init;
// WS#4 can register additional reversers via Register for future extension.
type Registry struct {
    mu   sync.RWMutex
    byKind map[ReceiptKind]Reverser
}

func NewRegistry(j *Journal, s *Snapshotter) *Registry
func (r *Registry) Register(rev Reverser)
func (r *Registry) Reverse(ctx context.Context, receiptID string) (ReverseResult, error)
```

### The three reversers

#### 1. FSReverser — `Kind() == KindFSWrite | KindFSDelete`

- Reads the receipt, resolves the snapshot subpath.
- Delegates to `Snapshotter.Restore`.
- Updates the receipt status to `StatusReversedExternally` (the file
  system is reverted; the LLM's original intent is gone but the bytes
  are back).
- Fail-closed: if snapshot is missing / corrupted / outside `<DataDir>` →
  returns `ErrReversalFailed` without touching the live path.

#### 2. ShellReverser — `Kind() == KindShell`

Inverse-command pattern matching, fail-closed. The reverser consults a
compiled-in map of `(original-command-pattern) → inverse-command-generator`:

| Original pattern (normalized) | Inverse | Notes |
|---|---|---|
| `mkdir <path>`                | `rmdir <path>` | only if dir empty |
| `touch <path>` (file did not exist before — verified from snapshot) | `rm <path>` | |
| `cp <src> <dst>` (dst did not exist before) | `rm <dst>` | |
| `mv <src> <dst>`              | `mv <dst> <src>` | |
| `chmod <mode> <path>`         | `chmod <original-mode> <path>` | original-mode read from snapshot metadata |
| `chown …`                     | same, with original uid/gid | |
| `rm <path>` (snapshot succeeded)| restore from snapshot | delegates to FSReverser |
| anything else                 | — | `ErrNoInverseKnown` → status stays `reversible` → UI shows "No automatic reversal — inspect payload" |

The pattern table is defined in a separate `reversers_shell_patterns.go`
file so the audit surface is small and reviewable. Fail-closed is the
default: if the original command did not match any entry, the reverser
returns `ErrNoInverseKnown` and records a `ReverseResult{Applied: false,
NewStatus: StatusAuditOnly, Details: "no inverse known for: <command>"}`.

**Critical safety rule**: the inverse command is NEVER executed directly.
It is written into a new approval request (level = Catastrophic) and
presented to the user. The user confirms; only then does the shell tool
execute. This keeps the reverser from becoming its own attack surface —
an injection in the `<path>` field of a spoofed receipt cannot silently
execute because the approval flow catches it.

#### 3. BrowserFormReverser — `Kind() == KindBrowserForm`

**AUDIT-ONLY**. This reverser never executes anything.

- Records the intent "user wanted to undo browser form X submitted at
  time T to URL U."
- Updates receipt status to `StatusAuditOnly` (no-op — receipt was already
  audit-only when created per phase12.md).
- Emits a user-visible deep-link to the SaaS undo surface when available
  (e.g. Gmail "Undo send" 30-second window, Stripe "refund" button,
  Google Calendar "delete event" link).
- Returns `ReverseResult{Applied: false, NewStatus: StatusAuditOnly,
  Details: "Manual reversal required: <deep-link or guidance>"}`.

This is explicit per the spec: "Browser form submissions: always audit-only
lane — no snapshot of live profile." The reverser exists so the
`/v1/recovery/undo` endpoint has a consistent contract across kinds
(same return shape whether reversal is automatic or manual).

### Sentinel errors

```go
var (
    ErrNoReverserRegistered = errors.New("recovery: no reverser for receipt kind")
    ErrReversalFailed       = errors.New("recovery: reversal failed")
    ErrNoInverseKnown       = errors.New("recovery: no inverse known for command")
    ErrSnapshotMissing      = errors.New("recovery: snapshot missing or corrupted")
)
```

### Test boundaries (`reversers_test.go`)

- `TestFSReverserRoundtrip` — write, snapshot, mutate, reverse, assert bytes.
- `TestShellReverserFailClosed` — unknown command → `ErrNoInverseKnown`,
  status becomes `StatusAuditOnly`, no shell invocation happens.
- `TestShellReverserMkdirRmdir` — happy path for each entry in the pattern
  table.
- `TestShellReverserRequiresApproval` — inject a fake approval.Store; assert
  every shell reversal generates a Catastrophic approval before execution.
- `TestBrowserFormReverserNeverExecutes` — assert no tool invocation,
  no shell exec, no rod browser call; only journal status read + UI payload.
- `TestRegistryDispatch` — table of kind → expected reverser; unknown kind
  returns `ErrNoReverserRegistered`.

---

## File 4 — `endpoints.go`

HTTP handlers for `/v1/recovery/{list,undo,diff}`. Uses Go 1.22+ `ServeMux`
pattern routing, same style as `internal/gateway/routes.go`.

### Route registration

Registered from `internal/gateway/routes.go` alongside the existing
skill/approval/session routes:

```go
// internal/gateway/routes.go — added in the /v1/* block
mux.HandleFunc("GET /v1/recovery/list",              s.handleRecoveryList)
mux.HandleFunc("GET /v1/recovery/list/{taskID}",     s.handleRecoveryListByTask)
mux.HandleFunc("POST /v1/recovery/undo/{receiptID}", s.handleRecoveryUndo)
mux.HandleFunc("GET /v1/recovery/diff/{receiptID}",  s.handleRecoveryDiff)
```

The handlers themselves live in `internal/recovery/endpoints.go` with
thin shims in `gateway/` that resolve `s.journal` / `s.reverser` /
`s.snapshotter` and delegate.

### Handler contracts

```go
// GET /v1/recovery/list?sessionID=<id>&limit=50&offset=0
// Returns []ReceiptDTO newest-first. Two swim-lanes: reversible + audit-only.
func (h *Handler) List(w http.ResponseWriter, r *http.Request)

// GET /v1/recovery/list/{taskID}?limit=50&offset=0
// Same but scoped to one task.
func (h *Handler) ListByTask(w http.ResponseWriter, r *http.Request)

// POST /v1/recovery/undo/{receiptID}
// Body: {"confirm": true} (required to avoid double-click fat-finger undos)
// Returns: {"applied": bool, "newStatus": "…", "details": "…"}
// For KindShell receipts: returns 202 Accepted with a new approval_id
//   pointing at the inverse-command approval the user must confirm.
func (h *Handler) Undo(w http.ResponseWriter, r *http.Request)

// GET /v1/recovery/diff/{receiptID}
// Returns: {"kind":"…","before":"…","after":"…","contentType":"text/plain|json|binary"}
// For binary content, "before" and "after" are SHA-256 + size; UI renders
// "binary, <size> bytes, hash <abbr>" instead of showing bytes.
func (h *Handler) Diff(w http.ResponseWriter, r *http.Request)
```

### DTO shape

```go
type ReceiptDTO struct {
    ID              string         `json:"id"`
    TaskID          string         `json:"taskId"`
    Kind            ReceiptKind    `json:"kind"`
    SnapshotTier    SnapshotTier   `json:"snapshotTier"`
    ReversalStatus  ReversalStatus `json:"reversalStatus"`
    RedactedPayload string         `json:"redactedPayload"` // already HMAC-masked
    SaaSDeepLink    string         `json:"saasDeepLink,omitempty"`
    CreatedAt       int64          `json:"createdAt"`
}
```

### Error handling

Uses `writeAPIError` (the unified envelope from `internal/gateway/routes.go`):

| Error | HTTP | Code |
|-------|------|------|
| `ErrReceiptNotFound`       | 404 | `not_found` |
| `ErrReceiptAlreadyFinal`   | 409 | `conflict` |
| `ErrNoReverserRegistered`  | 400 | `invalid_request` |
| `ErrNoInverseKnown`        | 409 | `no_inverse_known` (new code, added to codeForStatus) |
| `ErrReversalFailed`        | 500 | `internal_error` |
| missing `confirm: true` body | 400 | `invalid_request` |

### Test boundaries (`endpoints_test.go`)

- `httptest.Server` + in-memory `Journal` backed by `:memory:` SQLite.
- `TestListReturnsNewestFirst`.
- `TestUndoRequiresConfirm` — POST without body → 400.
- `TestUndoShellRoutesThroughApproval` — stub reverser returns approval id,
  asserts 202 Accepted + `X-Approval-ID` header.
- `TestDiffBinarySanitization` — upload binary file, assert response
  contains hash + size, not raw bytes.
- `TestRecoveryListRespectsRedaction` — inject a raw payload containing
  `sk_test_xxx`; assert the DTO never leaks it.

---

## File 5 — `reaper.go`

Shared heartbeat watcher — WS#4 (task runner) reuses this package to
demote `running` tasks to `zombie` when heartbeats stop. Factored here
because WS#2's snapshot purge loop runs on the same cadence.

### Struct fields

```go
type Reaper struct {
    db         *sql.DB
    snap       *Snapshotter
    interval   time.Duration     // default 10s
    staleAfter time.Duration     // default 60s (zombie threshold)
    purgeAge   time.Duration     // default 7d (snapshot retention)
    quit       chan struct{}
    clock      func() time.Time  // monotonic via time.Now for in-process liveness
}

func NewReaper(db *sql.DB, s *Snapshotter) *Reaper
func (r *Reaper) Start(ctx context.Context) error
func (r *Reaper) Stop()
```

### Operations per tick

1. Heartbeat sweep (C3-compliant):
   `UPDATE tasks SET status='zombie' WHERE id=? AND status='running' AND last_heartbeat_at < ?`
   — CAS transition, won't re-transition a task already moved to
   `paused`/`succeeded`/`cancelled`.
2. Snapshot purge: `Snapshotter.Purge(ctx, now - purgeAge)` drops
   snapshot trees older than 7 days whose receipt is final
   (`audit_only` or `reversed_externally` or `irrecoverable`).
3. Orphan snapshot cleanup: remove snapshot subdirs whose receipt_id
   does not exist in `action_receipts` (can happen after DB restore).

### Concurrency contract (C3)

- Runs on its own goroutine; writes only through `storage.DB.heartbeatWriter`,
  which is a SEPARATE `*sql.DB` instance opened against the same WAL-mode
  file (WS#4 introduces this second instance). It is not a channel.
- A single `*sql.DB` with `SetMaxOpenConns(1)` cannot serve both the main
  writer and the heartbeat writer — they would contend for the one connection
  and a long main-writer transaction would starve heartbeats past the 60s
  stale threshold. See spec C3.
- Does NOT share the main writer pool — a stuck main writer cannot starve
  the reaper.
- Uses `time.Now()` monotonic clock for in-process timing; wall-clock
  only for the persisted `last_heartbeat_at` value.

---

## Top-level dependency graph

```
                        ┌──────────────────────────┐
                        │ internal/recovery        │
                        │                          │
                        │  endpoints.go ←──┐       │
                        │    │             │       │
                        │    ▼             │       │
                        │  reversers.go    │       │
                        │    │             │       │
                        │    ▼             │       │
                        │  journal.go  snapshot.go │
                        │    │             │       │
                        │    ▼             ▼       │
                        │  storage.DB   paths.     │
                        │  (existing)   DataDir    │
                        └────┬───────────────┬─────┘
                             │               │
                             ▼               ▼
                    internal/secret      internal/paths
                    (redaction, keyring) (existing)

  gateway/routes.go  →  mounts endpoints.go handlers
  internal/tasks/*   →  calls Journal.Record on every mutating tool call
  internal/tools/fs  →  calls Snapshotter.Capture before writes
  internal/tools/terminal → calls Snapshotter.CaptureMany on destructive commands
```

Every arrow is a one-way import — no cycles.

---

## Non-goals (explicitly out of scope for Phase 12)

- **Restic/Kopia/Borg-grade dedup** — tiered CoW + audit-only degradation
  is sufficient per D4 + risk register.
- **Live browser-profile rollback** — excluded per C2. Browser-form
  receipts are always audit-only regardless of snapshot-tier availability.
- **Cross-session rollback** — receipts are scoped per task. Undoing a
  receipt from an old session is allowed but does NOT attempt to replay
  subsequent related actions.
- **Redaction false-positive unmask in the `/v1/recovery/diff` endpoint** —
  phase12.md open question 4 still owed a decision. Until then, diff
  returns the already-redacted payload only; there is no "reveal original"
  HTTP endpoint. The UI can call `RedactWithMap` in-process and show the
  reveal only via an explicit user gesture in the Desktop app.
- **Batch undo ("undo last 5 actions")** — v0.6.0 ships per-receipt undo
  only. Batch undo is a UI composition of per-receipt calls; it can
  arrive later without schema changes.
- **Network / email / payment side-effect reversal** — all of these are
  audit-only with SaaS deep-links where available. No automated email
  unsend, no automated payment refund; only the recorded receipt and the
  deep-link to the provider's own undo surface.
