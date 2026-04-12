package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ---------------------------------------------------------------------------
// Credential pool (auth.json)
// ---------------------------------------------------------------------------

// Credential represents a single API credential entry stored in auth.json.
type Credential struct {
	// Key is the API key or token value.
	Key string `json:"key"`
	// Label is a human-readable name for this credential (e.g. "Work account").
	Label string `json:"label"`
}

// authStore is the top-level structure of auth.json.
type authStore struct {
	CredentialPool map[string][]Credential `json:"credential_pool,omitempty"`
	// Preserve any other top-level keys that may be in the file.
	Extra map[string]json.RawMessage `json:"-"`
}

// readAuthStore reads and parses auth.json.  Returns an empty store (not an
// error) when the file is absent or cannot be parsed.
func readAuthStore() (authStore, error) {
	p := paths.AuthFile()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return authStore{CredentialPool: make(map[string][]Credential)}, nil
	}
	if err != nil {
		return authStore{CredentialPool: make(map[string][]Credential)},
			fmt.Errorf("config.readAuthStore: %w", err)
	}

	// First unmarshal into a raw map to preserve unknown keys.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// Corrupt JSON: return empty store gracefully (same as TS try/catch {}).
		return authStore{CredentialPool: make(map[string][]Credential)}, nil
	}

	store := authStore{
		CredentialPool: make(map[string][]Credential),
		Extra:          make(map[string]json.RawMessage),
	}

	for k, v := range raw {
		if k == "credential_pool" {
			if err := json.Unmarshal(v, &store.CredentialPool); err != nil {
				// Malformed pool — treat as empty.
				store.CredentialPool = make(map[string][]Credential)
			}
		} else {
			store.Extra[k] = v
		}
	}

	return store, nil
}

// writeAuthStore serialises the store back to auth.json, preserving any extra
// top-level keys that were in the file.
func writeAuthStore(store authStore) error {
	// Re-merge extra keys + credential_pool into a single map so the file
	// round-trips cleanly without losing other data.
	out := make(map[string]any, len(store.Extra)+1)
	for k, v := range store.Extra {
		out[k] = v
	}
	out["credential_pool"] = store.CredentialPool

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("config.writeAuthStore: %w", err)
	}
	return safeWriteFile(paths.AuthFile(), string(data))
}

// GetCredentialPool returns the full credential pool from auth.json.
// On any read or parse error it returns an empty map.
func GetCredentialPool() map[string][]Credential {
	store, _ := readAuthStore()
	if store.CredentialPool == nil {
		return make(map[string][]Credential)
	}
	return store.CredentialPool
}

// SetCredentialPool replaces the credential list for provider in auth.json.
// Other providers' credentials and any unrelated auth.json keys are preserved.
func SetCredentialPool(provider string, creds []Credential) error {
	store, err := readAuthStore()
	if err != nil {
		return fmt.Errorf("config.SetCredentialPool: %w", err)
	}
	if store.CredentialPool == nil {
		store.CredentialPool = make(map[string][]Credential)
	}
	store.CredentialPool[provider] = creds
	return writeAuthStore(store)
}
