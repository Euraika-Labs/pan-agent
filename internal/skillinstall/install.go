// Package skillinstall is the bridge between marketplace.LoadBundle
// and skills.Manager — the WS#13.C install pipeline that takes a
// signed bundle on disk, verifies it, and stages every file into
// the existing reviewer-agent proposal queue.
//
// Why a separate package: marketplace stays skill-package-free
// (so it can be reused from a future "preview manifest" UI flow
// that doesn't touch the proposal queue), and skills stays
// marketplace-free (so the package's surface remains the
// curator/reviewer pipeline that predates marketplace). This
// package is the single place that knows about both ends.
package skillinstall

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/euraika-labs/pan-agent/internal/marketplace"
	"github.com/euraika-labs/pan-agent/internal/skills"
)

// ErrAlreadyExists is returned when a skill with the same
// (category, name) already exists in the active tree. The marketplace
// UI shows this distinct from generic install errors so it can offer
// an "uninstall + reinstall" or "upgrade" flow rather than a generic
// "install failed" message.
var ErrAlreadyExists = errors.New("skillinstall: skill already exists")

// Result reports what Install did. ProposalID is the uuid the
// reviewer agent will see in /v1/skills/proposals; the desktop UI
// surfaces a notification + link to the review queue.
type Result struct {
	ProposalID  string
	SkillName   string
	Category    string
	Description string
	// Publisher is the public-key hex of the signer. The desktop UI
	// shows a fingerprint so the reviewer can compare against the
	// expected publisher before approving.
	Publisher string
	// Supporting reports how many supporting files were staged
	// alongside the SKILL.md.
	Supporting int
}

// Install verifies the bundle at bundleRoot against trustedPubs,
// extracts the skill, calls skills.Manager.CreateProposal to stage
// SKILL.md, then stages every supporting file via WriteProposalFile.
//
// Failure modes (all wrapped sentinels — callers use errors.Is):
//
//	marketplace.ErrSignatureInvalid     — sig missing/malformed/tampered
//	marketplace.ErrUntrustedPublisher   — sig valid, publisher not pinned
//	marketplace.ErrBundleInvalid        — layout / hash / size violation
//	skillinstall.ErrAlreadyExists       — collision with active skill
//
// On success the bundle is left in place — Install does NOT delete
// or move it. Cleanup is the caller's responsibility (the desktop
// downloads bundles to a temp dir + removes them after install).
//
// Pre-condition: mgr is bound to the profile that should receive the
// proposal. trustedPubs follows marketplace.Verify's semantics — nil
// is test-only.
func Install(bundleRoot string, trustedPubs []ed25519.PublicKey, mgr *skills.Manager, sessionID string) (*Result, error) {
	if mgr == nil {
		return nil, fmt.Errorf("skillinstall: Install: nil manager")
	}

	// 1. Verify the bundle (sig + hashes + layout).
	b, err := marketplace.LoadBundle(bundleRoot, trustedPubs)
	if err != nil {
		return nil, err // pass through wrapped sentinel
	}

	// 2. Extract the skill contents (frontmatter + supporting files).
	contents, err := marketplace.ExtractSkill(b)
	if err != nil {
		return nil, err
	}

	// 3. Stage SKILL.md as a proposal. CreateProposal handles guard
	//    scanning, name + category validation, and the active-tree
	//    collision check.
	source := "marketplace:" + marketplace.FingerprintOf(parsePublicKeyOrEmpty(b.Manifest.PublicKeyHex))
	meta, _, err := mgr.CreateProposal(
		contents.Name,
		contents.Category,
		contents.Description,
		contents.Content,
		sessionID,
		source,
	)
	if err != nil {
		// Translate the well-known "already exists" message from
		// CreateProposal so the marketplace UI can offer the
		// reinstall/upgrade flow.
		if isAlreadyExistsErr(err) {
			return nil, fmt.Errorf("%w: %v", ErrAlreadyExists, err)
		}
		return nil, fmt.Errorf("skillinstall: CreateProposal: %w", err)
	}

	// 4. Stage every supporting file into the same proposal dir.
	staged := 0
	for relPath, body := range contents.Supporting {
		if err := mgr.WriteProposalFile(meta.ID, relPath, body); err != nil {
			// Best-effort cleanup is the reviewer-agent's job — we
			// don't unwind the proposal directory because partial
			// state is still useful for diagnosing what failed.
			return nil, fmt.Errorf("skillinstall: stage %q: %w", relPath, err)
		}
		staged++
	}

	return &Result{
		ProposalID:  meta.ID,
		SkillName:   contents.Name,
		Category:    contents.Category,
		Description: contents.Description,
		Publisher:   b.Manifest.PublicKeyHex,
		Supporting:  staged,
	}, nil
}

// parsePublicKeyOrEmpty is a small helper for the source-tag string —
// returns a 32-byte zero key on parse failure rather than propagating
// the error, because the source field is best-effort metadata, not a
// verification surface (Verify already ran in LoadBundle).
func parsePublicKeyOrEmpty(hex string) ed25519.PublicKey {
	pub, err := marketplace.ParsePublicKey(hex)
	if err != nil {
		return make(ed25519.PublicKey, marketplace.PublicKeySize)
	}
	return pub
}

// isAlreadyExistsErr scans the CreateProposal error chain for the
// well-known "already exists" wording. We don't have a dedicated
// sentinel in skills (yet — that's a follow-up), so string-matching
// is the bridge for now. Documented so the day a sentinel lands,
// this scan can be replaced with errors.Is.
func isAlreadyExistsErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, []string{"already exists"})
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if len(n) <= len(s) && stringContains(s, n) {
			return true
		}
	}
	return false
}

func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
