package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/euraika-labs/pan-agent/internal/claw3d"
	"github.com/euraika-labs/pan-agent/internal/config"
)

// handleOfficeMigrationStatus services GET /v1/office/migration/status.
// The frontend Layout.tsx polls this on mount to decide whether to render
// the first-launch migration banner. The response shape is stable:
//
//	{
//	  "needed":    bool,   // legacy source exists on disk
//	  "legacyPath": string, // absolute path (or "" if absent)
//	  "acked":     bool,   // user already dismissed/handled the banner
//	}
//
// Banner is shown when `needed && !acked`. The ack flag lives in
// config.yaml under office.migration_ack so dismissal is durable across
// restarts and across all three banner actions (Import / Keep / Dismiss).
func (s *Server) handleOfficeMigrationStatus(w http.ResponseWriter, _ *http.Request) {
	needed, legacyPath := claw3d.DetectLegacyState()
	oc := config.GetOfficeConfig(s.profile)
	resp := map[string]any{
		"needed":     needed,
		"legacyPath": legacyPath,
		"acked":      oc.MigrationAck,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// migrationRunRequest is the POST body for /v1/office/migration/run.
// Mirrors the CLI flags so the UI and the command-line are
// contract-compatible — an integration test can exercise either path and
// get the same MigrateReport shape back.
type migrationRunRequest struct {
	DryRun    bool   `json:"dryRun,omitempty"`
	Force     bool   `json:"force,omitempty"`
	Source    string `json:"source,omitempty"`
	BackupDir string `json:"backupDir,omitempty"`
}

// handleOfficeMigrationRun services POST /v1/office/migration/run. The
// handler always writes config.yaml's office.migration_ack=true on any
// outcome (ok / skip / missing) because that's the semantic the UI
// needs: "the user acted on the banner, don't show it again." Only
// explicit errors during the run leave the ack flag unchanged, so a
// broken run can be retried from the debug panel.
func (s *Server) handleOfficeMigrationRun(w http.ResponseWriter, r *http.Request) {
	var req migrationRunRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	rep, err := claw3d.RunMigration(s.db, claw3d.MigrateOpts{
		Source:    req.Source,
		DryRun:    req.DryRun,
		Force:     req.Force,
		BackupDir: req.BackupDir,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Mark the banner dismissed on every non-error outcome. "missing"
	// counts as dismissed because if there's nothing to migrate the
	// banner should never re-appear. "ok" and "skip" are obvious. Only
	// real errors (write failures, permissions) skip the ack write so
	// the user can retry from the UI.
	if rep.Status != "" {
		_ = config.SetMigrationAck(s.profile, true)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}
