// Package persona manages the SOUL.md persona file for a profile.
//
// SOUL.md contains the system-prompt persona for the AI agent.  When the file
// is absent, Read returns the built-in default persona rather than an error so
// that callers always get a usable string.
package persona

import (
	"fmt"
	"os"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// defaultPersona is the canonical Pan persona restored by Reset.
const defaultPersona = `You are Pan, a helpful AI assistant. You are friendly, knowledgeable, and always eager to help.

You communicate clearly and concisely. When asked to perform tasks, you think step-by-step and explain your reasoning. You are honest about your limitations and ask for clarification when needed.

You strive to be helpful while being safe and responsible. You respect the user's privacy and handle sensitive information carefully.`

// Read returns the contents of SOUL.md for the given profile.
// If the file does not exist the default persona is returned instead of an
// error, matching the behaviour of the original TypeScript implementation.
func Read(profile string) (string, error) {
	data, err := os.ReadFile(paths.SoulFile(profile))
	if err != nil {
		if os.IsNotExist(err) {
			return defaultPersona, nil
		}
		return "", fmt.Errorf("persona: read SOUL.md: %w", err)
	}
	return string(data), nil
}

// Write saves content to SOUL.md for the given profile.
func Write(content, profile string) error {
	if err := os.WriteFile(paths.SoulFile(profile), []byte(content), 0o600); err != nil {
		return fmt.Errorf("persona: write SOUL.md: %w", err)
	}
	return nil
}

// Reset writes the default persona to SOUL.md and returns it.
func Reset(profile string) error {
	return Write(defaultPersona, profile)
}
