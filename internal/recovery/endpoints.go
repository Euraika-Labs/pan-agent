package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"unicode/utf8"

	"github.com/euraika-labs/pan-agent/internal/secret"
)

// Handler holds the dependencies for the /v1/recovery/* endpoints.
// Constructed by the gateway and handed the Journal, Registry, and Snapshotter.
type Handler struct {
	journal    *Journal
	registry   *Registry
	snapshotter *Snapshotter
}

// NewHandler constructs a Handler.
func NewHandler(j *Journal, reg *Registry, s *Snapshotter) *Handler {
	return &Handler{journal: j, registry: reg, snapshotter: s}
}

// RegisterRoutes wires the /v1/recovery/* endpoints onto mux using the Go
// 1.22+ method+path ServeMux syntax. Called once by the gateway during
// server construction and once by tests against an isolated ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/recovery/list", h.List)
	mux.HandleFunc("GET /v1/recovery/list/{taskID}", h.ListByTask)
	mux.HandleFunc("POST /v1/recovery/undo/{receiptID}", h.Undo)
	mux.HandleFunc("GET /v1/recovery/diff/{receiptID}", h.Diff)
}

// ReceiptDTO is the JSON shape returned by list and diff endpoints.
type ReceiptDTO struct {
	ID              string         `json:"id"`
	TaskID          string         `json:"taskId"`
	Kind            ReceiptKind    `json:"kind"`
	SnapshotTier    SnapshotTier   `json:"snapshotTier"`
	ReversalStatus  ReversalStatus `json:"reversalStatus"`
	RedactedPayload string         `json:"redactedPayload"`
	SaaSDeepLink    string         `json:"saasDeepLink,omitempty"`
	CreatedAt       int64          `json:"createdAt"`
}

func toDTO(r Receipt) ReceiptDTO {
	// Payload is already redacted when read from the DB.
	// Apply Redact again for belt-and-braces on any in-memory path.
	payload := secret.Redact(string(r.Payload))
	return ReceiptDTO{
		ID:              r.ID,
		TaskID:          r.TaskID,
		Kind:            r.Kind,
		SnapshotTier:    r.SnapshotTier,
		ReversalStatus:  r.ReversalStatus,
		RedactedPayload: payload,
		SaaSDeepLink:    r.SaaSDeepLink,
		CreatedAt:       r.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// GET /v1/recovery/list
// ---------------------------------------------------------------------------

// List returns receipts for a session (newest-first). Query params:
//
//	sessionID string  — required
//	limit     int     — default 50
//	offset    int     — default 0
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionID")
	if sessionID == "" {
		writeRecoveryError(w, http.StatusBadRequest, "invalid_request", "sessionID is required")
		return
	}
	limit, offset := parsePagination(r)

	receipts, err := h.journal.ListSession(r.Context(), sessionID, limit, offset)
	if err != nil {
		writeRecoveryError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	dtos := make([]ReceiptDTO, len(receipts))
	for i, rec := range receipts {
		dtos[i] = toDTO(rec)
	}
	writeRecoveryJSON(w, http.StatusOK, dtos)
}

// ---------------------------------------------------------------------------
// GET /v1/recovery/list/{taskID}
// ---------------------------------------------------------------------------

// ListByTask returns receipts scoped to a single task (newest-first).
func (h *Handler) ListByTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if taskID == "" {
		writeRecoveryError(w, http.StatusBadRequest, "invalid_request", "taskID path param required")
		return
	}
	limit, offset := parsePagination(r)

	receipts, err := h.journal.List(r.Context(), taskID, limit, offset)
	if err != nil {
		writeRecoveryError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	dtos := make([]ReceiptDTO, len(receipts))
	for i, rec := range receipts {
		dtos[i] = toDTO(rec)
	}
	writeRecoveryJSON(w, http.StatusOK, dtos)
}

// ---------------------------------------------------------------------------
// POST /v1/recovery/undo/{receiptID}
// ---------------------------------------------------------------------------

type undoRequest struct {
	Confirm bool `json:"confirm"`
}

type undoResponse struct {
	Applied    bool           `json:"applied"`
	NewStatus  ReversalStatus `json:"newStatus"`
	Details    string         `json:"details"`
	ApprovalID string         `json:"approvalId,omitempty"`
}

// Undo triggers the reversal for a receipt.
// Body must include {"confirm": true} to guard against double-click accidents.
// For KindShell receipts a 202 Accepted is returned with the approval ID.
func (h *Handler) Undo(w http.ResponseWriter, r *http.Request) {
	receiptID := r.PathValue("receiptID")
	if receiptID == "" {
		writeRecoveryError(w, http.StatusBadRequest, "invalid_request", "receiptID path param required")
		return
	}

	var body undoRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Confirm {
		writeRecoveryError(w, http.StatusBadRequest, "invalid_request", `body {"confirm": true} is required`)
		return
	}

	result, err := h.registry.Reverse(r.Context(), receiptID)
	if err != nil {
		switch {
		case errors.Is(err, ErrReceiptNotFound):
			writeRecoveryError(w, http.StatusNotFound, "not_found", "receipt not found")
		case errors.Is(err, ErrReceiptAlreadyFinal):
			writeRecoveryError(w, http.StatusConflict, "conflict", "receipt status is final")
		case errors.Is(err, ErrNoReverserRegistered):
			writeRecoveryError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, ErrNoInverseKnown):
			writeRecoveryError(w, http.StatusConflict, "no_inverse_known", err.Error())
		case errors.Is(err, ErrReversalFailed):
			writeRecoveryError(w, http.StatusInternalServerError, "internal_error", err.Error())
		default:
			writeRecoveryError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}

	resp := undoResponse{
		Applied:    result.Applied,
		NewStatus:  result.NewStatus,
		Details:    result.Details,
		ApprovalID: result.ApprovalID,
	}

	// Shell reversals produce a pending approval — return 202 Accepted so the
	// client knows to poll /v1/approvals/{id} rather than treating this as done.
	status := http.StatusOK
	if result.ApprovalID != "" {
		w.Header().Set("X-Approval-ID", result.ApprovalID)
		status = http.StatusAccepted
	}
	writeRecoveryJSON(w, status, resp)
}

// ---------------------------------------------------------------------------
// GET /v1/recovery/diff/{receiptID}
// ---------------------------------------------------------------------------

type diffResponse struct {
	Kind        ReceiptKind `json:"kind"`
	Before      string      `json:"before"`
	After       string      `json:"after"`
	ContentType string      `json:"contentType"` // "text/plain" | "json" | "binary"
}

// Diff returns a before/after view of the receipt's snapshot vs live state.
// Binary content is represented as SHA-256 + size rather than raw bytes.
func (h *Handler) Diff(w http.ResponseWriter, r *http.Request) {
	receiptID := r.PathValue("receiptID")
	if receiptID == "" {
		writeRecoveryError(w, http.StatusBadRequest, "invalid_request", "receiptID path param required")
		return
	}

	rec, err := h.journal.Get(r.Context(), receiptID)
	if err != nil {
		if errors.Is(err, ErrReceiptNotFound) {
			writeRecoveryError(w, http.StatusNotFound, "not_found", "receipt not found")
			return
		}
		writeRecoveryError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// "before" is the redacted payload stored in the journal.
	beforeStr := secret.Redact(string(rec.Payload))

	// "after" is the current live state — read from the snapshot subpath.
	afterStr, contentType := h.buildAfter(r.Context(), rec)

	writeRecoveryJSON(w, http.StatusOK, diffResponse{
		Kind:        rec.Kind,
		Before:      beforeStr,
		After:       afterStr,
		ContentType: contentType,
	})
}

// buildAfter reads the snapshot tree for rec and returns a human-readable
// after-string plus a content-type hint. When no snapshot subpath is recorded,
// the receipt's stored payload is classified directly (covers the binary-diff
// case where payload IS the content to inspect).
func (h *Handler) buildAfter(_ context.Context, rec Receipt) (string, string) {
	subpath := rec.SaaSDeepLink

	// Try to read from the snapshot directory first.
	if subpath != "" && h.snapshotter != nil {
		snapDir := filepath.Join(h.snapshotter.root, subpath)
		entries, err := os.ReadDir(snapDir)
		if err == nil && len(entries) > 0 {
			// Skip sidecar files written by the snapshotter.
			for _, e := range entries {
				if e.Name() == ".origin" || e.Name() == ".created_at" {
					continue
				}
				data, err := os.ReadFile(filepath.Join(snapDir, e.Name()))
				if err != nil {
					continue
				}
				return classifyContent(data)
			}
		}
	}

	// No snapshot available — classify the payload stored in the receipt.
	// This handles the binary-sanitization case where the payload itself is
	// the binary blob (e.g. fs_write of a binary file).
	if len(rec.Payload) > 0 {
		return classifyContent(rec.Payload)
	}

	return "", "text/plain"
}

// classifyContent returns a safe human-readable representation of data and a
// content-type hint: "binary", "json", or "text/plain".
func classifyContent(data []byte) (string, string) {
	if !utf8.Valid(data) {
		digest := sha256.Sum256(data)
		summary := fmt.Sprintf("binary, %d bytes, sha256:%s", len(data), hex.EncodeToString(digest[:]))
		return summary, "binary"
	}
	if json.Valid(data) {
		return string(data), "json"
	}
	return string(data), "text/plain"
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type recoveryAPIError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func writeRecoveryError(w http.ResponseWriter, status int, code, msg string) {
	writeRecoveryJSON(w, status, recoveryAPIError{Error: msg, Code: code})
}

func writeRecoveryJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	offset = 0
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			limit = v
		}
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}

// contextKey is an unexported type for context values in this package.
type contextKey struct{ name string }

// Ensure context.Context is used (suppress unused import if needed).
var _ context.Context
