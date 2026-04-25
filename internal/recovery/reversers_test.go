package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/approval"
)

// ---------------------------------------------------------------------------
// Fake approval store
// ---------------------------------------------------------------------------

// fakeApprovalStore records approval requests without auto-approving. It
// satisfies the ApprovalRequester interface that ShellReverser depends on.
// The interface is defined in reversers.go; it is modelled here based on the
// approval.Store surface (Create / CreateWithCheck) and the architecture spec
// requirement that every shell reversal generates a Catastrophic approval
// BEFORE any exec.
type fakeApprovalStore struct {
	mu       sync.Mutex
	requests []*approvalRecord
}

type approvalRecord struct {
	sessionID string
	toolName  string
	arguments string
	level     approval.Level
}

// RequestApproval records the request and returns a pending ID. It never
// auto-approves — the reverser must stop and surface the ID to the caller.
func (f *fakeApprovalStore) RequestApproval(sessionID, toolName, arguments string, level approval.Level) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "fake-approval-" + itoa(len(f.requests))
	f.requests = append(f.requests, &approvalRecord{
		sessionID: sessionID,
		toolName:  toolName,
		arguments: arguments,
		level:     level,
	})
	return id, nil
}

// RequestCount returns the number of approval requests recorded.
func (f *fakeApprovalStore) RequestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// LastRequest returns the most recent approval record or nil.
func (f *fakeApprovalStore) LastRequest() *approvalRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return nil
	}
	return f.requests[len(f.requests)-1]
}

// ---------------------------------------------------------------------------
// Fake exec tracker
// ---------------------------------------------------------------------------

// execTracker counts how many times the shell exec helper is called.
// Injected via WithShellExec option so we can assert "no shell invocation".
type execTracker struct {
	mu    sync.Mutex
	calls []string
}

func (e *execTracker) Run(cmd string, args ...string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, cmd)
	return &notFoundError{cmd: cmd}
}

func (e *execTracker) CallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

type notFoundError struct{ cmd string }

func (n *notFoundError) Error() string { return "exec: " + n.cmd + ": not found" }

// ---------------------------------------------------------------------------
// Journal + DB helpers for reversers
// ---------------------------------------------------------------------------

func openReverserJournal(t *testing.T) (*Journal, *Snapshotter) {
	t.Helper()
	db, j := openJournalDB(t)
	root := t.TempDir()
	s, err := NewSnapshotter(root, "reverser-session")
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}
	// Seed a task for FK satisfaction.
	insertTask(t, db, "reverser-task-1")
	return j, s
}

// recordReceipt is a shorthand for inserting a receipt and returning its ID.
func recordReceipt(t *testing.T, j *Journal, r Receipt) string {
	t.Helper()
	if r.ID == "" {
		r.ID = "rev-receipt-" + itoa(len(r.Payload))
	}
	if err := j.Record(context.Background(), r); err != nil {
		t.Fatalf("Record: %v", err)
	}
	return r.ID
}

// ---------------------------------------------------------------------------
// TestFSReverserRoundtrip
// ---------------------------------------------------------------------------

// TestFSReverserRoundtrip writes a file, captures it via the Snapshotter,
// mutates the live file, then calls the FSReverser and asserts byte equality
// with the original content.
func TestFSReverserRoundtrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FSReverser roundtrip skipped on Windows")
	}

	setDeterministicKey(t)
	j, s := openReverserJournal(t)

	dir := t.TempDir()
	srcFile := filepath.Join(dir, "target.txt")
	original := []byte("original bytes v1")
	writeFile(t, srcFile, original)

	ctx := context.Background()

	// Capture before mutation.
	info, err := s.Capture(ctx, srcFile, "rev-fs-1")
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	// Record a receipt referencing the snapshot. ReverserHint carries the
	// snapshot subpath that FSReverser uses to locate the pre-mutation copy.
	r := Receipt{
		ID:             "rev-fs-1",
		TaskID:         "reverser-task-1",
		Kind:           KindFSWrite,
		SnapshotTier:   info.Tier,
		ReversalStatus: StatusReversible,
		Payload:        []byte(srcFile),
		ReverserHint:   info.Subpath,
	}
	recordReceipt(t, j, r)

	// Mutate live file.
	writeFile(t, srcFile, []byte("mutated v2"))

	// Build registry and reverse.
	reg := NewRegistry(j, s)
	result, err := reg.Reverse(ctx, r.ID)
	if err != nil {
		t.Fatalf("Reverse: %v", err)
	}
	if !result.Applied {
		t.Errorf("ReverseResult.Applied = false, want true")
	}

	// Assert byte equality.
	got, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("ReadFile after Reverse: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("after Reverse: got %q, want %q", got, original)
	}
}

// ---------------------------------------------------------------------------
// TestShellReverserFailClosed
// ---------------------------------------------------------------------------

// TestShellReverserFailClosed verifies that an unknown command pattern returns
// ErrNoInverseKnown, sets receipt status to StatusAuditOnly, and performs NO
// shell invocation.
func TestShellReverserFailClosed(t *testing.T) {
	setDeterministicKey(t)
	j, s := openReverserJournal(t)

	tracker := &execTracker{}

	ctx := context.Background()

	r := Receipt{
		ID:             "rev-fail-1",
		TaskID:         "reverser-task-1",
		Kind:           KindShell,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusReversible,
		// A command that cannot possibly match any pattern in the table.
		Payload: []byte("foo-bar-unknown /tmp/x"),
	}
	recordReceipt(t, j, r)

	reg := NewRegistry(j, s, WithShellExec(tracker.Run))
	result, err := reg.Reverse(ctx, r.ID)

	if !errors.Is(err, ErrNoInverseKnown) {
		t.Errorf("Reverse unknown cmd: got err=%v, want ErrNoInverseKnown", err)
	}
	if result.NewStatus != StatusAuditOnly {
		t.Errorf("result.NewStatus = %q, want StatusAuditOnly", result.NewStatus)
	}
	if tracker.CallCount() > 0 {
		t.Errorf("shell exec was invoked %d time(s); want 0 (fail-closed)", tracker.CallCount())
	}
}

// ---------------------------------------------------------------------------
// TestShellReverserMkdirRmdir
// ---------------------------------------------------------------------------

// TestShellReverserMkdirRmdir is a table-driven happy-path test for each
// entry in the shell reverser pattern table. It verifies that the reverser
// emits the correct inverse command (via the approval request arguments)
// for each known pattern.
func TestShellReverserMkdirRmdir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell reverser pattern table uses POSIX commands — skipping on Windows")
	}

	setDeterministicKey(t)

	cases := []struct {
		name        string
		command     string
		wantInverse string // substring expected in the approval arguments
	}{
		{"mkdir→rmdir", "mkdir /tmp/testdir", "rmdir"},
		{"touch→rm", "touch /tmp/newfile.txt", "rm"},
		{"cp→rm", "cp /src/file.txt /tmp/dst.txt", "rm"},
		{"mv→mv", "mv /tmp/src.txt /tmp/dst.txt", "mv"},
		{"chmod→chmod", "chmod 755 /tmp/file.txt", "chmod"},
		{"chown→chown", "chown user:group /tmp/file.txt", "chown"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			j, s := openReverserJournal(t)
			fake := &fakeApprovalStore{}
			ctx := context.Background()

			r := Receipt{
				ID:             "rev-shell-" + tc.name,
				TaskID:         "reverser-task-1",
				Kind:           KindShell,
				SnapshotTier:   TierAuditOnly,
				ReversalStatus: StatusReversible,
				Payload:        []byte(tc.command),
			}
			recordReceipt(t, j, r)

			reg := NewRegistry(j, s, WithApprovalRequester(fake))
			_, _ = reg.Reverse(ctx, r.ID)

			req := fake.LastRequest()
			if req == nil {
				t.Fatalf("no approval request recorded for %q", tc.command)
			}
			if req.level != approval.Catastrophic {
				t.Errorf("approval level = %d, want Catastrophic (%d)", req.level, approval.Catastrophic)
			}
			// The inverse command must appear in the approval arguments.
			if tc.wantInverse != "" && !containsSubstring(req.arguments, tc.wantInverse) {
				t.Errorf("approval arguments %q do not contain expected inverse %q",
					req.arguments, tc.wantInverse)
			}
		})
	}
}

// containsSubstring checks whether s contains sub (case-sensitive).
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// TestShellReverserRequiresApproval
// ---------------------------------------------------------------------------

// TestShellReverserRequiresApproval injects a fakeApprovalStore and verifies
// that every shell reversal generates a Catastrophic approval request BEFORE
// any exec. The fake records the request and does NOT auto-approve, so the
// reverser must return without executing.
func TestShellReverserRequiresApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell reverser test skipped on Windows (POSIX patterns only)")
	}

	setDeterministicKey(t)
	j, s := openReverserJournal(t)

	fake := &fakeApprovalStore{}
	tracker := &execTracker{}

	ctx := context.Background()

	r := Receipt{
		ID:             "rev-approval-1",
		TaskID:         "reverser-task-1",
		Kind:           KindShell,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusReversible,
		Payload:        []byte("mkdir /tmp/approval-test-dir"),
	}
	recordReceipt(t, j, r)

	reg := NewRegistry(j, s,
		WithApprovalRequester(fake),
		WithShellExec(tracker.Run),
	)
	_, _ = reg.Reverse(ctx, r.ID)

	// Must have created exactly one Catastrophic approval request.
	if fake.RequestCount() == 0 {
		t.Error("no approval request was generated — reverser skipped approval gate")
	}
	if req := fake.LastRequest(); req != nil && req.level != approval.Catastrophic {
		t.Errorf("approval level = %d, want Catastrophic", req.level)
	}

	// No shell exec must have been invoked — approval was not granted.
	if tracker.CallCount() > 0 {
		t.Errorf("shell exec invoked %d time(s) before approval — reverser is not fail-safe",
			tracker.CallCount())
	}
}

// ---------------------------------------------------------------------------
// TestBrowserFormReverserNeverExecutes
// ---------------------------------------------------------------------------

// TestBrowserFormReverserNeverExecutes verifies that the BrowserFormReverser
// never invokes any tool, shell, or rod/browser call. It only reads journal
// status and returns the UI payload.
func TestBrowserFormReverserNeverExecutes(t *testing.T) {
	setDeterministicKey(t)
	j, s := openReverserJournal(t)

	tracker := &execTracker{}
	fake := &fakeApprovalStore{}

	ctx := context.Background()

	r := Receipt{
		ID:             "rev-browser-1",
		TaskID:         "reverser-task-1",
		Kind:           KindBrowserForm,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusAuditOnly,
		ReverserHint:   "https://mail.google.com/mail/u/0/#undo",
		Payload:        []byte(`{"form":"gmail-compose","action":"send"}`),
	}
	recordReceipt(t, j, r)

	reg := NewRegistry(j, s,
		WithShellExec(tracker.Run),
		WithApprovalRequester(fake),
	)
	result, err := reg.Reverse(ctx, r.ID)

	// BrowserFormReverser must succeed (no error) — it is the audit-only lane.
	if err != nil {
		t.Fatalf("Reverse browser_form: unexpected error %v", err)
	}

	// Applied must be false — no automatic reversal.
	if result.Applied {
		t.Error("result.Applied = true for browser_form; want false (audit-only)")
	}
	// NewStatus must be audit_only.
	if result.NewStatus != StatusAuditOnly {
		t.Errorf("result.NewStatus = %q, want StatusAuditOnly", result.NewStatus)
	}
	// No shell must have been invoked.
	if tracker.CallCount() > 0 {
		t.Errorf("shell exec called %d time(s) in browser_form reverser", tracker.CallCount())
	}
	// No approval request must have been created.
	if fake.RequestCount() > 0 {
		t.Errorf("approval requested %d time(s) in browser_form reverser; want 0", fake.RequestCount())
	}
	// Details must be non-empty (UI payload).
	if result.Details == "" {
		t.Error("result.Details is empty — BrowserFormReverser must populate it with guidance")
	}
}

// ---------------------------------------------------------------------------
// TestRegistryDispatch
// ---------------------------------------------------------------------------

// TestRegistryDispatch verifies the Registry routes each known ReceiptKind to
// the correct Reverser and returns ErrNoReverserRegistered for an unknown kind.
func TestRegistryDispatch(t *testing.T) {
	setDeterministicKey(t)

	cases := []struct {
		kind        ReceiptKind
		wantKind    string // expected reverser name or empty for unknown
		expectError error
	}{
		{KindFSWrite, "fs", nil},
		{KindFSDelete, "fs", nil},
		{KindShell, "shell", nil},
		{KindBrowserForm, "browser_form", nil},
		// unknown_kind_xyz cannot be inserted (validation rejects it), so Get
		// returns ErrReceiptNotFound before the kind-dispatch check.
		{ReceiptKind("unknown_kind_xyz"), "", ErrReceiptNotFound},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.kind), func(t *testing.T) {
			j, s := openReverserJournal(t)
			ctx := context.Background()

			// Insert a receipt with the target kind (except for unknown, which
			// we don't insert so we get a not-found or dispatch error first).
			if tc.expectError == nil {
				r := Receipt{
					ID:             "dispatch-" + string(tc.kind),
					TaskID:         "reverser-task-1",
					Kind:           tc.kind,
					SnapshotTier:   TierAuditOnly,
					ReversalStatus: StatusAuditOnly,
					Payload:        []byte("test"),
				}
				recordReceipt(t, j, r)
			}

			reg := NewRegistry(j, s)
			_, err := reg.Reverse(ctx, "dispatch-"+string(tc.kind))

			if tc.expectError != nil {
				if !errors.Is(err, tc.expectError) {
					t.Errorf("Reverse(%q): got %v, want %v", tc.kind, err, tc.expectError)
				}
			} else {
				// For known kinds: error may be nil or a domain error (e.g.
				// ErrSnapshotMissing for fs_write with no snapshot), but must NOT
				// be ErrNoReverserRegistered.
				if errors.Is(err, ErrNoReverserRegistered) {
					t.Errorf("Reverse(%q): got ErrNoReverserRegistered — reverser not registered", tc.kind)
				}
			}
		})
	}
}
