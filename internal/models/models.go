// Package models manages the saved-model library stored in models.json.
// On first use the file is seeded with three default entries (Claude, GPT-4o,
// Kimi).  All mutations are protected by an in-process mutex so concurrent
// goroutines sharing a single process are safe; the file is the source of truth
// for multi-process scenarios.
package models

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SavedModel is a single entry in the model library.
type SavedModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	CreatedAt int64  `json:"created_at"`
}

// defaultModel is the seed shape (no ID / timestamp – those are assigned on
// first write).
type defaultModel struct {
	name     string
	provider string
	model    string
	baseURL  string
}

// defaultModels matches the TypeScript DEFAULT_MODELS list.
var defaultModels = []defaultModel{
	{name: "Claude 3.5 Sonnet", provider: "anthropic", model: "claude-3-5-sonnet-20241022", baseURL: ""},
	{name: "GPT-4o", provider: "openai", model: "gpt-4o", baseURL: ""},
	{name: "Kimi K2", provider: "regolo", model: "kimi-k2-0905", baseURL: "https://api.regolo.ai/v1"},
}

// ---------------------------------------------------------------------------
// File I/O helpers
// ---------------------------------------------------------------------------

var mu sync.Mutex // guards reads/writes within this process

func filePath() string {
	return paths.ModelsFile()
}

func readModels() ([]SavedModel, error) {
	data, err := os.ReadFile(filePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("models: read file: %w", err)
	}
	var models []SavedModel
	if err := json.Unmarshal(data, &models); err != nil {
		return nil, fmt.Errorf("models: parse file: %w", err)
	}
	return models, nil
}

func writeModels(models []SavedModel) error {
	data, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		return fmt.Errorf("models: marshal: %w", err)
	}
	if err := os.MkdirAll(paths.AgentHome(), 0o700); err != nil {
		return fmt.Errorf("models: mkdir: %w", err)
	}
	if err := os.WriteFile(filePath(), data, 0o600); err != nil {
		return fmt.Errorf("models: write file: %w", err)
	}
	return nil
}

// newUUID returns a random UUID v4 string using crypto/rand.
func newUUID() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		// Extremely unlikely; fall back to a timestamp-based pseudo-id.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

// ---------------------------------------------------------------------------
// Seed
// ---------------------------------------------------------------------------

func seedDefaults() ([]SavedModel, error) {
	models := make([]SavedModel, 0, len(defaultModels))
	for _, d := range defaultModels {
		models = append(models, SavedModel{
			ID:        newUUID(),
			Name:      d.name,
			Provider:  d.provider,
			Model:     d.model,
			BaseURL:   d.baseURL,
			CreatedAt: nowMs(),
		})
	}
	if err := writeModels(models); err != nil {
		return nil, err
	}
	return models, nil
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// List returns all saved models.  If models.json does not exist yet the three
// default models are written and returned (same behaviour as the TypeScript
// listModels function).
func List() ([]SavedModel, error) {
	mu.Lock()
	defer mu.Unlock()

	if _, err := os.Stat(filePath()); os.IsNotExist(err) {
		return seedDefaults()
	}

	models, err := readModels()
	if err != nil {
		return nil, err
	}
	return models, nil
}

// Add appends a new model entry.  If an entry with the same model ID and
// provider already exists the existing entry is returned without modification
// (dedup by model+provider, matching TypeScript behaviour).
func Add(name, provider, model, baseURL string) (*SavedModel, error) {
	mu.Lock()
	defer mu.Unlock()

	models, err := readModels()
	if err != nil {
		return nil, err
	}

	// Dedup check.
	for i := range models {
		if models[i].Model == model && models[i].Provider == provider {
			return &models[i], nil
		}
	}

	entry := SavedModel{
		ID:        newUUID(),
		Name:      name,
		Provider:  provider,
		Model:     model,
		BaseURL:   baseURL,
		CreatedAt: nowMs(),
	}
	models = append(models, entry)

	if err := writeModels(models); err != nil {
		return nil, err
	}
	return &entry, nil
}

// Remove deletes the model with the given ID.  Returns an error if no such ID
// exists.
func Remove(id string) error {
	mu.Lock()
	defer mu.Unlock()

	models, err := readModels()
	if err != nil {
		return err
	}

	filtered := models[:0]
	found := false
	for _, m := range models {
		if m.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}
	if !found {
		return fmt.Errorf("models: id %q not found", id)
	}

	return writeModels(filtered)
}

// Update applies a patch map to the model with the given ID.
// Recognised keys: "name", "provider", "model", "base_url" (and the camelCase
// variants "baseUrl" / "baseURL" are normalised automatically).
func Update(id string, fields map[string]string) error {
	mu.Lock()
	defer mu.Unlock()

	models, err := readModels()
	if err != nil {
		return err
	}

	idx := -1
	for i := range models {
		if models[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("models: id %q not found", id)
	}

	for k, v := range fields {
		switch strings.ToLower(k) {
		case "name":
			models[idx].Name = v
		case "provider":
			models[idx].Provider = v
		case "model":
			models[idx].Model = v
		case "base_url", "baseurl":
			models[idx].BaseURL = v
		}
	}

	return writeModels(models)
}

// remoteModelsResponse is the OpenAI-compatible /models response envelope.
type remoteModelsResponse struct {
	Data   []remoteModel `json:"data"`
	Models []remoteModel `json:"models"`
}

type remoteModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SyncRemote fetches the /models endpoint at baseURL and adds any models not
// already in the library under the given provider.  It returns the full list
// after the sync (matching the TypeScript syncRemoteModels return value).
//
// The apiKey is sent as a Bearer token when non-empty.
func SyncRemote(provider, baseURL, apiKey string) ([]SavedModel, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("models: sync remote build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("models: sync remote request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models: sync remote HTTP %d", resp.StatusCode)
	}

	var envelope remoteModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("models: sync remote decode: %w", err)
	}

	remoteList := envelope.Data
	if len(remoteList) == 0 {
		remoteList = envelope.Models
	}

	mu.Lock()
	defer mu.Unlock()

	existing, err := readModels()
	if err != nil {
		return nil, err
	}

	added := false
	for _, remote := range remoteList {
		found := false
		for _, m := range existing {
			if m.Model == remote.ID && m.Provider == provider {
				found = true
				break
			}
		}
		if found {
			continue
		}
		displayName := remote.Name
		if displayName == "" {
			displayName = remote.ID
		}
		existing = append(existing, SavedModel{
			ID:        newUUID(),
			Name:      displayName,
			Provider:  provider,
			Model:     remote.ID,
			BaseURL:   baseURL,
			CreatedAt: nowMs(),
		})
		added = true
	}

	if added {
		if err := writeModels(existing); err != nil {
			return nil, err
		}
	}
	return existing, nil
}
