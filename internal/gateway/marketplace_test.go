package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
	"github.com/euraika-labs/pan-agent/internal/paths"
)

// Phase 13 WS#13.C — marketplace HTTP handler tests. The Install
// pipeline + signing + bundle parser are covered upstream; these
// tests pin the wire-format contract: status codes, JSON shapes,
// trust-set persistence, error code mapping.

const testSkillBody = `---
name: weather-tool
description: Look up the weather at a given location
category: utility
---
# Weather

Use this skill to fetch the current weather.
`

func makeMarketplaceBundle(t *testing.T, kp *marketplace.Keypair) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, marketplace.SkillFilename),
		[]byte(testSkillBody), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	sum := sha256.Sum256([]byte(testSkillBody))
	m := &marketplace.Manifest{
		Schema: marketplace.SchemaSkillV1,
		Name:   "weather-tool", Version: "1.0.0", Author: "alice",
		Description: "test", SignedAt: 1700000000,
		Files: []marketplace.ManifestFile{
			{Path: marketplace.SkillFilename,
				SHA256: hex.EncodeToString(sum[:]),
				Size:   int64(len(testSkillBody))},
		},
	}
	if err := marketplace.Sign(m, kp); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	mb, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(
		filepath.Join(dir, marketplace.ManifestFilename), mb, 0o644,
	); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func setupMarketplaceServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	srv := setupTestServer(t)
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	return srv, mux
}

func pinPublisher(t *testing.T, srv *Server, kp *marketplace.Keypair, name string) {
	t.Helper()
	path := paths.MarketplaceTrustFile(srv.profile)
	ts, _, err := marketplace.LoadTrustSet(path)
	if err != nil {
		t.Fatalf("LoadTrustSet: %v", err)
	}
	if _, added := marketplace.PinPublisher(ts, kp.Public, name, 1700000000); !added {
		t.Fatalf("PinPublisher: not added (already present?)")
	}
	if err := marketplace.SaveTrustSet(path, ts); err != nil {
		t.Fatalf("SaveTrustSet: %v", err)
	}
}

// ---------------------------------------------------------------------------
// /v1/marketplace/install
// ---------------------------------------------------------------------------

func TestMarketplaceInstall_HappyPath(t *testing.T) {
	srv, mux := setupMarketplaceServer(t)
	kp, _ := marketplace.GenerateKeypair()
	pinPublisher(t, srv, kp, "Test Publisher")
	bundle := makeMarketplaceBundle(t, kp)

	body, _ := json.Marshal(marketplaceInstallRequest{
		BundlePath: bundle, SessionID: "sess",
	})
	req := httptest.NewRequest("POST", "/v1/marketplace/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp marketplaceInstallResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SkillName != "weather-tool" {
		t.Errorf("SkillName = %q", resp.SkillName)
	}
	if resp.PublisherFingerprint != kp.Fingerprint() {
		t.Errorf("Fingerprint = %q, want %q", resp.PublisherFingerprint, kp.Fingerprint())
	}
}

func TestMarketplaceInstall_UntrustedPublisher(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	kp, _ := marketplace.GenerateKeypair()
	bundle := makeMarketplaceBundle(t, kp)

	body, _ := json.Marshal(marketplaceInstallRequest{BundlePath: bundle})
	req := httptest.NewRequest("POST", "/v1/marketplace/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
	var apiErr APIError
	_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
	if apiErr.Code != "untrusted_publisher" {
		t.Errorf("code = %q, want untrusted_publisher", apiErr.Code)
	}
}

func TestMarketplaceInstall_BundleNotFound(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	// Path is well-formed (absolute, under temp) and passes the
	// sanitiser's structural validation, but the directory doesn't
	// exist on disk — LoadBundle returns ErrBundleInvalid which
	// surfaces as the bundle_invalid code.
	body, _ := json.Marshal(marketplaceInstallRequest{
		BundlePath: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	req := httptest.NewRequest("POST", "/v1/marketplace/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var apiErr APIError
	_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
	if apiErr.Code != "bundle_invalid" {
		t.Errorf("code = %q, want bundle_invalid", apiErr.Code)
	}
}

// TestMarketplaceInstall_RelativePath verifies the absolute-path
// requirement of sanitiseBundlePath.
func TestMarketplaceInstall_RelativePath(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	body, _ := json.Marshal(marketplaceInstallRequest{BundlePath: "relative/path"})
	req := httptest.NewRequest("POST", "/v1/marketplace/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var apiErr APIError
	_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
	if apiErr.Code != "invalid_request" {
		t.Errorf("code = %q, want invalid_request", apiErr.Code)
	}
}

// TestMarketplaceInstall_OutsideAllowlist confirms a path under /etc
// (or another non-allowlisted parent) is rejected.
func TestMarketplaceInstall_OutsideAllowlist(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	body, _ := json.Marshal(marketplaceInstallRequest{BundlePath: "/etc"})
	req := httptest.NewRequest("POST", "/v1/marketplace/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestMarketplaceInstall_InvalidJSON(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	req := httptest.NewRequest("POST", "/v1/marketplace/install",
		bytes.NewReader([]byte("not json {")))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestMarketplaceInstall_MissingBundlePath(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	body, _ := json.Marshal(marketplaceInstallRequest{})
	req := httptest.NewRequest("POST", "/v1/marketplace/install", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /v1/marketplace/trusted
// ---------------------------------------------------------------------------

func TestMarketplaceTrustList_Empty(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	req := httptest.NewRequest("GET", "/v1/marketplace/trusted", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp marketplaceTrustListResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Publishers) != 0 {
		t.Errorf("expected 0 publishers on fresh server, got %d", len(resp.Publishers))
	}
}

func TestMarketplaceTrustList_AfterPin(t *testing.T) {
	srv, mux := setupMarketplaceServer(t)
	kp, _ := marketplace.GenerateKeypair()
	pinPublisher(t, srv, kp, "Pinned One")

	req := httptest.NewRequest("GET", "/v1/marketplace/trusted", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp marketplaceTrustListResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Publishers) != 1 {
		t.Fatalf("expected 1 publisher, got %d", len(resp.Publishers))
	}
	if resp.Publishers[0].Fingerprint != kp.Fingerprint() {
		t.Errorf("Fingerprint = %q, want %q",
			resp.Publishers[0].Fingerprint, kp.Fingerprint())
	}
	if resp.Publishers[0].Name != "Pinned One" {
		t.Errorf("Name = %q", resp.Publishers[0].Name)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/marketplace/trusted
// ---------------------------------------------------------------------------

func TestMarketplacePin_NewEntry(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	kp, _ := marketplace.GenerateKeypair()

	body, _ := json.Marshal(marketplacePinRequest{
		PublicKey: kp.PublicKeyHex(), Name: "Alice",
	})
	req := httptest.NewRequest("POST", "/v1/marketplace/trusted", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp marketplacePinResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Pinned {
		t.Error("expected Pinned=true on fresh pin")
	}
	if resp.Publisher.Fingerprint != kp.Fingerprint() {
		t.Errorf("Fingerprint = %q", resp.Publisher.Fingerprint)
	}
}

func TestMarketplacePin_Idempotent(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	kp, _ := marketplace.GenerateKeypair()
	body, _ := json.Marshal(marketplacePinRequest{PublicKey: kp.PublicKeyHex()})

	// First pin.
	mux.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest("POST", "/v1/marketplace/trusted", bytes.NewReader(body)))

	// Second pin: same key.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/marketplace/trusted", bytes.NewReader(body)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp marketplacePinResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Pinned {
		t.Errorf("second pin: expected Pinned=false")
	}
}

func TestMarketplacePin_BadPublicKey(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	body, _ := json.Marshal(marketplacePinRequest{PublicKey: "zznotahexstring"})
	req := httptest.NewRequest("POST", "/v1/marketplace/trusted", bytes.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /v1/marketplace/trusted/{fingerprint}
// ---------------------------------------------------------------------------

func TestMarketplaceUnpin_HappyPath(t *testing.T) {
	srv, mux := setupMarketplaceServer(t)
	kp, _ := marketplace.GenerateKeypair()
	pinPublisher(t, srv, kp, "tmp")

	req := httptest.NewRequest("DELETE",
		"/v1/marketplace/trusted/"+kp.Fingerprint(), nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp marketplaceUnpinResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Removed {
		t.Errorf("expected Removed=true")
	}

	// Verify the trust list is now empty.
	listReq := httptest.NewRequest("GET", "/v1/marketplace/trusted", nil)
	listW := httptest.NewRecorder()
	mux.ServeHTTP(listW, listReq)
	var list marketplaceTrustListResponse
	_ = json.Unmarshal(listW.Body.Bytes(), &list)
	if len(list.Publishers) != 0 {
		t.Errorf("after unpin, list len = %d, want 0", len(list.Publishers))
	}
}

func TestMarketplaceUnpin_NotFound(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	req := httptest.NewRequest("DELETE",
		"/v1/marketplace/trusted/0000000000000000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ---------------------------------------------------------------------------
// End-to-end: pin → install → unpin → install fails again
// ---------------------------------------------------------------------------

func TestMarketplace_PinInstallUnpinFlow(t *testing.T) {
	_, mux := setupMarketplaceServer(t)
	kp, _ := marketplace.GenerateKeypair()
	bundle := makeMarketplaceBundle(t, kp)

	// 1. Install before pin: must fail untrusted.
	installBody, _ := json.Marshal(marketplaceInstallRequest{BundlePath: bundle})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/marketplace/install",
		bytes.NewReader(installBody)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("pre-pin install: status = %d, want 403", w.Code)
	}

	// 2. Pin publisher.
	pinBody, _ := json.Marshal(marketplacePinRequest{
		PublicKey: kp.PublicKeyHex(), Name: "Test",
	})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/marketplace/trusted",
		bytes.NewReader(pinBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("pin: status = %d", w.Code)
	}

	// 3. Install after pin: must succeed.
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/marketplace/install",
		bytes.NewReader(installBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("post-pin install: status = %d, body = %s", w.Code, w.Body.String())
	}

	// 4. Unpin.
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("DELETE",
		"/v1/marketplace/trusted/"+kp.Fingerprint(), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("unpin: status = %d", w.Code)
	}

	// 5. Install after unpin: must fail untrusted again. (Use a
	// DIFFERENT bundle dir because the first install's proposal
	// stays staged + a re-install of the same skill would error
	// "already exists" first, masking the trust check.)
	bundle2 := makeMarketplaceBundle(t, kp)
	installBody2, _ := json.Marshal(marketplaceInstallRequest{BundlePath: bundle2})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/v1/marketplace/install",
		bytes.NewReader(installBody2)))
	if w.Code != http.StatusForbidden {
		t.Errorf("post-unpin install: status = %d, want 403", w.Code)
	}
}
