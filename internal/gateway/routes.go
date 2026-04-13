package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/approval"
	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/cron"
	"github.com/euraika-labs/pan-agent/internal/llm"
	"github.com/euraika-labs/pan-agent/internal/memory"
	"github.com/euraika-labs/pan-agent/internal/models"
	"github.com/euraika-labs/pan-agent/internal/persona"
	"github.com/euraika-labs/pan-agent/internal/skills"
	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/euraika-labs/pan-agent/internal/tools"
)

// registerRoutes mounts all REST endpoints onto mux.
// Go 1.22+ pattern syntax is used: "METHOD /path" automatically restricts the
// handler to that HTTP method. The {id} and {index} tokens are path parameters
// accessible via r.PathValue("id").
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// ------------------------------------------------------------------ chat
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /v1/chat/abort", s.handleChatAbort)

	// --------------------------------------------------------------- approvals
	mux.HandleFunc("POST /v1/approvals/{id}", s.handleApprovalResolve)
	mux.HandleFunc("GET /v1/approvals/{id}", s.handleApprovalGet)
	mux.HandleFunc("GET /v1/approvals", s.handleApprovalList)

	// --------------------------------------------------------------- sessions
	mux.HandleFunc("GET /v1/sessions", s.handleSessionList)
	mux.HandleFunc("GET /v1/sessions/{id}", s.handleSessionGet)

	// ----------------------------------------------------------------- models
	mux.HandleFunc("GET /v1/models", s.handleModelList)
	mux.HandleFunc("POST /v1/models", s.handleModelAdd)
	mux.HandleFunc("DELETE /v1/models/{id}", s.handleModelDelete)
	mux.HandleFunc("POST /v1/models/sync", s.handleModelSync)

	// ----------------------------------------------------------------- config
	mux.HandleFunc("GET /v1/config", s.handleConfigGet)
	mux.HandleFunc("PUT /v1/config", s.handleConfigPut)

	// ----------------------------------------------------------------- memory
	mux.HandleFunc("GET /v1/memory", s.handleMemoryGet)
	mux.HandleFunc("POST /v1/memory", s.handleMemoryAdd)
	mux.HandleFunc("PUT /v1/memory/{index}", s.handleMemoryUpdate)
	mux.HandleFunc("DELETE /v1/memory/{index}", s.handleMemoryDelete)

	// ---------------------------------------------------------------- persona
	mux.HandleFunc("GET /v1/persona", s.handlePersonaGet)
	mux.HandleFunc("PUT /v1/persona", s.handlePersonaPut)
	mux.HandleFunc("POST /v1/persona/reset", s.handlePersonaReset)

	// ------------------------------------------------------------------ tools
	mux.HandleFunc("GET /v1/tools", s.handleToolList)
	mux.HandleFunc("PUT /v1/tools/{key}", s.handleToolToggle)

	// ----------------------------------------------------------------- skills
	mux.HandleFunc("GET /v1/skills", s.handleSkillList)
	mux.HandleFunc("POST /v1/skills/install", s.handleSkillInstall)
	mux.HandleFunc("POST /v1/skills/uninstall", s.handleSkillUninstall)

	// ------------------------------------------------------------------- cron
	mux.HandleFunc("GET /v1/cron", s.handleCronList)
	mux.HandleFunc("POST /v1/cron", s.handleCronCreate)
	mux.HandleFunc("DELETE /v1/cron/{id}", s.handleCronDelete)

	// ----------------------------------------------------------------- health
	mux.HandleFunc("GET /v1/health", s.handleHealth)
}

// =============================================================================
// Approval handlers
// =============================================================================

// handleApprovalList returns all pending approval requests.
func (s *Server) handleApprovalList(w http.ResponseWriter, r *http.Request) {
	list := s.approvals.ListPending()
	writeJSON(w, http.StatusOK, list)
}

// handleApprovalGet returns a single approval by ID.
func (s *Server) handleApprovalGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.approvals.Get(id)
	if err == approval.ErrNotFound {
		writeError(w, http.StatusNotFound, "approval not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// handleApprovalResolve resolves a pending approval.
// Body: {"approved": true|false}
func (s *Server) handleApprovalResolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var body struct {
		Approved bool `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := s.approvals.Resolve(id, body.Approved); err != nil {
		switch err {
		case approval.ErrNotFound:
			writeError(w, http.StatusNotFound, "approval not found")
		case approval.ErrAlreadyResolved:
			writeError(w, http.StatusConflict, "approval already resolved")
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Session handlers
// =============================================================================

// handleSessionList returns sessions with optional pagination and search.
// Query params: limit (default 50), offset (default 0), q (search string).
func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := queryInt(q.Get("limit"), 50)
	offset := queryInt(q.Get("offset"), 0)
	search := strings.TrimSpace(q.Get("q"))

	if search != "" {
		results, err := s.db.SearchSessions(search, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if results == nil {
			results = []storage.SearchResult{}
		}
		writeJSON(w, http.StatusOK, results)
		return
	}

	sessions, err := s.db.ListSessions(limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sessions == nil {
		sessions = []storage.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

// handleSessionGet returns all messages for a single session.
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msgs, err := s.db.GetMessages(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if msgs == nil {
		msgs = []storage.Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

// =============================================================================
// Model handlers
// =============================================================================

// handleModelList returns the list of models from the active LLM provider.
func (s *Server) handleModelList(w http.ResponseWriter, r *http.Request) {
	if s.llmClient == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	models, err := s.llmClient.ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if models == nil {
		models = []llm.ModelInfo{}
	}
	writeJSON(w, http.StatusOK, models)
}

// handleModelAdd updates the active model configuration.
// Body: {"provider": "...", "model": "...", "base_url": "..."}
func (s *Server) handleModelAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		BaseURL  string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := config.SetModelConfig(body.Provider, body.Model, body.BaseURL, s.profile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Refresh the in-process LLM client.
	s.llmClient = llm.NewClient(body.BaseURL, "", body.Model)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleModelDelete removes a saved model entry by ID.
func (s *Server) handleModelDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := models.Remove(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleModelSync fetches and caches the remote model list for the given provider.
// Body: {"provider": "...", "base_url": "...", "api_key": "..."}
func (s *Server) handleModelSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url is required")
		return
	}
	ms, err := models.SyncRemote(body.Provider, body.BaseURL, body.APIKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

// =============================================================================
// Config handlers
// =============================================================================

// handleConfigGet returns the .env key/value map for the active profile.
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	env, err := config.ReadProfileEnv(s.profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// handleConfigPut updates one or more .env key/value pairs.
// Body: {"KEY": "value", ...}
func (s *Server) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	for k, v := range updates {
		if err := config.SetProfileEnvValue(s.profile, k, v); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Memory handlers
// =============================================================================

// handleMemoryGet returns the full memory state for the active profile.
func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	state, err := memory.ReadMemory(s.profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handleMemoryAdd appends a new entry to MEMORY.md.
// Body: {"content": "..."}
func (s *Server) handleMemoryAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if err := memory.AddEntry(body.Content, s.profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// handleMemoryUpdate replaces the memory entry at the given zero-based index.
// Body: {"content": "..."}
func (s *Server) handleMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	idx, ok := pathInt(w, r, "index")
	if !ok {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := memory.UpdateEntry(idx, body.Content, s.profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMemoryDelete removes the memory entry at the given zero-based index.
func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	idx, ok := pathInt(w, r, "index")
	if !ok {
		return
	}
	if err := memory.RemoveEntry(idx, s.profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Persona handlers
// =============================================================================

// handlePersonaGet returns the contents of SOUL.md.
func (s *Server) handlePersonaGet(w http.ResponseWriter, r *http.Request) {
	content, err := persona.Read(s.profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"persona": content})
}

// handlePersonaPut writes new persona content to SOUL.md.
// Body: {"persona": "..."}
func (s *Server) handlePersonaPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Persona string `json:"persona"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := persona.Write(body.Persona, s.profile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handlePersonaReset restores the default persona.
func (s *Server) handlePersonaReset(w http.ResponseWriter, r *http.Request) {
	if err := persona.Reset(s.profile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Tool handlers
// =============================================================================

// handleToolList returns all registered tools from the tools registry.
func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	all := tools.All()
	// Convert map to a stable slice of name+description objects for JSON.
	type toolEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	result := make([]toolEntry, 0, len(all))
	for _, t := range all {
		result = append(result, toolEntry{Name: t.Name(), Description: t.Description()})
	}
	writeJSON(w, http.StatusOK, result)
}

// handleToolToggle enables or disables a toolset by key.
// Tool toggling is a config concern; for now we acknowledge the request and return 200.
func (s *Server) handleToolToggle(w http.ResponseWriter, r *http.Request) {
	_ = r.PathValue("key")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Skill handlers
// =============================================================================

// handleSkillList returns all installed and bundled skills combined.
func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	installed, err := skills.ListInstalled(s.profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bundled, err := skills.ListBundled()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	combined := append(installed, bundled...)
	if combined == nil {
		combined = []skills.Skill{}
	}
	writeJSON(w, http.StatusOK, combined)
}

// handleSkillInstall installs a skill by id for the active profile.
// Body: {"id": "category/skill-name", "profile": "..."}
func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Profile string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	profile := body.Profile
	if profile == "" {
		profile = s.profile
	}
	if err := skills.Install(body.ID, profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleSkillUninstall removes an installed skill by id.
// Body: {"id": "category/skill-name", "profile": "..."}
func (s *Server) handleSkillUninstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Profile string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.ID == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	profile := body.Profile
	if profile == "" {
		profile = s.profile
	}
	if err := skills.Uninstall(body.ID, profile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Cron handlers
// =============================================================================

// handleCronList returns all scheduled cron jobs.
func (s *Server) handleCronList(w http.ResponseWriter, r *http.Request) {
	jobs, err := cron.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if jobs == nil {
		jobs = []cron.Job{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

// handleCronCreate creates a new cron job.
// Body: {"name": "...", "schedule": "...", "prompt": "..."}
func (s *Server) handleCronCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Schedule == "" {
		writeError(w, http.StatusBadRequest, "schedule is required")
		return
	}
	job, err := cron.Create(body.Name, body.Schedule, body.Prompt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

// handleCronDelete removes a cron job by ID.
func (s *Server) handleCronDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := cron.Remove(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Health handler
// =============================================================================

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// helpers
// =============================================================================

// writeJSON serialises v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error envelope.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// queryInt parses s as an integer, returning fallback on any parse error.
func queryInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return fallback
	}
	return v
}

// pathInt reads a named path parameter as a zero-based integer index.
// It writes a 400 Bad Request and returns false on any parse error.
func pathInt(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	raw := r.PathValue(name)
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		writeError(w, http.StatusBadRequest, name+" must be a non-negative integer")
		return 0, false
	}
	return v, true
}
