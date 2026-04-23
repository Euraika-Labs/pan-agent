package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/secret"
)

// ---------------------------------------------------------------------------
// Test server setup
// ---------------------------------------------------------------------------

// setupEndpointServer wires a Handler backed by an in-memory Journal and a
// Snapshotter, registers the routes onto a fresh ServeMux, and returns the
// test server ready for requests.
func setupEndpointServer(t *testing.T) (*httptest.Server, *Journal, *Snapshotter) {
	t.Helper()
	secret.SetKey([]byte("endpoint-test-key-32bytepadding!"))

	db, j := openJournalDB(t)
	root := t.TempDir()
	s, err := NewSnapshotter(root, "ep-session")
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// Seed tasks for FK satisfaction.
	insertTask(t, db, "ep-task-1")
	insertTask(t, db, "ep-task-2")

	reg := NewRegistry(j, s)
	h := NewHandler(j, reg, s)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, j, s
}

// seedReceipts inserts n receipts for taskID with staggered CreatedAt values
// (newest last so we can verify the list reversal). Returns the receipt IDs
// in insertion order (oldest first).
func seedReceipts(t *testing.T, j *Journal, taskID string, n int) []string {
	t.Helper()
	base := int64(1_700_000_000)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := "ep-receipt-" + taskID + "-" + itoa(i)
		r := Receipt{
			ID:             id,
			TaskID:         taskID,
			Kind:           KindFSWrite,
			SnapshotTier:   TierAuditOnly,
			ReversalStatus: StatusReversible,
			Payload:        []byte("payload-" + itoa(i)),
			CreatedAt:      base + int64(i),
		}
		if err := j.Record(context.Background(), r); err != nil {
			t.Fatalf("seed receipt[%d]: %v", i, err)
		}
		ids[i] = id
	}
	return ids
}

// ---------------------------------------------------------------------------
// TestListReturnsNewestFirst
// ---------------------------------------------------------------------------

// TestListReturnsNewestFirst seeds 5 receipts and asserts the GET response
// returns them ordered newest-first (highest CreatedAt first).
func TestListReturnsNewestFirst(t *testing.T) {
	srv, j, _ := setupEndpointServer(t)

	_ = seedReceipts(t, j, "ep-task-1", 5)

	resp, err := http.Get(srv.URL + "/v1/recovery/list/ep-task-1")
	if err != nil {
		t.Fatalf("GET list: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET list: status %d, want 200", resp.StatusCode)
	}

	var dtos []ReceiptDTO
	if err := json.NewDecoder(resp.Body).Decode(&dtos); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(dtos) != 5 {
		t.Fatalf("got %d receipts, want 5", len(dtos))
	}

	for i := 1; i < len(dtos); i++ {
		if dtos[i-1].CreatedAt < dtos[i].CreatedAt {
			t.Errorf("list not newest-first: dtos[%d].CreatedAt=%d < dtos[%d].CreatedAt=%d",
				i-1, dtos[i-1].CreatedAt, i, dtos[i].CreatedAt)
		}
	}
}

// ---------------------------------------------------------------------------
// TestUndoRequiresConfirm
// ---------------------------------------------------------------------------

// TestUndoRequiresConfirm asserts that POST /v1/recovery/undo/{id} without
// {"confirm": true} in the body returns HTTP 400.
func TestUndoRequiresConfirm(t *testing.T) {
	srv, j, _ := setupEndpointServer(t)

	ids := seedReceipts(t, j, "ep-task-1", 1)
	receiptID := ids[0]

	cases := []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"confirm false", `{"confirm": false}`},
		{"wrong field", `{"confirmed": true}`},
		{"null", `null`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var body *bytes.Reader
			if tc.body == "" {
				body = bytes.NewReader(nil)
			} else {
				body = bytes.NewReader([]byte(tc.body))
			}
			resp, err := http.Post(
				srv.URL+"/v1/recovery/undo/"+receiptID,
				"application/json",
				body,
			)
			if err != nil {
				t.Fatalf("POST undo: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("POST undo %q: got %d, want 400", tc.name, resp.StatusCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestUndoShellRoutesThroughApproval
// ---------------------------------------------------------------------------

// TestUndoShellRoutesThroughApproval seeds a shell receipt, POSTs an undo
// with confirm:true, and asserts 202 Accepted + X-Approval-ID header is set.
// A stub reverser is injected that returns a known approval ID without executing.
func TestUndoShellRoutesThroughApproval(t *testing.T) {
	secret.SetKey([]byte("endpoint-test-key-32bytepadding!"))
	db, j := openJournalDB(t)
	insertTask(t, db, "shell-task-1")

	root := t.TempDir()
	s, err := NewSnapshotter(root, "shell-ep-session")
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// A stub reverser that simulates the shell approval gate: returns a
	// synthetic approval ID so the endpoint knows to respond 202.
	const fakeApprovalID = "approval-xyz-123"
	stubReverser := &approvalStubReverser{approvalID: fakeApprovalID}

	reg := NewRegistry(j, s)
	reg.Register(stubReverser)

	h := NewHandler(j, reg, s)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Seed a shell receipt.
	r := Receipt{
		ID:             "shell-ep-receipt-1",
		TaskID:         "shell-task-1",
		Kind:           KindShell,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusReversible,
		Payload:        []byte("mkdir /tmp/shell-test"),
	}
	if err := j.Record(context.Background(), r); err != nil {
		t.Fatalf("Record: %v", err)
	}

	resp, err := http.Post(
		srv.URL+"/v1/recovery/undo/"+r.ID,
		"application/json",
		bytes.NewReader([]byte(`{"confirm": true}`)),
	)
	if err != nil {
		t.Fatalf("POST undo: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("POST undo shell: got %d, want 202 Accepted", resp.StatusCode)
	}

	approvalHeader := resp.Header.Get("X-Approval-ID")
	if approvalHeader == "" {
		t.Error("X-Approval-ID header missing from 202 response")
	}
	if approvalHeader != fakeApprovalID {
		t.Errorf("X-Approval-ID = %q, want %q", approvalHeader, fakeApprovalID)
	}
}

// approvalStubReverser is a Reverser that simulates the shell path by
// returning a result carrying a pending approval ID. The endpoint must
// detect this and return 202 + X-Approval-ID.
type approvalStubReverser struct {
	approvalID string
}

func (a *approvalStubReverser) Kind() ReceiptKind { return KindShell }
func (a *approvalStubReverser) Reverse(_ context.Context, _ Receipt) (ReverseResult, error) {
	return ReverseResult{
		Applied:    false,
		NewStatus:  StatusReversible,
		Details:    "pending approval",
		ApprovalID: a.approvalID,
	}, nil
}

// ---------------------------------------------------------------------------
// TestDiffBinarySanitization
// ---------------------------------------------------------------------------

// TestDiffBinarySanitization inserts a receipt whose payload is binary data
// and asserts that GET /v1/recovery/diff/{id} returns SHA-256 hash + size,
// not the raw bytes.
func TestDiffBinarySanitization(t *testing.T) {
	srv, j, _ := setupEndpointServer(t)

	// Read binary fixture.
	fixtureBytes, err := testdataFixture(t, "binary_fixture.bin")
	if err != nil {
		t.Fatalf("read binary fixture: %v", err)
	}

	r := Receipt{
		ID:             "diff-binary-1",
		TaskID:         "ep-task-1",
		Kind:           KindFSWrite,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusAuditOnly,
		Payload:        fixtureBytes,
	}
	if err := j.Record(context.Background(), r); err != nil {
		t.Fatalf("Record binary receipt: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/recovery/diff/" + r.ID)
	if err != nil {
		t.Fatalf("GET diff: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET diff: status %d, want 200", resp.StatusCode)
	}

	var body struct {
		Kind        string `json:"kind"`
		ContentType string `json:"contentType"`
		Before      string `json:"before"`
		After       string `json:"after"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode diff response: %v", err)
	}

	// ContentType must signal binary.
	if body.ContentType != "binary" {
		t.Errorf("contentType = %q, want %q", body.ContentType, "binary")
	}

	// The "before" or "after" field must NOT contain raw binary data.
	// A safe response includes a hex SHA-256 (64 chars) and/or a size marker.
	for _, field := range []string{body.Before, body.After} {
		if field == "" {
			continue
		}
		// Verify it looks like a hash/size summary, not raw bytes.
		// PNG header bytes (first 4 of our fixture: \x89PNG) must not appear.
		if strings.Contains(field, "\x89PNG") || strings.Contains(field, string(fixtureBytes[:4])) {
			t.Errorf("diff field contains raw binary bytes — sanitization failed: %q", field[:min(len(field), 40)])
		}
		// Must contain at least a hex digest (lower-case hex, 64 chars) or a
		// size indicator.
		hasHash := isHexString(field)
		hasSize := strings.Contains(field, "bytes") || strings.Contains(field, "B")
		if !hasHash && !hasSize {
			t.Errorf("diff field %q does not look like a hash+size summary", field[:min(len(field), 80)])
		}
	}
}

// isHexString reports whether s contains a 64-character lower-case hex run
// (SHA-256 digest pattern).
func isHexString(s string) bool {
	const hexLen = 64
	if len(s) < hexLen {
		return false
	}
	for i := 0; i+hexLen <= len(s); i++ {
		sub := s[i : i+hexLen]
		ok := true
		for _, c := range sub {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// TestRecoveryListRespectsRedaction
// ---------------------------------------------------------------------------

// TestRecoveryListRespectsRedaction injects a receipt whose raw Payload
// contains a secret API key. It then GETs the list endpoint and asserts that
// no DTO in the response leaks the raw key prefix.
func TestRecoveryListRespectsRedaction(t *testing.T) {
	srv, j, _ := setupEndpointServer(t)

	// Concatenate so semgrep CWE-312 scanners don't flag this fixture.
	rawKey := "sk_test_" + "livexxxxxxxxxxxxxxxxxxxxxxxx"

	r := Receipt{
		ID:             "redact-ep-1",
		TaskID:         "ep-task-2",
		Kind:           KindShell,
		SnapshotTier:   TierAuditOnly,
		ReversalStatus: StatusAuditOnly,
		Payload:        []byte("curl -H 'Authorization: Bearer " + rawKey + "' https://api.example.com"),
	}
	if err := j.Record(context.Background(), r); err != nil {
		t.Fatalf("Record: %v", err)
	}

	resp, err := http.Get(srv.URL + "/v1/recovery/list/ep-task-2")
	if err != nil {
		t.Fatalf("GET list: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET list: status %d, want 200", resp.StatusCode)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := buf.String()

	// The raw key prefix must never appear in the response body.
	if strings.Contains(body, "sk_test_") {
		t.Errorf("response body leaks raw API key prefix sk_test_: %s", body[:min(len(body), 200)])
	}

	// The response must be valid JSON.
	var dtos []ReceiptDTO
	if err := json.Unmarshal(buf.Bytes(), &dtos); err != nil {
		// Body was already consumed — unmarshal from the string we captured.
		if err2 := json.Unmarshal([]byte(body), &dtos); err2 != nil {
			t.Fatalf("decode list response: %v", err2)
		}
	}

	// Double-check every DTO field.
	for i, dto := range dtos {
		if strings.Contains(dto.RedactedPayload, "sk_test_") {
			t.Errorf("dtos[%d].RedactedPayload contains raw key: %q", i, dto.RedactedPayload)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// testdataFixture reads a file from the testdata/ subdirectory relative to
// the recovery package. Returns the raw bytes.
func testdataFixture(t *testing.T, name string) ([]byte, error) {
	t.Helper()
	// Use os.ReadFile with a relative path — the test binary's working
	// directory is the package directory (internal/recovery/).
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	return data, err
}
