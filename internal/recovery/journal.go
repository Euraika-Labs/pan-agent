// Package recovery implements the action journal, pre-execution filesystem
// snapshots, per-tool reversers, and the /v1/recovery/* HTTP surface.
// Pure Go, no CGo. Uses modernc.org/sqlite via the shared *sql.DB that
// internal/storage owns.
package recovery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/euraika-labs/pan-agent/internal/secret"
	"github.com/google/uuid"
)

// Sentinel errors for the journal. Follow internal/approval/approval.go pattern.
var (
	ErrReceiptNotFound       = errors.New("recovery: receipt not found")
	ErrReceiptAlreadyFinal   = errors.New("recovery: receipt status is final")
	ErrInvalidReceiptKind    = errors.New("recovery: invalid receipt kind")
	ErrUnknownReversalStatus = errors.New("recovery: unknown reversal status")
)

// finalStatuses cannot be moved back to a non-final status.
var finalStatuses = map[ReversalStatus]bool{
	StatusReversedExternally: true,
	StatusIrrecoverable:      true,
}

// ReceiptKind names the category of action logged.
type ReceiptKind string

const (
	KindFSWrite     ReceiptKind = "fs_write"
	KindFSDelete    ReceiptKind = "fs_delete"
	KindShell       ReceiptKind = "shell"
	KindBrowserForm ReceiptKind = "browser_form"
	KindSaaSAPI     ReceiptKind = "saas_api"
)

var validKinds = map[ReceiptKind]bool{
	KindFSWrite:     true,
	KindFSDelete:    true,
	KindShell:       true,
	KindBrowserForm: true,
	KindSaaSAPI:     true,
}

// SnapshotTier records which tier was used to capture the pre-action state.
type SnapshotTier string

const (
	TierCoW       SnapshotTier = "cow"
	TierCopyFS    SnapshotTier = "copyfs"
	TierAuditOnly SnapshotTier = "audit_only"
)

// ReversalStatus is the lifecycle state of a receipt.
type ReversalStatus string

const (
	StatusReversible         ReversalStatus = "reversible"
	StatusAuditOnly          ReversalStatus = "audit_only"
	StatusReversedExternally ReversalStatus = "reversed_externally"
	StatusIrrecoverable      ReversalStatus = "irrecoverable"
)

var validStatuses = map[ReversalStatus]bool{
	StatusReversible:         true,
	StatusAuditOnly:          true,
	StatusReversedExternally: true,
	StatusIrrecoverable:      true,
}

// Receipt is the in-memory shape tools pass to Record.
// Payload holds the RAW pre-redaction bytes; Journal.Record redacts before write.
type Receipt struct {
	ID             string
	TaskID         string
	EventID        *int64
	Kind           ReceiptKind
	SnapshotTier   SnapshotTier
	ReversalStatus ReversalStatus
	Payload        []byte // raw — never persisted directly
	SaaSDeepLink   string
	CreatedAt      int64 // unix seconds; populated by Record when zero
}

// Journal is the writer + reader facade over action_receipts.
// Safe for concurrent use; SQLite's single-writer pool handles serialisation.
type Journal struct {
	db  *sql.DB
	now func() int64 // injectable clock for tests
}

// NewJournal constructs a Journal backed by db. db must already have the
// action_receipts schema applied (storage.Open calls migrate() which does this).
func NewJournal(db *sql.DB) *Journal {
	return &Journal{
		db:  db,
		now: func() int64 { return time.Now().Unix() },
	}
}

// SetClock replaces the clock function. Exposed for tests; not for production use.
func (j *Journal) SetClock(fn func() int64) { j.now = fn }

// Record writes a receipt with the payload HMAC-redacted via internal/secret.
// The raw payload is never persisted — redaction happens before any DB write.
// r.ID is generated if empty. r.CreatedAt is set to now if zero.
func (j *Journal) Record(ctx context.Context, r Receipt) error {
	if !validKinds[r.Kind] {
		return fmt.Errorf("%w: %q", ErrInvalidReceiptKind, r.Kind)
	}
	if !validStatuses[r.ReversalStatus] {
		return fmt.Errorf("%w: %q", ErrUnknownReversalStatus, r.ReversalStatus)
	}
	if r.TaskID == "" {
		return fmt.Errorf("recovery: Record: TaskID is required")
	}
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.SnapshotTier == "" {
		r.SnapshotTier = TierAuditOnly
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = j.now()
	}

	// Redact before any write. Raw payload never touches the DB.
	redacted := secret.RedactBytes(r.Payload)

	_, err := j.db.ExecContext(ctx,
		`INSERT INTO action_receipts
		 (id, task_id, event_id, kind, snapshot_tier, reversal_status,
		  redacted_payload, saas_deep_link, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TaskID, r.EventID, string(r.Kind), string(r.SnapshotTier),
		string(r.ReversalStatus), string(redacted), r.SaaSDeepLink, r.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("recovery: Record: %w", err)
	}
	return nil
}

// UpdateStatus mutates reversal_status only. Returns ErrReceiptNotFound if the
// receipt does not exist and ErrReceiptAlreadyFinal if the current status is a
// terminal state.
func (j *Journal) UpdateStatus(ctx context.Context, id string, status ReversalStatus) error {
	if !validStatuses[status] {
		return fmt.Errorf("%w: %q", ErrUnknownReversalStatus, status)
	}

	r, err := j.Get(ctx, id)
	if err != nil {
		return err
	}
	if finalStatuses[r.ReversalStatus] {
		return fmt.Errorf("%w: current=%s", ErrReceiptAlreadyFinal, r.ReversalStatus)
	}

	res, err := j.db.ExecContext(ctx,
		`UPDATE action_receipts SET reversal_status=? WHERE id=?`, string(status), id,
	)
	if err != nil {
		return fmt.Errorf("recovery: UpdateStatus: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrReceiptNotFound
	}
	return nil
}

// List returns receipts for a task, newest first.
func (j *Journal) List(ctx context.Context, taskID string, limit, offset int) ([]Receipt, error) {
	rows, err := j.db.QueryContext(ctx,
		`SELECT id, task_id, event_id, kind, snapshot_tier, reversal_status,
		        redacted_payload, saas_deep_link, created_at
		 FROM action_receipts
		 WHERE task_id=?
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		taskID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("recovery: List: %w", err)
	}
	defer rows.Close()
	return scanReceipts(rows)
}

// ListSession returns all receipts for all tasks whose task_id is in the given
// session. This feeds the Desktop History screen's two-lane view.
// The session linkage is resolved via the tasks table (tasks.session_id).
func (j *Journal) ListSession(ctx context.Context, sessionID string, limit, offset int) ([]Receipt, error) {
	rows, err := j.db.QueryContext(ctx,
		`SELECT ar.id, ar.task_id, ar.event_id, ar.kind, ar.snapshot_tier,
		        ar.reversal_status, ar.redacted_payload, ar.saas_deep_link, ar.created_at
		 FROM action_receipts ar
		 JOIN tasks t ON t.id = ar.task_id
		 WHERE t.session_id=?
		 ORDER BY ar.created_at DESC
		 LIMIT ? OFFSET ?`,
		sessionID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("recovery: ListSession: %w", err)
	}
	defer rows.Close()
	return scanReceipts(rows)
}

// Get returns a single receipt by id.
func (j *Journal) Get(ctx context.Context, id string) (Receipt, error) {
	row := j.db.QueryRowContext(ctx,
		`SELECT id, task_id, event_id, kind, snapshot_tier, reversal_status,
		        redacted_payload, saas_deep_link, created_at
		 FROM action_receipts WHERE id=?`, id,
	)
	r, err := scanReceipt(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrReceiptNotFound
	}
	return r, err
}

// ---------------------------------------------------------------------------
// scan helpers
// ---------------------------------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReceipt(row rowScanner) (Receipt, error) {
	var r Receipt
	var kind, tier, status, payload, deepLink string
	var eventID sql.NullInt64
	err := row.Scan(
		&r.ID, &r.TaskID, &eventID, &kind, &tier, &status,
		&payload, &deepLink, &r.CreatedAt,
	)
	if err != nil {
		return Receipt{}, err
	}
	if eventID.Valid {
		v := eventID.Int64
		r.EventID = &v
	}
	r.Kind = ReceiptKind(kind)
	r.SnapshotTier = SnapshotTier(tier)
	r.ReversalStatus = ReversalStatus(status)
	// Payload field holds the already-redacted bytes (what is stored).
	r.Payload = []byte(payload)
	r.SaaSDeepLink = deepLink
	return r, nil
}

func scanReceipts(rows *sql.Rows) ([]Receipt, error) {
	var out []Receipt
	for rows.Next() {
		r, err := scanReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
