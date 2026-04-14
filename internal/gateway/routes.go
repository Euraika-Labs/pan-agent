package gateway

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/approval"
	"github.com/euraika-labs/pan-agent/internal/claw3d"
	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/cron"
	"github.com/euraika-labs/pan-agent/internal/memory"
	"github.com/euraika-labs/pan-agent/internal/models"
	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/persona"
	"github.com/euraika-labs/pan-agent/internal/skills"
	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/euraika-labs/pan-agent/internal/tools"
	"github.com/euraika-labs/pan-agent/internal/version"
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
	// GET /v1/skills         → installed skills only (per-profile)
	// GET /v1/skills/bundled → skills shipped with the binary
	// UI discriminates between the two so "Browse" vs "Installed" tabs
	// render cleanly.
	mux.HandleFunc("GET /v1/skills", s.handleSkillListInstalled)
	mux.HandleFunc("GET /v1/skills/bundled", s.handleSkillListBundled)
	mux.HandleFunc("POST /v1/skills/install", s.handleSkillInstall)
	mux.HandleFunc("POST /v1/skills/uninstall", s.handleSkillUninstall)

	// ----------------------------------------------------- skill self-healing
	// Proposal queue (reviewer agent + manual UI both consume these).
	mux.HandleFunc("GET /v1/skills/proposals", s.handleProposalList)
	mux.HandleFunc("GET /v1/skills/proposals/{id}", s.handleProposalGet)
	mux.HandleFunc("POST /v1/skills/proposals/{id}/approve", s.handleProposalApprove)
	mux.HandleFunc("POST /v1/skills/proposals/{id}/reject", s.handleProposalReject)

	// History (rollback) for active skills.
	mux.HandleFunc("GET /v1/skills/history/{category}/{name}", s.handleHistoryList)
	mux.HandleFunc("POST /v1/skills/history/{category}/{name}/rollback", s.handleHistoryRollback)

	// Usage stats per skill.
	mux.HandleFunc("GET /v1/skills/usage/{category}/{name}", s.handleSkillUsageList)
	mux.HandleFunc("GET /v1/skills/usage/{category}/{name}/stats", s.handleSkillUsageStats)

	// Run reviewer / curator agent loops on demand.
	mux.HandleFunc("POST /v1/skills/reviewer/run", s.handleReviewerRun)
	mux.HandleFunc("POST /v1/skills/curator/run", s.handleCuratorRun)

	// ------------------------------------------------------------------- cron
	mux.HandleFunc("GET /v1/cron", s.handleCronList)
	mux.HandleFunc("POST /v1/cron", s.handleCronCreate)
	mux.HandleFunc("DELETE /v1/cron/{id}", s.handleCronDelete)

	// ----------------------------------------------------------------- health
	mux.HandleFunc("GET /v1/health", s.handleHealth)

	// -------------------------------------------------------------- profiles
	mux.HandleFunc("GET /v1/config/profiles", s.handleProfileList)
	mux.HandleFunc("POST /v1/config/profiles", s.handleProfileCreate)
	mux.HandleFunc("DELETE /v1/config/profiles/{name}", s.handleProfileDelete)

	// ------------------------------------------------------------- doctor
	mux.HandleFunc("POST /v1/config/doctor", s.handleDoctorRun)
	mux.HandleFunc("POST /v1/config/update", s.handleUpdateCheck)

	// -------------------------------------------------- gateway start/stop
	mux.HandleFunc("POST /v1/health/gateway/start", s.handleGatewayStart)
	mux.HandleFunc("POST /v1/health/gateway/stop", s.handleGatewayStop)

	// ----------------------------------------------------------------- office
	// (formerly /v1/claw3d/*; renamed in the audit pass to match UI expectations.
	// The underlying package is still internal/claw3d — Claw3D is one specific
	// engine backing the "office workspace" abstraction.)
	mux.HandleFunc("GET /v1/office/status", s.handleOfficeStatus)
	mux.HandleFunc("POST /v1/office/setup", s.handleOfficeSetup)
	mux.HandleFunc("POST /v1/office/start", s.handleOfficeStart)
	mux.HandleFunc("POST /v1/office/stop", s.handleOfficeStop)
	mux.HandleFunc("GET /v1/office/logs", s.handleOfficeLogs)
	mux.HandleFunc("GET /v1/office/config", s.handleOfficeConfig)
	mux.HandleFunc("GET /v1/office/setup/progress", s.handleOfficeSetupProgress)
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

// handleModelList returns the local model library.
func (s *Server) handleModelList(w http.ResponseWriter, _ *http.Request) {
	list, err := models.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
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
	s.refreshLLMClient(body.BaseURL, body.Model, s.profile)
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

// configResponse is the structured response for GET /v1/config.
type configResponse struct {
	Env            map[string]string              `json:"env"`
	AgentHome      string                         `json:"agentHome"`
	Model          configModelResponse            `json:"model"`
	CredentialPool map[string][]config.Credential `json:"credentialPool"`
	AppVersion     string                         `json:"appVersion"`
	AgentVersion   *string                        `json:"agentVersion"`
}

type configModelResponse struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseUrl"`
}

// handleConfigGet returns the full config for the active profile.
//
// Secret values in env (API keys, tokens) are masked to their last 4 chars so
// the UI can confirm a key is set without exposing it to browser devtools,
// proxy logs, or MCP servers with localhost access. The local HTTP API has no
// auth (binds to 127.0.0.1 only) — masking is the last line of defence.
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	env, err := config.ReadProfileEnv(profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mc := config.GetModelConfig(profile)
	pool := config.GetCredentialPool()

	resp := configResponse{
		Env:       maskSecretEnv(env),
		AgentHome: paths.AgentHome(),
		Model: configModelResponse{
			Provider: mc.Provider,
			Model:    mc.Model,
			BaseURL:  mc.BaseURL,
		},
		CredentialPool: maskCredentialPool(pool),
		AppVersion:     version.Version,
		AgentVersion:   nil,
	}
	writeJSON(w, http.StatusOK, resp)
}

// maskSecretEnv returns a copy of env with secret-shaped keys (ending in
// _API_KEY, _TOKEN, _SECRET, _PASSWORD) masked to "<prefix>...<last4>".
// Empty values pass through unchanged so the UI can distinguish "never set"
// from "set to an empty string".
func maskSecretEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if isSecretEnvKey(k) && v != "" {
			out[k] = maskSecret(v)
			continue
		}
		out[k] = v
	}
	return out
}

// maskCredentialPool masks the credential-pool keys/tokens the same way.
func maskCredentialPool(pool map[string][]config.Credential) map[string][]config.Credential {
	if pool == nil {
		return nil
	}
	out := make(map[string][]config.Credential, len(pool))
	for provider, creds := range pool {
		masked := make([]config.Credential, len(creds))
		for i, c := range creds {
			masked[i] = c
			if c.Key != "" {
				masked[i].Key = maskSecret(c.Key)
			}
		}
		out[provider] = masked
	}
	return out
}

// isSecretEnvKey returns true for env var names that typically hold secrets.
// Keep conservative — false positives (masking a non-secret) are harmless;
// false negatives (leaking a secret) are not. Expanded during the audit
// debate to cover AWS-style ACCESS_KEY_ID, _PRIVATE_KEY, and the various
// suffix families common in cloud credentials.
func isSecretEnvKey(name string) bool {
	upper := strings.ToUpper(name)
	// Any containing match on a secret-word substring. Expanded after
	// code review: DSN/URL/URI commonly embed credentials
	// (postgres://user:pw@host, https://hooks.slack.com/services/T…/B…/<secret>).
	// Cookie/JWT/Bearer/Signature are credential material. Plain KEY + APIKEY
	// cover the bare-env case OPENAI_KEY / KEY= / APIKEY=.
	secretTokens := []string{
		"API_KEY",
		"APIKEY",
		"TOKEN",
		"SECRET",
		"PASSWORD",
		"PASSWD",
		"PRIVATE_KEY",
		"ACCESS_KEY", // AWS pattern
		"AUTH_KEY",
		"SIGNING_KEY",
		"ENCRYPTION_KEY",
		"SESSION_KEY",
		"CREDENTIAL",
		"DATABASE_URL",
		"REDIS_URL",
		"POSTGRES_URL",
		"MONGO_URI",
		"SMTP_URL",
		"DSN",     // SENTRY_DSN etc — DSNs embed auth
		"WEBHOOK", // Slack/Discord/GitHub webhook URLs are bearer-equivalent
		"JWT",
		"BEARER",
		"COOKIE",
		"SIGNATURE",
		"SALT",
		"HMAC",
	}
	for _, tok := range secretTokens {
		if strings.Contains(upper, tok) {
			return true
		}
	}
	// Bare "KEY" as a whole env name is also a secret-by-convention.
	if upper == "KEY" {
		return true
	}
	return false
}

// maskSecret returns "<first3>***<last4>" for values ≥ 8 chars, or "***"
// for anything shorter (don't leak the length of a short password).
func maskSecret(v string) string {
	if len(v) < 8 {
		return "***"
	}
	return v[:3] + "***" + v[len(v)-4:]
}

// configPutBody is the union of all fields the PUT /v1/config endpoint accepts.
type configPutBody struct {
	Profile         string                         `json:"profile,omitempty"`
	Env             map[string]string              `json:"env,omitempty"`
	Model           *configModelPut                `json:"model,omitempty"`
	CredentialPool  map[string][]config.Credential `json:"credentialPool,omitempty"`
	PlatformEnabled map[string]bool                `json:"platformEnabled,omitempty"`
}

type configModelPut struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseUrl"`
}

// handleConfigPut updates config for the active profile.
// Accepts a structured body with optional env, model, credentialPool, and
// platformEnabled fields.
func (s *Server) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	var body configPutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	profile := body.Profile
	if profile == "" {
		profile = s.profile
	}

	// Update env values.
	for k, v := range body.Env {
		if err := config.SetProfileEnvValue(profile, k, v); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Update model config.
	if body.Model != nil {
		if err := config.SetModelConfig(body.Model.Provider, body.Model.Model, body.Model.BaseURL, profile); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.refreshLLMClient(body.Model.BaseURL, body.Model.Model, profile)
	}

	// Update credential pool.
	for provider, creds := range body.CredentialPool {
		if err := config.SetCredentialPool(provider, creds); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Update platform toggles.
	for platform, enabled := range body.PlatformEnabled {
		if err := config.SetPlatformEnabled(platform, enabled, profile); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Memory handlers
// =============================================================================

// memoryEntry is one indexed memory entry. The UI (Memory.tsx:5) keys its
// map by entry.index — returning bare strings caused an "undefined key"
// React warning during the audit.
type memoryEntry struct {
	Index   int    `json:"index"`
	Content string `json:"content"`
}

// memorySection carries agent-memory or user-profile content for the UI.
type memorySection struct {
	Content      string        `json:"content"`
	Exists       bool          `json:"exists"`
	LastModified *int64        `json:"lastModified,omitempty"`
	Entries      []memoryEntry `json:"entries"`
	CharCount    int           `json:"charCount"`
	CharLimit    int           `json:"charLimit"`
}

// memoryStats aggregates DB-level session/message counts that Memory.tsx
// renders in its stats strip.
type memoryStats struct {
	TotalSessions int `json:"totalSessions"`
	TotalMessages int `json:"totalMessages"`
}

// memoryResponse is the shape Memory.tsx expects from GET /v1/memory.
type memoryResponse struct {
	Memory memorySection `json:"memory"`
	User   memorySection `json:"user"`
	Stats  memoryStats   `json:"stats"`
}

// handleMemoryGet composes the nested {memory, user, stats} payload the UI
// expects. Keeps internal/memory/ pure disk-I/O; the DB join lives here.
func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	state, err := memory.ReadMemory(s.profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Last-modified timestamps from disk. Best-effort: any stat failure
	// yields nil rather than erroring out the whole response.
	memModified := statFileModified(paths.MemoryFile(s.profile))
	userModified := statFileModified(paths.UserFile(s.profile))

	// DB stats are best-effort too — if the DB blows up we still return
	// the disk data and zeros for stats (better than a 500).
	var totalSessions, totalMessages int
	if s.db != nil {
		totalSessions, _ = s.db.CountSessions()
		totalMessages, _ = s.db.CountMessages()
	}

	// Convert []string → []memoryEntry so the UI can key on entry.index
	// rather than the string content (which would collide on duplicates
	// and trigger React's missing-key warning).
	entries := make([]memoryEntry, 0, len(state.Entries))
	for i, e := range state.Entries {
		entries = append(entries, memoryEntry{Index: i, Content: e})
	}

	resp := memoryResponse{
		Memory: memorySection{
			Content:      strings.Join(state.Entries, memory.EntryDelimiter),
			Exists:       memModified != nil,
			LastModified: memModified,
			Entries:      entries,
			CharCount:    state.CharCount,
			CharLimit:    state.CharLimit,
		},
		User: memorySection{
			Content:      state.UserProfile,
			Exists:       userModified != nil,
			LastModified: userModified,
			Entries:      []memoryEntry{},
			CharCount:    state.UserCharCount,
			CharLimit:    state.UserCharLimit,
		},
		Stats: memoryStats{
			TotalSessions: totalSessions,
			TotalMessages: totalMessages,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

// statFileModified returns the file's ModTime as Unix millis, or nil if the
// file doesn't exist / isn't stat-able.
func statFileModified(path string) *int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	ms := fi.ModTime().UnixMilli()
	return &ms
}

// operationResult is the shape the desktop UI's OperationResult type expects
// from mutation endpoints. Using {success: bool, error?: string} instead of
// a status string lets the React components do a single boolean check rather
// than parsing status codes + strings.
type operationResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, operationResult{Success: true})
}

func writeOpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, operationResult{Success: false, Error: msg})
}

// handleMemoryAdd appends a new entry to MEMORY.md.
// Body: {"content": "..."}
func (s *Server) handleMemoryAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeOpError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Content == "" {
		writeOpError(w, http.StatusBadRequest, "content is required")
		return
	}
	if err := memory.AddEntry(body.Content, s.profile); err != nil {
		writeOpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w)
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
		writeOpError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := memory.UpdateEntry(idx, body.Content, s.profile); err != nil {
		writeOpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w)
}

// handleMemoryDelete removes the memory entry at the given zero-based index.
func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	idx, ok := pathInt(w, r, "index")
	if !ok {
		return
	}
	if err := memory.RemoveEntry(idx, s.profile); err != nil {
		writeOpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w)
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
//
// Shape matches desktop/src/screens/Tools/Tools.tsx:4's ToolsetInfo:
//
//	{key, label, description, enabled}
//
// `key` is the canonical tool name (used as the React map key + the path
// param for the toggle endpoint). `label` is the same name but humanised
// for display. `enabled` defaults true — config.GetToolsEnabled could wire
// a per-profile disable list here in future; for now all tools are always
// available.
func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	all := tools.All()
	type toolsetInfo struct {
		Key         string `json:"key"`
		Label       string `json:"label"`
		Description string `json:"description"`
		Enabled     bool   `json:"enabled"`
	}
	// Sort for stable UI ordering.
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]toolsetInfo, 0, len(keys))
	for _, k := range keys {
		t := all[k]
		result = append(result, toolsetInfo{
			Key:         t.Name(),
			Label:       humaniseToolName(t.Name()),
			Description: t.Description(),
			Enabled:     true,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// humaniseToolName turns "skill_manage" → "Skill Manage", "web_search" →
// "Web Search". Cheap helper that keeps the UI looking tidy without
// forcing every tool to carry a Display() method.
func humaniseToolName(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
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

// handleSkillListInstalled returns only installed skills for the active
// profile. Underscore-prefixed reserved subdirs (_proposed, _archived, …)
// are excluded by walkSkillsDir in the skills package.
func (s *Server) handleSkillListInstalled(w http.ResponseWriter, r *http.Request) {
	installed, err := skills.ListInstalled(s.profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if installed == nil {
		installed = []skills.Skill{}
	}
	writeJSON(w, http.StatusOK, installed)
}

// handleSkillListBundled returns only bundled skills (shipped with the binary).
// Feeds the UI's "Browse" tab that lets the user pick a bundled skill to
// install into their profile.
func (s *Server) handleSkillListBundled(w http.ResponseWriter, _ *http.Request) {
	bundled, err := skills.ListBundled()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if bundled == nil {
		bundled = []skills.Skill{}
	}
	writeJSON(w, http.StatusOK, bundled)
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

type healthResponse struct {
	Gateway         bool              `json:"gateway"`
	Env             map[string]string `json:"env"`
	PlatformEnabled map[string]bool   `json:"platformEnabled"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	profile := s.resolveProfile(r)
	env, _ := config.ReadProfileEnv(profile)
	resp := healthResponse{
		Gateway:         s.isGatewayRunning(),
		Env:             env,
		PlatformEnabled: config.GetPlatformEnabled(profile),
	}
	writeJSON(w, http.StatusOK, resp)
}

// =============================================================================
// Gateway start/stop handlers
// =============================================================================

func (s *Server) handleGatewayStart(w http.ResponseWriter, _ *http.Request) {
	s.gatewayMu.Lock()
	defer s.gatewayMu.Unlock()

	if s.gatewayRunning {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already running"})
		return
	}
	if s.botCancels == nil {
		s.botCancels = make(map[string]context.CancelFunc)
	}

	env, _ := config.ReadProfileEnv(s.profile)
	enabled := config.GetPlatformEnabled(s.profile)
	var started []string

	// Telegram
	if enabled["telegram"] {
		token := env["TELEGRAM_BOT_TOKEN"]
		if token != "" {
			cancel, err := s.startTelegram(token, env["TELEGRAM_ALLOWED_USERS"])
			if err != nil {
				log.Printf("[gateway] telegram start error: %v", err)
			} else {
				s.botCancels["telegram"] = cancel
				started = append(started, "telegram")
			}
		}
	}

	// Discord
	if enabled["discord"] {
		token := env["DISCORD_BOT_TOKEN"]
		if token != "" {
			cancel, err := s.startDiscord(token)
			if err != nil {
				log.Printf("[gateway] discord start error: %v", err)
			} else {
				s.botCancels["discord"] = cancel
				started = append(started, "discord")
			}
		}
	}

	// Slack
	if enabled["slack"] {
		botToken := env["SLACK_BOT_TOKEN"]
		appToken := env["SLACK_APP_TOKEN"]
		if botToken != "" {
			cancel, err := s.startSlack(botToken, appToken)
			if err != nil {
				log.Printf("[gateway] slack start error: %v", err)
			} else {
				s.botCancels["slack"] = cancel
				started = append(started, "slack")
			}
		}
	}

	s.gatewayRunning = len(s.botCancels) > 0
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"started": started,
	})
}

func (s *Server) handleGatewayStop(w http.ResponseWriter, _ *http.Request) {
	s.gatewayMu.Lock()
	defer s.gatewayMu.Unlock()

	for platform, cancel := range s.botCancels {
		cancel()
		delete(s.botCancels, platform)
	}
	s.gatewayRunning = false
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// =============================================================================
// Profile handlers
// =============================================================================

func (s *Server) handleProfileList(w http.ResponseWriter, _ *http.Request) {
	profiles := config.ListProfiles(s.profile)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"profiles": profiles,
	})
}

func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		CloneConfig bool   `json:"cloneConfig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	cloneFrom := ""
	if body.CloneConfig {
		cloneFrom = s.profile
	}
	if err := config.CreateProfile(body.Name, cloneFrom); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Server) handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := config.DeleteProfile(name); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// =============================================================================
// Doctor / update handlers
// =============================================================================

func (s *Server) handleDoctorRun(w http.ResponseWriter, _ *http.Request) {
	output := config.RunDoctor(s.profile)
	writeJSON(w, http.StatusOK, map[string]string{"output": output})
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available": false,
		"current":   version.Version,
	})
}

// =============================================================================
// Office handlers (backed by internal/claw3d — "office workspace" abstraction
// over the Claw3D engine)
// =============================================================================

// handleOfficeStatus returns the current Office installation and process state.
func (s *Server) handleOfficeStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, claw3d.Status())
}

// handleOfficeSetup clones the upstream engine repo and runs npm install.
// Streams progress lines as newline-delimited JSON objects:
//
//	{"progress": "..."}
//
// Ends with {"done": true} on success or {"error": "..."} on failure.
func (s *Server) handleOfficeSetup(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	enc := json.NewEncoder(w)
	emit := func(line string) {
		_ = enc.Encode(map[string]string{"progress": line})
		flush()
	}

	err := claw3d.Setup(emit)
	if err != nil {
		_ = enc.Encode(map[string]string{"error": err.Error()})
		flush()
		return
	}
	_ = enc.Encode(map[string]bool{"done": true})
	flush()
}

// handleOfficeStart starts both the dev server and the adapter.
// Returns {success, error} matching the UI's OperationResult shape.
func (s *Server) handleOfficeStart(w http.ResponseWriter, _ *http.Request) {
	if err := claw3d.StartDevServer(); err != nil {
		writeOpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := claw3d.StartAdapter(); err != nil {
		writeOpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w)
}

// handleOfficeStop stops both the dev server and the adapter.
func (s *Server) handleOfficeStop(w http.ResponseWriter, _ *http.Request) {
	devErr := claw3d.StopDevServer()
	adpErr := claw3d.StopAdapter()
	if devErr != nil {
		writeOpError(w, http.StatusInternalServerError, devErr.Error())
		return
	}
	if adpErr != nil {
		writeOpError(w, http.StatusInternalServerError, adpErr.Error())
		return
	}
	writeOK(w)
}

// handleOfficeLogs returns recent log lines from the office engine. The
// claw3d package buffers stdout/stderr from both the dev server and the
// adapter into an in-process ring so silent failures (port collisions,
// missing deps) surface here instead of the void.
func (s *Server) handleOfficeLogs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"logs": claw3d.GetLogs()})
}

// handleOfficeConfig returns the office engine's effective config.
// Stub: returns what's knowable without a running engine. UI uses this to
// populate the "configured port" display.
func (s *Server) handleOfficeConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"port":      3000,
		"installed": claw3d.Status().Installed,
	})
}

// handleOfficeSetupProgress returns the in-progress setup state when polled.
// Stub: returns "idle" — the real setup flow uses the NDJSON stream at
// /v1/office/setup. This endpoint exists so the UI's optimistic polling
// doesn't 404 during normal operation.
func (s *Server) handleOfficeSetupProgress(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"state":    "idle",
		"progress": 0,
	})
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
