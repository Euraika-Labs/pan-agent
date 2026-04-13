package config

import (
	"os"
	"regexp"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ---------------------------------------------------------------------------
// Model configuration
// ---------------------------------------------------------------------------

// ModelConfig holds the three model-related fields read from config.yaml.
type ModelConfig struct {
	// Provider is the inference provider name as surfaced in the UI.
	// "regolo" is used instead of "custom" when the base URL contains regolo.ai.
	Provider string
	// Model is the default model identifier (the "default:" key in config.yaml).
	Model string
	// BaseURL is the API base URL used by the provider.
	BaseURL string
}

// cliProviderMap translates UI provider names to the values written into
// config.yaml (for the agent CLI).
var cliProviderMap = map[string]string{
	"regolo": "custom",
}

// regoloPattern detects a Regolo base URL so we can reverse-map "custom" →
// "regolo" when reading the config.
var regoloPattern = regexp.MustCompile(`(?i)regolo\.ai`)

// GetModelConfig reads the model configuration from the config.yaml that
// belongs to profile.  Defaults to {Provider:"auto"} when the file is absent
// or a key is missing.
//
// Results are cached for cacheTTL.
func GetModelConfig(profile string) ModelConfig {
	cacheKey := "mc:" + profile
	if v, ok := getCached[ModelConfig](cacheKey); ok {
		return v
	}

	defaults := ModelConfig{Provider: "auto"}
	configFile := paths.ConfigFile(profile)

	data, err := os.ReadFile(configFile)
	if err != nil {
		return defaults
	}
	content := string(data)

	provider := extractYAMLScalar(content, "provider", defaults.Provider)
	model := extractYAMLScalar(content, "default", defaults.Model)
	baseURL := extractYAMLScalar(content, "base_url", defaults.BaseURL)

	// Reverse-map: the CLI stores "custom" for Regolo providers; translate
	// back to "regolo" for the UI dropdown.
	if provider == "custom" && regoloPattern.MatchString(baseURL) {
		provider = "regolo"
	}

	result := ModelConfig{
		Provider: provider,
		Model:    model,
		BaseURL:  baseURL,
	}
	setCache(cacheKey, result)
	return result
}

// SetModelConfig writes provider, model, and baseUrl into the config.yaml for
// profile.  It also:
//
//   - Maps "regolo" → "custom" before writing (agent CLI vocabulary).
//   - Disables smart_model_routing.enabled when it immediately follows the
//     smart_model_routing: key.
//   - Ensures streaming: true.
func SetModelConfig(provider, model, baseURL, profile string) error {
	invalidatePrefix("mc:" + profile)

	configFile := paths.ConfigFile(profile)
	data, err := os.ReadFile(configFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)

	// Translate UI provider name to agent CLI value.
	writeProvider := provider
	if mapped, ok := cliProviderMap[provider]; ok {
		writeProvider = mapped
	}

	// Update provider:
	providerRe := regexp.MustCompile(`(?m)^(\s*provider:\s*)["']?[^"'\n#]*["']?`)
	if providerRe.MatchString(content) {
		content = providerRe.ReplaceAllString(content,
			"${1}\""+escapeReplacement(writeProvider)+"\"")
	}

	// Update default: (model)
	modelRe := regexp.MustCompile(`(?m)^(\s*default:\s*)["']?[^"'\n#]*["']?`)
	if modelRe.MatchString(content) {
		content = modelRe.ReplaceAllString(content,
			"${1}\""+escapeReplacement(model)+"\"")
	}

	// Update base_url:
	baseURLRe := regexp.MustCompile(`(?m)^(\s*base_url:\s*)["']?[^"'\n#]*["']?`)
	if baseURLRe.MatchString(content) {
		content = baseURLRe.ReplaceAllString(content,
			"${1}\""+escapeReplacement(baseURL)+"\"")
	}

	// Disable smart_model_routing: the "enabled:" line that immediately
	// follows the "smart_model_routing:" key is set to false.
	smrEnabledRe := regexp.MustCompile(`(enabled:\s*)(?:true|false)`)
	lines := strings.Split(content, "\n")
	for i := 1; i < len(lines); i++ {
		if smrEnabledRe.MatchString(lines[i]) && strings.Contains(lines[i-1], "smart_model_routing") {
			lines[i] = smrEnabledRe.ReplaceAllString(lines[i], "${1}false")
		}
	}
	content = strings.Join(lines, "\n")

	// Ensure streaming: true
	streamingRe := regexp.MustCompile(`(?m)^(\s*streaming:\s*)\S+`)
	if streamingRe.MatchString(content) {
		content = streamingRe.ReplaceAllString(content, "${1}true")
	}

	return safeWriteFile(configFile, content)
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// extractYAMLScalar extracts the string value for a top-level YAML key using
// the same regex logic as GetValue, returning fallback when the key is absent.
func extractYAMLScalar(content, key, fallback string) string {
	re := regexp.MustCompile(
		`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*["']?([^"'\n#]+)["']?`,
	)
	m := re.FindStringSubmatch(content)
	if m == nil {
		return fallback
	}
	v := strings.TrimSpace(m[1])
	if v == "" {
		return fallback
	}
	return v
}
