package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetModelConfigCreatesMissingFile reproduces the 0.4.0 regression: on
// a fresh install (no config.yaml for the profile), the old SetModelConfig
// silently early-returned on os.IsNotExist — the UI's PUT /v1/config looked
// successful but nothing was written, GET /v1/config returned the empty
// defaults, and the in-memory LLM client got re-set to {"",""} on UI
// re-hydration, breaking chat after the first message.
func TestSetModelConfigCreatesMissingFile(t *testing.T) {
	t.Setenv("PAN_AGENT_HOME", t.TempDir())
	// Cache would hide file-level changes across subtests.
	invalidatePrefix("mc:")

	profile := "default"
	if err := SetModelConfig("regolo", "gpt-oss-120b", "https://api.regolo.ai/v1", profile); err != nil {
		t.Fatalf("SetModelConfig on missing file: %v", err)
	}

	invalidatePrefix("mc:")
	got := GetModelConfig(profile)
	if got.Provider != "regolo" {
		t.Errorf("Provider = %q, want %q", got.Provider, "regolo")
	}
	if got.Model != "gpt-oss-120b" {
		t.Errorf("Model = %q, want %q", got.Model, "gpt-oss-120b")
	}
	if got.BaseURL != "https://api.regolo.ai/v1" {
		t.Errorf("BaseURL = %q, want %q", got.BaseURL, "https://api.regolo.ai/v1")
	}
}

// TestSetModelConfigWritesCustomForRegolo verifies the UI-to-CLI provider
// name mapping is applied even on the fresh-file write path.
func TestSetModelConfigWritesCustomForRegolo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PAN_AGENT_HOME", home)
	invalidatePrefix("mc:")

	if err := SetModelConfig("regolo", "qwen3-coder-next", "https://api.regolo.ai/v1", "default"); err != nil {
		t.Fatalf("SetModelConfig: %v", err)
	}

	// The on-disk provider should read "custom" (agent CLI vocabulary);
	// GetModelConfig reverse-maps it back to "regolo" via regoloPattern.
	raw, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatalf("read back config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), `provider: "custom"`) {
		t.Errorf("config.yaml missing provider: \"custom\"; got:\n%s", raw)
	}
	if !strings.Contains(string(raw), `streaming: true`) {
		t.Errorf("config.yaml missing streaming: true; got:\n%s", raw)
	}
}
