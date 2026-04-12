package llm

import "fmt"

// ProviderBaseURLs maps provider names to their default OpenAI-compatible base URLs.
var ProviderBaseURLs = map[string]string{
	"openai":     "https://api.openai.com/v1",
	"anthropic":  "https://api.anthropic.com/v1",
	"regolo":     "https://api.regolo.ai/v1",
	"ollama":     "http://localhost:11434/v1",
	"lmstudio":   "http://localhost:1234/v1",
	"vllm":       "http://localhost:8000/v1",
	"llamacpp":   "http://localhost:8080/v1",
	"openrouter": "https://openrouter.ai/api/v1",
	"groq":       "https://api.groq.com/openai/v1",
}

// BaseURL returns the default base URL for the named provider.
// It returns an error if the provider is unknown.
func BaseURL(provider string) (string, error) {
	u, ok := ProviderBaseURLs[provider]
	if !ok {
		return "", fmt.Errorf("llm: unknown provider %q", provider)
	}
	return u, nil
}

// NewClientForProvider is a convenience constructor that looks up the default
// base URL for provider and calls NewClient. Pass an empty apiKey for local
// providers that do not require authentication.
func NewClientForProvider(provider, apiKey, model string) (*Client, error) {
	u, err := BaseURL(provider)
	if err != nil {
		return nil, err
	}
	return NewClient(u, apiKey, model), nil
}

// Providers returns the sorted list of known provider names.
func Providers() []string {
	names := make([]string, 0, len(ProviderBaseURLs))
	for k := range ProviderBaseURLs {
		names = append(names, k)
	}
	// Stable sort so callers get consistent output.
	sortStrings(names)
	return names
}

// sortStrings is a minimal insertion sort to avoid importing "sort".
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
