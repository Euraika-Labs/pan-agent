package gateway

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/skillinstall"
	"github.com/euraika-labs/pan-agent/internal/skills"
)

// Phase 13 WS#13.C — marketplace HTTP endpoints.
//
//   POST   /v1/marketplace/install                 — install a bundle
//   GET    /v1/marketplace/trusted                 — list pinned publishers
//   POST   /v1/marketplace/trusted                 — pin a publisher
//   DELETE /v1/marketplace/trusted/{fingerprint}   — unpin a publisher
//
// The trust set lives at paths.MarketplaceTrustFile(profile) and is
// re-read on every request so a publisher pinned in another tab takes
// effect immediately without a server restart.
//
// Install accepts a local filesystem path (the desktop downloads the
// bundle to a temp dir before posting). Future work: accept an upload
// stream so HTTP-only clients can install without writing to a known
// path. Out of scope here.

// ---------------------------------------------------------------------------
// Request / response shapes
// ---------------------------------------------------------------------------

type marketplaceInstallRequest struct {
	BundlePath string `json:"bundle_path"`
	SessionID  string `json:"session_id,omitempty"`
}

type marketplaceInstallResponse struct {
	ProposalID  string `json:"proposal_id"`
	SkillName   string `json:"skill_name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	// PublisherFingerprint is the 16-hex-char display form derived
	// from the publisher's public key; the desktop UI shows this
	// alongside a "Pin Publisher" button when the install fails
	// with code "untrusted_publisher".
	PublisherFingerprint string `json:"publisher_fingerprint"`
	Supporting           int    `json:"supporting"`
}

type marketplaceTrustEntry struct {
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	Name        string `json:"name,omitempty"`
	AddedAt     int64  `json:"added_at,omitempty"`
}

type marketplaceTrustListResponse struct {
	Publishers []marketplaceTrustEntry `json:"publishers"`
}

type marketplacePinRequest struct {
	PublicKey string `json:"public_key"`
	Name      string `json:"name,omitempty"`
}

type marketplacePinResponse struct {
	Pinned    bool                  `json:"pinned"` // false when already present
	Publisher marketplaceTrustEntry `json:"publisher"`
}

type marketplaceUnpinResponse struct {
	Removed bool `json:"removed"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleMarketplaceInstall reads a bundle from BundlePath, verifies
// it against the trust set, and stages it as a proposal.
//
// BundlePath is user-supplied data that flows into filesystem
// operations (LoadBundle reads manifest.json + every declared file
// under the path). CodeQL flags this as a path-injection sink unless
// the input is sanitised through an allowlist boundary; sanitiseBundlePath
// is that boundary.
func (s *Server) handleMarketplaceInstall(w http.ResponseWriter, r *http.Request) {
	var req marketplaceInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", "invalid JSON body", nil)
		return
	}
	cleanPath, err := sanitiseBundlePath(req.BundlePath, s.profile)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", err.Error(), nil)
		return
	}

	pubs, _, err := s.loadTrustedPublishers()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"trust_set_invalid", err.Error(), nil)
		return
	}

	mgr := skills.NewManager(s.profile)
	res, err := skillinstall.Install(cleanPath, pubs, mgr, req.SessionID)
	if err != nil {
		writeMarketplaceError(w, err)
		return
	}

	pubKey, _ := marketplace.ParsePublicKey(res.Publisher)
	writeJSON(w, http.StatusOK, marketplaceInstallResponse{
		ProposalID:           res.ProposalID,
		SkillName:            res.SkillName,
		Category:             res.Category,
		Description:          res.Description,
		PublisherFingerprint: marketplace.FingerprintOf(pubKey),
		Supporting:           res.Supporting,
	})
}

// handleMarketplaceTrustList returns the set of pinned publishers.
func (s *Server) handleMarketplaceTrustList(w http.ResponseWriter, _ *http.Request) {
	ts, _, err := s.loadTrustedPublishers()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"trust_set_invalid", err.Error(), nil)
		return
	}
	out := marketplaceTrustListResponse{
		Publishers: make([]marketplaceTrustEntry, 0, len(ts)),
	}
	// Re-load the full TrustSet so we get names + addedAt; loadTrustedPublishers
	// returns just the parsed []ed25519.PublicKey for the install path.
	full, _, err := marketplace.LoadTrustSet(paths.MarketplaceTrustFile(s.profile))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"trust_set_invalid", err.Error(), nil)
		return
	}
	for _, p := range full.Publishers {
		out.Publishers = append(out.Publishers, marketplaceTrustEntry{
			Fingerprint: p.Fingerprint,
			PublicKey:   p.PublicKey,
			Name:        p.Name,
			AddedAt:     p.AddedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMarketplacePin adds a publisher to the trust set. Idempotent —
// re-pinning an existing publisher returns pinned=false with the
// existing entry so the desktop UI can confirm without a duplicate.
func (s *Server) handleMarketplacePin(w http.ResponseWriter, r *http.Request) {
	var req marketplacePinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", "invalid JSON body", nil)
		return
	}
	pub, err := marketplace.ParsePublicKey(req.PublicKey)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", "public_key: "+err.Error(), nil)
		return
	}

	path := paths.MarketplaceTrustFile(s.profile)
	ts, _, err := marketplace.LoadTrustSet(path)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"trust_set_invalid", err.Error(), nil)
		return
	}
	entry, added := marketplace.PinPublisher(ts, pub, req.Name, time.Now().Unix())
	if added {
		if err := marketplace.SaveTrustSet(path, ts); err != nil {
			writeAPIError(w, http.StatusInternalServerError,
				"internal_error", err.Error(), nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, marketplacePinResponse{
		Pinned: added,
		Publisher: marketplaceTrustEntry{
			Fingerprint: entry.Fingerprint,
			PublicKey:   entry.PublicKey,
			Name:        entry.Name,
			AddedAt:     entry.AddedAt,
		},
	})
}

// handleMarketplaceUnpin removes a publisher by fingerprint. 404 when
// the fingerprint isn't in the trust set.
func (s *Server) handleMarketplaceUnpin(w http.ResponseWriter, r *http.Request) {
	fp := r.PathValue("fingerprint")
	if fp == "" {
		writeAPIError(w, http.StatusBadRequest,
			"invalid_request", "fingerprint required", nil)
		return
	}
	path := paths.MarketplaceTrustFile(s.profile)
	ts, _, err := marketplace.LoadTrustSet(path)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"trust_set_invalid", err.Error(), nil)
		return
	}
	idx := -1
	for i, p := range ts.Publishers {
		if p.Fingerprint == fp {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeAPIError(w, http.StatusNotFound,
			"not_found", "publisher fingerprint not pinned", nil)
		return
	}
	ts.Publishers = append(ts.Publishers[:idx], ts.Publishers[idx+1:]...)
	if err := marketplace.SaveTrustSet(path, ts); err != nil {
		writeAPIError(w, http.StatusInternalServerError,
			"internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, marketplaceUnpinResponse{Removed: true})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadTrustedPublishers reads the profile's trust-set file and returns
// the parsed []ed25519.PublicKey ready to pass to skillinstall.Install.
// A missing file yields an empty slice (LoadTrustSet's documented
// behaviour) — the install path then rejects every bundle, which is
// the correct fail-closed default for a fresh install.
func (s *Server) loadTrustedPublishers() ([]ed25519.PublicKey, *marketplace.TrustSet, error) {
	path := paths.MarketplaceTrustFile(s.profile)
	ts, pubs, err := marketplace.LoadTrustSet(path)
	if err != nil {
		return nil, nil, err
	}
	return pubs, ts, nil
}

// sanitiseBundlePath validates a user-supplied bundle path and returns
// a cleaned absolute path that's safe to pass to LoadBundle. Three
// failure modes:
//
//  1. Empty input.
//  2. After Clean+Abs, the path doesn't sit inside one of the
//     allowed parent directories. The desktop client downloads
//     bundles to either os.TempDir() (the install-from-marketplace
//     path) or the agent home (the install-from-cache path); paths
//     outside those are rejected.
//  3. The resolved path doesn't exist or isn't a directory.
//
// The allowlist boundary is the load-bearing piece for CodeQL's
// go/path-injection sink — by ensuring every path that reaches
// LoadBundle has been Clean+Abs'd AND prefix-checked against a
// known-safe parent, the data flow is no longer "uncontrolled".
//
// The "agent home" prefix is profile-aware so a user pre-staging
// bundles under their per-profile agent home (~/.local/share/
// pan-agent/<profile>/bundles/, etc.) works without surfacing the
// directory layout.
func sanitiseBundlePath(raw string, profile string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("bundle_path required")
	}
	cleaned := filepath.Clean(raw)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("bundle_path must be absolute, got %q", raw)
	}
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("bundle_path: %w", err)
	}
	// Disallow any residual ".." after Clean (defence-in-depth — Clean
	// should have collapsed them but a symlinked parent could re-introduce).
	for _, seg := range strings.Split(abs, string(filepath.Separator)) {
		if seg == ".." {
			return "", fmt.Errorf("bundle_path must not contain traversal segments")
		}
	}

	// Resolve the actual on-disk location AFTER symlink expansion so
	// a symlink under /tmp pointing at /etc can't sneak past the
	// prefix check.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("bundle_path: stat: %w", err)
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("bundle_path: stat: %w", err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("bundle_path must be a directory")
	}

	// Allowed parent prefixes — every accepted bundle root must sit
	// under one of these. EvalSymlinks the parents too so the
	// comparison is realpath-vs-realpath.
	tmpReal, _ := filepath.EvalSymlinks(os.TempDir())
	homeReal, _ := filepath.EvalSymlinks(paths.AgentHome())
	profileReal, _ := filepath.EvalSymlinks(paths.ProfileHome(profile))
	allowed := []string{tmpReal, homeReal, profileReal}
	for _, prefix := range allowed {
		if prefix == "" {
			continue
		}
		if pathHasPrefix(resolved, prefix) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("bundle_path %q must be under temp dir or agent home", raw)
}

// pathHasPrefix reports whether path is exactly prefix or a child
// of prefix. Pure-prefix string comparison is wrong because
// "/tmp-evil/bundle" would falsely match prefix "/tmp"; we require
// either equality or a separator at the boundary.
func pathHasPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(prefix, sep) {
		prefix += sep
	}
	return strings.HasPrefix(path, prefix)
}

// writeMarketplaceError translates the four well-known install
// errors into stable status + code pairs so the desktop UI can
// distinguish them cleanly.
func writeMarketplaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplace.ErrSignatureInvalid):
		writeAPIError(w, http.StatusBadRequest,
			"signature_invalid", err.Error(), nil)
	case errors.Is(err, marketplace.ErrUntrustedPublisher):
		writeAPIError(w, http.StatusForbidden,
			"untrusted_publisher", err.Error(), nil)
	case errors.Is(err, marketplace.ErrBundleInvalid):
		writeAPIError(w, http.StatusBadRequest,
			"bundle_invalid", err.Error(), nil)
	case errors.Is(err, skillinstall.ErrAlreadyExists):
		writeAPIError(w, http.StatusConflict,
			"already_exists", err.Error(), nil)
	default:
		writeAPIError(w, http.StatusInternalServerError,
			"internal_error", err.Error(), nil)
	}
}
