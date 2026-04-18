package gateway

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/euraika-labs/pan-agent/internal/approval"
	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/recovery"
)

// gatewayApprovalRequester adapts *approval.Store to recovery.ApprovalRequester.
type gatewayApprovalRequester struct {
	store *approval.Store
}

func (g *gatewayApprovalRequester) RequestApproval(sessionID, toolName, arguments string, level approval.Level) (string, error) {
	chk := approval.ApprovalCheck{
		Level:       level,
		PatternKey:  "recovery.shell_inverse",
		Description: fmt.Sprintf("Reversal: %s", toolName),
	}
	a := g.store.CreateWithCheck(sessionID, toolName, arguments, chk)
	return a.ID, nil
}

// recoveryHandler returns (or lazily initialises) the recovery.Handler.
// Constructed on first request so the storage migration (action_receipts table)
// has already run by the time we access the DB.
func (s *Server) recoveryHandler() *recovery.Handler {
	s.recoveryOnce.Do(func() {
		j := recovery.NewJournal(s.db.RawDB())
		root := filepath.Join(paths.AgentHome(), "recovery")
		snap, err := recovery.NewSnapshotter(root, s.profile)
		if err != nil {
			snap = recovery.DefaultSnapshotter(s.profile)
		}
		req := &gatewayApprovalRequester{store: s.approvals}
		reg := recovery.NewRegistry(j, snap, recovery.WithApprovalRequester(req))
		s.recoveryH = recovery.NewHandler(j, reg, snap)
	})
	return s.recoveryH
}

func (s *Server) handleRecoveryList(w http.ResponseWriter, r *http.Request) {
	s.recoveryHandler().List(w, r)
}

func (s *Server) handleRecoveryListByTask(w http.ResponseWriter, r *http.Request) {
	s.recoveryHandler().ListByTask(w, r)
}

func (s *Server) handleRecoveryUndo(w http.ResponseWriter, r *http.Request) {
	s.recoveryHandler().Undo(w, r)
}

func (s *Server) handleRecoveryDiff(w http.ResponseWriter, r *http.Request) {
	s.recoveryHandler().Diff(w, r)
}
