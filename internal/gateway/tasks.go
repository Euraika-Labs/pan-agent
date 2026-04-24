package gateway

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/euraika-labs/pan-agent/internal/taskrunner"
)

func (s *Server) taskStore() *taskrunner.Store {
	return taskrunner.NewStore(s.db.RawDB())
}

func (s *Server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID  string  `json:"session_id"`
		PlanJSON   string  `json:"plan_json"`
		CostCapUSD float64 `json:"cost_cap_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.SessionID == "" {
		writeError(w, http.StatusBadRequest, "session_id is required")
		return
	}

	task, err := s.taskStore().CreateTask(body.SessionID, body.PlanJSON, body.CostCapUSD)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	store := s.taskStore()

	var tasks []taskrunner.Task
	var err error
	if sessionID != "" {
		tasks, err = store.ListTasks(sessionID, 100)
	} else {
		tasks, err = store.ListAllTasks(100)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []taskrunner.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.taskStore().GetTask(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.taskStore().ListEvents(id, 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []taskrunner.TaskEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleTaskPause(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.taskStore().UpdateStatus(id, taskrunner.StatusRunning, taskrunner.StatusPaused)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "task is not in running state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleTaskResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.taskStore().UpdateStatus(id, taskrunner.StatusPaused, taskrunner.StatusQueued)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "task is not in paused state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store := s.taskStore()
	task, err := store.GetTask(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "task not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	switch task.Status {
	case taskrunner.StatusSucceeded, taskrunner.StatusFailed,
		taskrunner.StatusCancelled, taskrunner.StatusZombie:
		writeError(w, http.StatusConflict, "task is already in a terminal state")
		return
	}

	ok, err := store.UpdateStatus(id, task.Status, taskrunner.StatusCancelled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "concurrent status change")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
