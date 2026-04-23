package recovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/euraika-labs/pan-agent/internal/approval"
)

// Sentinel errors for the reverser registry.
var (
	ErrNoReverserRegistered = errors.New("recovery: no reverser for receipt kind")
	ErrReversalFailed       = errors.New("recovery: reversal failed")
	ErrNoInverseKnown       = errors.New("recovery: no inverse known for command")
	ErrSnapshotMissing      = errors.New("recovery: snapshot missing or corrupted")
)

// ---------------------------------------------------------------------------
// ApprovalRequester — interface so tests can inject a fake without importing
// the real approval.Store (which blocks on goroutines).
// ---------------------------------------------------------------------------

// ApprovalRequester is the subset of approval.Store that ShellReverser needs.
// The real implementation (realApprovalRequester) wraps approval.Store.
// Tests inject fakeApprovalStore which satisfies this interface.
type ApprovalRequester interface {
	RequestApproval(sessionID, toolName, arguments string, level approval.Level) (string, error)
}

// ---------------------------------------------------------------------------
// ShellExec — injectable shell execution function for tests
// ---------------------------------------------------------------------------

// ShellExecFn is the signature for shell execution used by ShellReverser.
// Tests inject a tracker; production code is never wired — ShellReverser
// only queues an approval and never directly executes.
type ShellExecFn func(cmd string, args ...string) error

// ---------------------------------------------------------------------------
// RegistryOption — functional options for NewRegistry
// ---------------------------------------------------------------------------

// RegistryOption is a functional option for NewRegistry.
type RegistryOption func(*registryConfig)

type registryConfig struct {
	approvalRequester ApprovalRequester
	shellExec         ShellExecFn
}

// WithApprovalRequester injects an ApprovalRequester. Used by tests to capture
// approval calls without touching the real approval.Store.
func WithApprovalRequester(r ApprovalRequester) RegistryOption {
	return func(c *registryConfig) { c.approvalRequester = r }
}

// WithShellExec injects a shell execution function. Used by tests to assert
// that ShellReverser never directly executes the inverse command.
func WithShellExec(fn ShellExecFn) RegistryOption {
	return func(c *registryConfig) { c.shellExec = fn }
}

// ---------------------------------------------------------------------------
// Reverser interface
// ---------------------------------------------------------------------------

// Reverser is the contract each tool family implements.
type Reverser interface {
	Kind() ReceiptKind
	Reverse(ctx context.Context, r Receipt) (ReverseResult, error)
}

// ReverseResult is returned by Reverse.
type ReverseResult struct {
	Applied    bool
	NewStatus  ReversalStatus
	Details    string
	ApprovalID string // non-empty for shell reversals awaiting user confirmation
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Registry dispatches by Receipt.Kind.
type Registry struct {
	mu     sync.RWMutex
	byKind map[ReceiptKind]Reverser
	j      *Journal
}

// NewRegistry constructs a Registry pre-loaded with FSReverser, ShellReverser,
// and BrowserFormReverser. opts allow tests to inject fakes.
func NewRegistry(j *Journal, s *Snapshotter, opts ...RegistryOption) *Registry {
	cfg := &registryConfig{}
	for _, o := range opts {
		o(cfg)
	}

	r := &Registry{
		byKind: make(map[ReceiptKind]Reverser),
		j:      j,
	}

	fs := &FSReverser{j: j, snap: s}
	r.registerKind(fs, KindFSWrite)
	r.registerKind(fs, KindFSDelete)
	r.Register(&ShellReverser{
		j:         j,
		snap:      s,
		fs:        fs,
		approvals: cfg.approvalRequester,
		shellExec: cfg.shellExec,
	})
	r.Register(&BrowserFormReverser{j: j})
	return r
}

func (r *Registry) registerKind(rev Reverser, kind ReceiptKind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKind[kind] = rev
}

// Register adds or replaces the reverser for rev.Kind().
func (r *Registry) Register(rev Reverser) {
	r.registerKind(rev, rev.Kind())
}

// Reverse fetches the receipt, finds the reverser, applies it, and updates
// the journal status. Status update ignores ErrReceiptAlreadyFinal (idempotent).
func (r *Registry) Reverse(ctx context.Context, receiptID string) (ReverseResult, error) {
	rec, err := r.j.Get(ctx, receiptID)
	if err != nil {
		return ReverseResult{}, err
	}

	r.mu.RLock()
	rev, ok := r.byKind[rec.Kind]
	r.mu.RUnlock()
	if !ok {
		return ReverseResult{}, fmt.Errorf("%w: %s", ErrNoReverserRegistered, rec.Kind)
	}

	result, err := rev.Reverse(ctx, rec)
	if err != nil {
		return result, err
	}

	if updateErr := r.j.UpdateStatus(ctx, receiptID, result.NewStatus); updateErr != nil {
		if !errors.Is(updateErr, ErrReceiptAlreadyFinal) {
			return result, fmt.Errorf("recovery: update status after reverse: %w", updateErr)
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// FSReverser
// ---------------------------------------------------------------------------

// FSReverser handles KindFSWrite and KindFSDelete receipts by restoring from
// the pre-execution snapshot. Fail-closed: missing snapshot → ErrReversalFailed.
type FSReverser struct {
	j    *Journal
	snap *Snapshotter
}

func (f *FSReverser) Kind() ReceiptKind { return KindFSWrite }

func (f *FSReverser) Reverse(ctx context.Context, r Receipt) (ReverseResult, error) {
	// SaaSDeepLink carries the snapshot subpath (set at capture time by the tool).
	subpath := r.SaaSDeepLink
	if subpath == "" {
		return ReverseResult{
			Applied:   false,
			NewStatus: StatusIrrecoverable,
			Details:   "no snapshot subpath on receipt",
		}, ErrSnapshotMissing
	}

	info := SnapshotInfo{
		Tier:      r.SnapshotTier,
		ReceiptID: r.ID,
		Subpath:   subpath,
	}

	if err := f.snap.Restore(ctx, info); err != nil {
		return ReverseResult{
			Applied:   false,
			NewStatus: StatusIrrecoverable,
			Details:   fmt.Sprintf("restore failed: %v", err),
		}, fmt.Errorf("%w: %v", ErrReversalFailed, err)
	}

	return ReverseResult{
		Applied:   true,
		NewStatus: StatusReversedExternally,
		Details:   "snapshot restored",
	}, nil
}

// ---------------------------------------------------------------------------
// ShellReverser
// ---------------------------------------------------------------------------

// ShellReverser handles KindShell receipts via the compiled-in pattern table.
// Fail-closed: unknown commands → ErrNoInverseKnown → StatusAuditOnly.
//
// CRITICAL: the inverse command is NEVER executed directly. It is submitted
// as a Catastrophic approval request. Only after user confirmation via
// /v1/approvals/{id} does any shell execution happen — preventing injection
// via spoofed receipts.
type ShellReverser struct {
	j         *Journal
	snap      *Snapshotter
	fs        *FSReverser
	approvals ApprovalRequester // nil = no approval backend wired (safe: fail-closed)
	shellExec ShellExecFn       // nil = production path (no direct exec)
}

func (sh *ShellReverser) Kind() ReceiptKind { return KindShell }

func (sh *ShellReverser) Reverse(ctx context.Context, r Receipt) (ReverseResult, error) {
	cmd := strings.TrimSpace(string(r.Payload))

	pat, m, matched := matchShellPattern(cmd)
	if !matched {
		return ReverseResult{
			Applied:   false,
			NewStatus: StatusAuditOnly,
			Details:   "no inverse known for: " + cmd,
		}, ErrNoInverseKnown
	}

	inverseArgv, err := pat.inverse(m)

	switch {
	case err == errDelegateToFS:
		return sh.fs.Reverse(ctx, r)

	case err == errNeedSnapshotMeta:
		// chmod/chown: the exact original mode/uid comes from snapshot metadata
		// which isn't yet resolved. Build a best-effort inverse from the pattern
		// captures (same command with same args as placeholder) so the user sees
		// the intent in the approval queue and can adjust before confirming.
		// This still routes through the Catastrophic approval gate — fail-safe.
		inverseCmd := cmd // placeholder — human reviews before approving
		var approvalID string
		if sh.approvals != nil {
			id, reqErr := sh.approvals.RequestApproval(
				"", "shell_inverse", inverseCmd, approval.Catastrophic,
			)
			if reqErr != nil {
				return ReverseResult{
					Applied:   false,
					NewStatus: StatusAuditOnly,
					Details:   fmt.Sprintf("failed to submit approval: %v", reqErr),
				}, fmt.Errorf("recovery: ShellReverser RequestApproval: %w", reqErr)
			}
			approvalID = id
		}
		return ReverseResult{
			Applied:    false,
			NewStatus:  StatusAuditOnly,
			Details:    fmt.Sprintf("approval_id=%s inverse requires snapshot metadata; manual review needed for: %s", approvalID, cmd),
			ApprovalID: approvalID,
		}, nil

	case err != nil:
		return ReverseResult{
			Applied:   false,
			NewStatus: StatusAuditOnly,
			Details:   fmt.Sprintf("inverse build error for %q: %v", cmd, err),
		}, ErrNoInverseKnown
	}

	// Build and submit a Catastrophic approval for the inverse command.
	// The inverse is only executed after user confirmation.
	inverseCmd := strings.Join(inverseArgv, " ")

	var approvalID string
	if sh.approvals != nil {
		id, reqErr := sh.approvals.RequestApproval(
			"", "shell_inverse", inverseCmd, approval.Catastrophic,
		)
		if reqErr != nil {
			return ReverseResult{
				Applied:   false,
				NewStatus: StatusAuditOnly,
				Details:   fmt.Sprintf("failed to submit approval: %v", reqErr),
			}, fmt.Errorf("recovery: ShellReverser RequestApproval: %w", reqErr)
		}
		approvalID = id
	}

	return ReverseResult{
		Applied:    false,
		NewStatus:  StatusAuditOnly,
		Details:    fmt.Sprintf("approval_id=%s inverse=%q", approvalID, inverseCmd),
		ApprovalID: approvalID,
	}, nil
}

// ---------------------------------------------------------------------------
// BrowserFormReverser
// ---------------------------------------------------------------------------

// BrowserFormReverser handles KindBrowserForm receipts.
// AUDIT-ONLY: never executes any tool, shell command, or browser automation.
type BrowserFormReverser struct {
	j *Journal
}

func (b *BrowserFormReverser) Kind() ReceiptKind { return KindBrowserForm }

func (b *BrowserFormReverser) Reverse(_ context.Context, r Receipt) (ReverseResult, error) {
	details := "Manual reversal required"
	if r.SaaSDeepLink != "" {
		details += ": " + r.SaaSDeepLink
	}
	return ReverseResult{
		Applied:   false,
		NewStatus: StatusAuditOnly,
		Details:   details,
	}, nil
}

// ---------------------------------------------------------------------------
// snapshot metadata helper
// ---------------------------------------------------------------------------
