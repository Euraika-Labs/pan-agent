package gateway

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/euraika-labs/pan-agent/internal/skills"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// =============================================================================
// Proposal handlers
// =============================================================================

// handleProposalList returns the queue of pending proposals.
func (s *Server) handleProposalList(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	mgr := skills.NewManager(profile)
	props, err := mgr.ListProposals()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if props == nil {
		props = []skills.Proposal{}
	}
	writeJSON(w, http.StatusOK, props)
}

// handleProposalGet returns a single proposal by ID.
func (s *Server) handleProposalGet(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	id := r.PathValue("id")
	mgr := skills.NewManager(profile)
	p, err := mgr.LoadProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleProposalApprove promotes a proposal (with optional refined content).
// Body: {"refined_content": "...", "reviewer_note": "..."}
func (s *Server) handleProposalApprove(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	id := r.PathValue("id")
	var body struct {
		RefinedContent string `json:"refined_content"`
		ReviewerNote   string `json:"reviewer_note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // body is optional

	mgr := skills.NewManager(profile)
	// Re-use the same intent-aware logic the reviewer agent uses.
	p, err := mgr.LoadProposal(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	switch p.Metadata.Intent {
	case skills.IntentArchive, skills.IntentRecategorize:
		if err := mgr.ApplyCuratorIntent(p.Metadata, ""); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		_ = mgr.RejectProposal(id, "applied: "+p.Metadata.Intent+" — "+body.ReviewerNote)
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied", "intent": p.Metadata.Intent})
		return

	case skills.IntentSplit:
		splitDir := filepath.Join(p.Dir, "split_children")
		if err := mgr.ApplyCuratorIntent(p.Metadata, splitDir); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		_ = mgr.RejectProposal(id, "applied: split — "+body.ReviewerNote)
		writeJSON(w, http.StatusOK, map[string]string{"status": "applied", "intent": "split"})
		return

	default:
		meta, err := mgr.PromoteProposal(id, body.RefinedContent, body.ReviewerNote)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if meta.Intent == skills.IntentMerge {
			_ = mgr.ApplyCuratorIntent(meta, "")
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status":   "approved",
			"approved": meta.Category + "/" + meta.Name,
		})
	}
}

// handleProposalReject moves a proposal to _rejected/.
// Body: {"reason": "..."}
func (s *Server) handleProposalReject(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	id := r.PathValue("id")
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	mgr := skills.NewManager(profile)
	if err := mgr.RejectProposal(id, body.Reason); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

// =============================================================================
// History handlers
// =============================================================================

// handleHistoryList returns history snapshots for one active skill.
func (s *Server) handleHistoryList(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	category := r.PathValue("category")
	name := r.PathValue("name")
	mgr := skills.NewManager(profile)
	hist, err := mgr.ListHistory(category, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if hist == nil {
		hist = []skills.HistoryEntry{}
	}
	writeJSON(w, http.StatusOK, hist)
}

// handleHistoryRollback restores an active skill from a snapshot.
// Body: {"timestamp_ms": 1234567890}
func (s *Server) handleHistoryRollback(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	category := r.PathValue("category")
	name := r.PathValue("name")
	var body struct {
		TimestampMs int64 `json:"timestamp_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.TimestampMs == 0 {
		writeError(w, http.StatusBadRequest, "timestamp_ms is required")
		return
	}
	mgr := skills.NewManager(profile)
	if err := mgr.Rollback(category, name, body.TimestampMs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Usage handlers
// =============================================================================

// handleSkillUsageList returns recent usage rows for one skill.
// Path: /v1/skills/usage/{category}/{name}?limit=N
func (s *Server) handleSkillUsageList(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	name := r.PathValue("name")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.ListSkillUsage(category+"/"+name, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []storage.SkillUsage{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleSkillUsageStats returns aggregate stats for one skill.
func (s *Server) handleSkillUsageStats(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	name := r.PathValue("name")
	stats, err := s.db.GetSkillUsageStats(category + "/" + name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// =============================================================================
// Agent run handlers
// =============================================================================

// handleReviewerRun fires one reviewer cycle synchronously.
func (s *Server) handleReviewerRun(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	rep, err := s.runReviewerAgent(r.Context(), profile)
	if err != nil {
		writeJSON(w, http.StatusOK, rep)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleCuratorRun fires one curator cycle synchronously.
func (s *Server) handleCuratorRun(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	rep, err := s.runCuratorAgent(r.Context(), profile)
	if err != nil {
		writeJSON(w, http.StatusOK, rep)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}
