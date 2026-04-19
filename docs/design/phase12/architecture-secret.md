# Phase 12 — `internal/secret/` architecture

**Status**: DESIGN (stage 1 of the Phase 12 parallel pipeline — 2026-04-18)
**Spec**: `docs/design/phase12.md` v3 (WS#1 + WS#2 cross-cutting)
**Scope**: shared OS-keyring wrapper + HMAC-deterministic redaction. Pure Go, no CGo.

This package exists because three separate Phase 12 workstreams need the same
two primitives — a platform keyring (WS#1 browser-profile master key, WS#2
redaction-HMAC key, WS#4 per-profile task-runner key) and a deterministic
secret-masking pipeline (D5: applied to `action_receipts.redacted_payload`,
`tasks.plan_json`, `task_events.payload_json`). Putting them in one package
keeps dependency ownership obvious — every caller imports `internal/secret/`
and nothing else.

## Package layout

```
internal/secret/
  keyring.go              — platform-dispatch wrapper + exported surface
  keyring_windows.go      — wincred (DPAPI) backend
  keyring_darwin.go       — /usr/bin/security shell-out backend
  keyring_linux.go        — Secret Service over godbus/dbus/v5 backend
  keyring_stub.go         — unsupported-platform stub (returns ErrUnsupportedPlatform)
  keyring_test.go         — platform-agnostic table-driven behaviour tests
  keyring_roundtrip_test.go — build-tagged roundtrip test, skipped in CI without keyring

  redaction.go            — classifiers + Redact() / RedactWithMap() API
  redaction_patterns.go   — Presidio-ported regex table (package-internal)
  redaction_test.go       — golden-file determinism + per-category corpus tests
```

The `keyring*` split mirrors `internal/tools/` cross-platform pattern
(`keyboard_common.go` + `keyboard_{windows,darwin,linux,stub}.go`).
`redaction*.go` lives with the platform code but compiles on every platform —
redaction does not need OS facilities.

## File 1 — `keyring.go`

Platform dispatch wrapper. The exported surface is three functions and three
sentinel errors; platform files provide the unexported backend.

### Exported API

```go
// Set writes value under the pan-agent service namespace with the given key.
// Overwrites any existing value. key must match keyNameRe.
func Set(key, value string) error

// Get reads the value previously stored under key.
// Returns ErrNotFound if the key does not exist.
func Get(key string) (string, error)

// Delete removes the key from the platform keyring.
// Returns ErrNotFound if the key did not exist; delete of a missing key is NOT
// an error for callers that want idempotent cleanup (see the IsNotFound helper).
func Delete(key string) error

// IsNotFound reports whether err (or any wrapped cause) is ErrNotFound.
// Cheap sugar so callers can write `if secret.IsNotFound(err) { … }`
// instead of errors.Is at every site.
func IsNotFound(err error) bool
```

### Sentinel errors

Follows `internal/approval/approval.go` convention (`errors.New` + top-level
`var`). Callers compare with `errors.Is`.

```go
var (
    ErrNotFound             = errors.New("secret: key not found")
    ErrUnsupportedPlatform  = errors.New("secret: keyring not supported on this platform")
    ErrKeyringUnavailable   = errors.New("secret: keyring daemon unavailable")
    ErrInvalidKey           = errors.New("secret: invalid key name")
)
```

`ErrKeyringUnavailable` is distinct from `ErrUnsupportedPlatform` because
"Linux without a running Secret Service daemon" is a recoverable runtime
condition (user can start `gnome-keyring-daemon`), whereas "compiled on
GOOS=openbsd" is a permanent build-time fact. Setup wizard surfaces these
with different copy.

### Constants + validation

```go
const (
    // serviceName is the keyring namespace all pan-agent secrets live under.
    // Windows: target name prefix. macOS: -s argument to `security`.
    // Linux: attribute {"service": serviceName} on Secret Service items.
    serviceName = "pan-agent"
)

// keyNameRe restricts key names to alphanumeric + dash + underscore + dot,
// 1–128 chars. Matches the paths.profileNameRe posture: conservative
// allowlist over blocklist. Prevents shell-argument injection through the
// macOS `security` CLI shell-out and DBus attribute confusion on Linux.
var keyNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
```

Each exported function calls `validateKey(key)` first and returns
`ErrInvalidKey` on mismatch. The regex is a hard requirement on macOS
(shell-out) and a belt-and-braces guard on Windows/Linux.

### Struct fields

None. `keyring.go` is stateless; process-global.

### Dependency graph

- Imports: `errors`, `regexp`.
- Calls unexported platform symbols: `setPlatform(key, value) error`,
  `getPlatform(key) (string, error)`, `deletePlatform(key) error`.
- Zero test-helper state — the platform backend does all the work.

### Test boundaries (`keyring_test.go`)

Platform-agnostic behaviours only:

- `validateKey` table — empty, leading dash, >128 chars, unicode fullwidth,
  path traversal (`../foo`), shell metachars (`;`, `` ` ``, `$(…)`), each
  rejected with `ErrInvalidKey`.
- `IsNotFound` wraps and unwraps correctly through `fmt.Errorf("%w", …)`.
- Smoke test calls `setPlatform` indirection pointing to an in-memory fake
  injected via an internal `setBackend(backend)` hook behind `//go:build
  test`-style tag (avoids compiling the real backend in tests).

The roundtrip test in `keyring_roundtrip_test.go` has build tag
`//go:build keyring_live` — CI runs it only on runners with a live keyring
(macOS runners get the login keychain, Windows gets Credential Manager;
Linux runners need dbus-launch + `gnome-keyring-daemon --start --components=secrets`).
Locally, `go test ./internal/secret/... -tags=keyring_live`.

## File 2 — `keyring_windows.go`

```go
//go:build windows

package secret

import (
    "errors"

    "github.com/danieljoos/wincred"
)
```

### Backend functions

```go
func setPlatform(key, value string) error
func getPlatform(key string) (string, error)
func deletePlatform(key string) error
```

### Implementation notes

- Target name = `serviceName + ":" + key` (e.g. `pan-agent:browser-profile-key`)
  so the Windows Credential Manager UI groups pan-agent entries together.
- `wincred.NewGenericCredential(target)` + `cred.Write()` for Set. Leave
  `cred.Persist` at its default (`wincred.PersistLocalMachine`) — user-scoped
  via DPAPI (tied to user SID) and survives reboot. `wincred.PersistEnterprise`
  would permit roaming on domain-joined machines; skip for predictability since
  pan-agent is a per-user desktop agent. `wincred.PersistSession` is too
  ephemeral (lost at logoff).
- `wincred.GetGenericCredential(target)` returns `nil, syscall.Errno(1168)`
  (ERROR_NOT_FOUND) — map to `ErrNotFound`.
- `cred.CredentialBlob` is `[]byte`; store value as UTF-8 bytes; Get returns
  `string(cred.CredentialBlob)`.
- `CRED_MAX_CREDENTIAL_BLOB_SIZE` is 2560 bytes. HMAC-SHA256 or AES-256 keys
  (32 bytes each) fit trivially. If a caller stores a serialized struct, Set
  must validate `len(value) ≤ 2560` before invoking wincred (return
  `fmt.Errorf("secret: value exceeds 2560-byte wincred blob limit")`).

### Sentinel error mapping table

| wincred error                     | Returned to caller         |
|-----------------------------------|----------------------------|
| `ERROR_NOT_FOUND` (1168)          | `ErrNotFound`              |
| RPC server unavailable (1722)     | `ErrKeyringUnavailable`    |
| any other                         | `fmt.Errorf("secret: windows keyring: %w", err)` |

### Dependency graph

- New module dep: `github.com/danieljoos/wincred` v1.x (MIT, pure Go —
  calls `advapi32.dll` via `syscall`, no CGo). Documented in the v0.5.0
  release notes as required for WS#1.

## File 3 — `keyring_darwin.go`

```go
//go:build darwin

package secret

import (
    "bytes"
    "errors"
    "os/exec"
)
```

### Backend strategy

Shell-out to `/usr/bin/security`. No CGo, no external module, ships with
every macOS. Absolute path is used so `$PATH` poisoning is not a concern.

### Commands

- **Set**: `security add-generic-password -U -a <key> -s pan-agent -w <value>`
  `-U` updates if an item already exists (otherwise returns
  `errSecDuplicateItem`, which we want to collapse into a silent upsert).
- **Get**: `security find-generic-password -a <key> -s pan-agent -w`
  `-w` prints only the password, no decoration.
- **Delete**: `security delete-generic-password -a <key> -s pan-agent`

### Error mapping

```
exit code 44 ("The specified item could not be found in the keychain")
    → ErrNotFound
exit code 45 (user denied access)
    → ErrKeyringUnavailable (with Setup banner pointing at Keychain Access)
exit code 51 (-25308 / errSecAuthFailed)
    → ErrKeyringUnavailable
any other non-zero                 → fmt.Errorf("secret: macOS keychain: %s: %w", stderr, err)
```

### Security hardening

- Pass the value via `-w <value>` **as a separate argv slot** — `exec.Command`
  already does NOT go through a shell, so injection is only possible inside
  an arg. `keyNameRe` guarantees the key is shell-inert; the value can be
  arbitrary bytes and is safe because it is argv[N] not part of a string.
- For large values (>16 KiB) prefer `-w -` (read from stdin) to keep the
  plaintext out of `/proc/*/cmdline`-equivalent (`ps`). Coder: implement
  stdin path, wrap it behind a size threshold, document the threshold.
- Run with `Cmd.Env = append(os.Environ(), "LC_ALL=C")` so exit-code text
  matching (used in a defensive fallback) is locale-independent.

### Dependency graph

- Stdlib only (`os/exec`). No new module deps.

## File 4 — `keyring_linux.go`

```go
//go:build linux

package secret

import (
    "context"
    "errors"
    "time"

    "github.com/godbus/dbus/v5"
    "github.com/godbus/dbus/v5/introspect"
)
```

### Backend strategy

> **TODO (coder)**: evaluate `github.com/ppacher/go-dbus-keyring` and
> `github.com/zalando/go-keyring` against direct godbus before implementing.
> Criteria: (a) MIT or BSD-3-Clause licensed, (b) pure Go (no CGo),
> (c) commit activity in 2026-02 or later, (d) covers our error surface
> (`ServiceUnknown`, `IsLocked`, `Unlock` navigation). If all four criteria
> pass, prefer the library to cut ~200 lines of protocol-level D-Bus wire
> code. If any criterion fails, fall back to the direct implementation below.

Speak the FreeDesktop.org Secret Service API (v1) over the session bus
directly. Implements the same wire protocol that `libsecret` + `secret-tool`
use, but in pure Go. GNOME Keyring and KWallet5 both implement this
interface.

### Key operations

1. Connect to session bus (`dbus.SessionBus()`), look up
   `org.freedesktop.secrets` at path `/org/freedesktop/secrets`.
2. Open the default collection (`/org/freedesktop/secrets/collection/login`).
   If locked, call `Unlock` and walk the `Prompt` object to completion.
3. Encrypt the session + value using the DH-IETF profile
   (`org.freedesktop.Secret.Service.OpenSession("dh-ietf1024-sha256-aes128-cbc-pkcs7", …)`).
   The plaintext secret never traverses the bus; only a wrapped
   `(parameters, value, content_type)` struct.
4. `CreateItem` with attributes `{service: "pan-agent", key: <key>}` to write;
   `SearchItems` with the same attributes to read.

### Sentinel error mapping

| DBus error                                    | Returned to caller        |
|-----------------------------------------------|---------------------------|
| `org.freedesktop.DBus.Error.ServiceUnknown`   | `ErrKeyringUnavailable`   |
| `org.freedesktop.DBus.Error.NoReply`          | `ErrKeyringUnavailable`   |
| `org.freedesktop.Secret.Error.IsLocked`       | retry after `Unlock`; final failure → `ErrKeyringUnavailable` |
| empty `SearchItems` result                    | `ErrNotFound`             |
| user-cancelled prompt during Unlock           | `ErrKeyringUnavailable`   |
| any other                                     | `fmt.Errorf("secret: secret-service: %w", err)` |

### Dependency graph

- New module dep: `github.com/godbus/dbus/v5` v5.x (BSD, pure Go).
  Used nowhere else in the tree; WS#1 adds it. No CGo.

### Test boundaries

- `TestLinuxEncryptsBeforeBus` — unit test constructs a fake Secret Service
  peer in-process and asserts the plaintext value is never present in the
  marshalled DBus call body (only the DH-wrapped ciphertext).
- `TestLinuxUnlockRetry` — fake service returns `IsLocked` on first read,
  expects the client to navigate the `Prompt` → `Completed` signal and
  re-issue the read.

## File 5 — `keyring_stub.go`

```go
//go:build !windows && !darwin && !linux

package secret

func setPlatform(_, _ string) error            { return ErrUnsupportedPlatform }
func getPlatform(_ string) (string, error)     { return "", ErrUnsupportedPlatform }
func deletePlatform(_ string) error            { return ErrUnsupportedPlatform }
```

Matches `internal/tools/keyboard_stub.go` posture: compile cleanly on any
goos/goarch combo so `go build ./...` stays green everywhere, but surface a
loud error at runtime. The Setup wizard branches on `ErrUnsupportedPlatform`
to disable browser-profile persistence cleanly rather than crashing.

## File 6 — `redaction.go`

Deterministic HMAC-SHA256 masking + Presidio-ported regex classifiers.
Every caller (WS#2 receipts, WS#4 plan_json / payload_json) hits this
same entry point so detection rules are defined exactly once.

### Exported API

```go
// Categories enumerates the detector families Redact uses.
type Category string

const (
    CatEmail      Category = "EMAIL"
    CatPhone      Category = "PHONE"
    CatSSN        Category = "SSN"
    CatCreditCard Category = "CC"
    CatAPIKey     Category = "API_KEY"
    CatJWT        Category = "JWT"
    CatAWSKeyID   Category = "AWS_KEY_ID"
    CatBearer     Category = "BEARER_TOKEN"
)

// Redact returns text with every detected secret replaced by a category-tagged
// deterministic token: "<REDACTED:EMAIL:a1b2c3>". Same input → same output
// across calls (cross-receipt correlation is preserved without leaking the
// value). Uses the per-profile key read from the OS keyring on first call.
func Redact(text string) string

// RedactWithMap returns the redacted text PLUS a map from token → original
// plaintext. The map is returned to the caller only; it is never persisted.
// The Desktop UI uses it for the "reveal original" affordance (gated behind
// a user gesture, not auto-shown) — see phase12.md open question 4.
func RedactWithMap(text string) (string, map[string]string)

// RedactBytes is the []byte convenience wrapper. Avoids a round-trip through
// string for the hot path where task_events.payload_json is a large JSON blob.
func RedactBytes(b []byte) []byte

// SetKey forces the HMAC key used by Redact/RedactWithMap. Exported ONLY so
// tests can set a deterministic key; production code uses init() + keyring.
// Package-private in spirit — expose via internal_test.go or a build-tagged
// helper if we want to prevent accidental use.
func SetKey(key []byte)
```

### Struct fields

```go
type redactor struct {
    key       []byte        // HMAC-SHA256 key, from keyring or SetKey
    keyOnce   sync.Once     // lazy-init key from keyring on first Redact call
    keyInitErr error        // if keyring init fails, redactor fails closed:
                            // returns "<REDACTED:ERR>" for every detection and
                            // logs once — better than leaking plaintext
    patterns  []classifier  // ordered list; longest-match-first within a line
}

type classifier struct {
    category Category
    re       *regexp.Regexp
    // negative holds a second regex that, if it matches the candidate, cancels
    // the detection. Mirrors approval/patterns.go NegativeRegex. Used to avoid
    // matching "example@example.com" (reserved docs address) as a real email.
    negative *regexp.Regexp
    // minLen / maxLen bound the span the regex can claim. Defensive against
    // pathological backtracking; also lets us reject 5-digit "phone numbers"
    // that are actually zip codes when context is missing.
    minLen, maxLen int
}
```

A single package-level `redactor` is fine (process-global). The key is read
lazily from `Get("redaction-hmac-key")`; absent on first run, a new
cryptographically-random 32-byte key is minted, written via `Set`, and
cached. Concurrency via `sync.Once`. No per-profile variation in v0.6.0 —
the key is agent-scoped, which is acceptable because the redaction map
never crosses process boundaries.

### Sentinel errors

Redaction does not return an error from the Redact path — it cannot fail in
a way the caller should handle, because the fallback must be "output is
safely redacted even if we cannot classify". Internal errors (keyring
init failure, regex compile failure — only at init) use:

```go
var ErrRedactionUnavailable = errors.New("secret: redaction subsystem unavailable")
```

Returned from an exported `Ready() error` function that the Setup wizard
and `pan-agent doctor` call to verify the redaction pipeline is healthy.
Redact itself degrades to `<REDACTED:ERR>` on any single failure and logs
once.

### Token format

```
<REDACTED:CATEGORY:XXXXXX>
```

- `CATEGORY` — one of the `Cat*` constants above (uppercase).
- `XXXXXX` — first 6 hex chars of `HMAC-SHA256(key, category || ":" || plaintext)`.
  Six chars = 24 bits = ~1 in 17M collision chance per category per receipt,
  which is a UX annoyance, not a security problem (the token is not a
  capability). Same value of plaintext always produces the same token for
  cross-action correlation ("step 3 and step 7 used the same API key").

### Dependency graph

- Imports: `crypto/hmac`, `crypto/sha256`, `encoding/hex`, `regexp`, `strings`,
  `sync`.
- Calls `Get`/`Set` from `keyring.go` for the HMAC key.
- Imported by:
  - `internal/recovery/journal.go` (WS#2) — redact `action_receipts.redacted_payload`.
  - `internal/tasks/*` (WS#4) — redact `tasks.plan_json`, `task_events.payload_json`.
  - `internal/gateway/*` — used for `/v1/recovery/list` display formatting.

Redaction has no dependency on any other pan-agent internal package, so its
test suite runs standalone and in parallel with the rest of `go test ./...`.

## File 7 — `redaction_patterns.go`

Package-internal table. Ordering matters — more specific patterns first so
AWS key IDs are not misclassified as generic API keys.

```go
var builtinPatterns = []classifier{
    // --- PII ---
    { category: CatEmail,      re: emailRe,     negative: docsEmailRe, minLen: 6,  maxLen: 254 },
    { category: CatPhone,      re: phoneRe,     minLen: 10, maxLen: 20 },
    { category: CatSSN,        re: ssnRe,       minLen: 9,  maxLen: 11 },
    { category: CatCreditCard, re: ccRe,        negative: testCCRe, minLen: 13, maxLen: 19 },

    // --- Credentials ---
    // Order matters: AWS key IDs match before the generic API_KEY pattern so
    // "AKIA…" lines are classified as AWS_KEY_ID not API_KEY.
    { category: CatAWSKeyID,   re: awsKeyIDRe,  minLen: 20, maxLen: 20 },
    { category: CatJWT,        re: jwtRe,       minLen: 20, maxLen: 4096 },
    { category: CatBearer,     re: bearerRe,    minLen: 20, maxLen: 4096 },
    { category: CatAPIKey,     re: apiKeyRe,    minLen: 20, maxLen: 512 },
}
```

Each regex is defined with a clear comment citing the Presidio source file
(e.g. `presidio-analyzer/presidio_analyzer/predefined_recognizers/email_recognizer.py`)
so the coder can audit against upstream without guesswork. `docsEmailRe`
matches `@example.(com|org|net)` and RFC 2606 reserved TLDs; `testCCRe`
matches Luhn-valid test numbers like `4111111111111111`.

### Test boundaries (`redaction_test.go`)

Table-driven, mirrors `internal/approval/check_test.go` structure:

- `TestRedactDeterministic` — 20+ rows, each `{input, category, wantPrefix}`.
  Asserts that the same input twice in a row produces the exact same token.
  Asserts that different inputs in the same category produce different tokens.
- `TestRedactOrdering` — AWS key ID in a line also containing a raw API key
  phrase must classify AWS first.
- `TestRedactNegative` — docs-address + test-card-number inputs must pass
  through unchanged.
- `TestRedactCorrelation` — the same plaintext in two different input strings
  produces the same token. This is the load-bearing behaviour for UI
  "step 3 and step 7 used the same credential".
- `TestRedactGoldenFile` — 200-line fixture of realistic mixed content
  (log output, JSON API responses, shell transcript with sudo prompts)
  hashed to a stable digest. If the digest moves, the coder must re-review
  patterns and bump a `GOLDEN_VERSION` constant. Prevents silent drift when
  patterns are tweaked.
- `TestRedactBytes` — equivalence with Redact(string) on the same content.

No live-keyring test here — `SetKey([]byte("test-key"))` in `TestMain`
gives deterministic output without touching the OS keyring.

## Top-level dependency graph

```
                        ┌─────────────────────────┐
                        │ internal/secret/keyring │
                        │   keyring.go            │
                        └──────────┬──────────────┘
                                   │  setPlatform / getPlatform / deletePlatform
            ┌──────────────────────┼──────────────────────┐
            │                      │                      │
            ▼                      ▼                      ▼
 keyring_windows.go     keyring_darwin.go      keyring_linux.go
 (wincred)              (/usr/bin/security)    (godbus/dbus/v5)

         + keyring_stub.go  (other GOOS)


                        ┌──────────────────────────┐
                        │ internal/secret/redaction │
                        │   redaction.go            │
                        │   redaction_patterns.go   │
                        └──────────┬────────────────┘
                                   │
                                   ▼
                           keyring.Get/Set
                           (HMAC-SHA256 key)
```

Downstream importers:

- `internal/recovery/journal.go`  → `secret.RedactBytes`
- `internal/recovery/endpoints.go` → `secret.RedactWithMap` (UI reveal path)
- `internal/tasks/*`               → `secret.RedactBytes`
- `internal/gateway/chat.go`       → `secret.Redact` (only for display logs)
- `cmd/pan-agent/doctor.go`        → `secret.Ready()` probe
- `desktop/src-tauri/src/permissions.rs` — does NOT import this package;
  the Setup wizard calls Go via the existing HTTP API.

## Non-goals (explicitly out of scope for Phase 12)

- **LLM prompt-history redaction before hitting the provider** — deferred
  to Phase 13 per phase12.md open question 4 and the risk-acceptance
  register. This package provides the primitive; the prompt-pipeline change
  is separate.
- **Per-profile key rotation** — v0.6.0 uses one agent-scoped HMAC key.
  Rotating would break correlation across the `(pre-rotation, post-rotation)`
  window. Design a rotation ceremony in Phase 13 only if needed.
- **Policy-driven redaction** (user-supplied patterns) — the patterns are
  compiled-in for v0.6.0. The `classifier` struct is shaped to accept
  external additions later without a schema break.
- **Format-preserving encryption** — we are NOT trying to keep the redacted
  output parseable as JSON/YAML/etc. Any downstream consumer that needs
  parseable payloads must inspect the pre-redaction version (which is not
  persisted — by design).
