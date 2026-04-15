package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ---------------------------------------------------------------------------
// config.yaml reading and writing
// ---------------------------------------------------------------------------

// GetValue reads a top-level scalar value from the YAML file at path using
// regex extraction.  It handles both quoted and unquoted values and ignores
// inline comments.
//
// The regex matches lines of the form:
//
//	<optional-whitespace><key>:<optional-whitespace><optional-quote><value><optional-quote>
//
// Returns ("", ErrNotFound) when the file does not exist or the key is absent.
func GetValue(path, key string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("config.GetValue %q: file not found", key)
	}
	if err != nil {
		return "", fmt.Errorf("config.GetValue %q: %w", key, err)
	}

	re := regexp.MustCompile(
		`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*["']?([^"'\n#]+)["']?`,
	)
	m := re.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("config.GetValue: key %q not found in %s", key, path)
	}
	return strings.TrimSpace(string(m[1])), nil
}

// SetValue replaces the value of a top-level key in the YAML file at path.
//
// If the key already exists (even if commented out) the line is updated
// in-place, wrapping the new value in double quotes.  The file is not created
// if it does not exist — the caller should ensure it exists beforehand (the
// TypeScript implementation has the same behaviour).
func SetValue(path, key, value string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// Match TypeScript: silently return when the file doesn't exist.
		return nil
	}
	if err != nil {
		return fmt.Errorf("config.SetValue %q: %w", key, err)
	}

	re := regexp.MustCompile(
		`(?m)^(\s*#?\s*` + regexp.QuoteMeta(key) + `:\s*)["']?[^"'\n#]*["']?`,
	)

	content := string(data)
	if re.MatchString(content) {
		content = re.ReplaceAllString(content, `${1}"`+escapeReplacement(value)+`"`)
	}
	// If the key was not found, the file is left unchanged (same as TS).

	return safeWriteFile(path, content)
}

// GetProfileValue is a convenience wrapper resolving the config.yaml path for
// a profile via paths.ConfigFile before calling GetValue.
func GetProfileValue(profile, key string) (string, error) {
	return GetValue(paths.ConfigFile(profile), key)
}

// SetProfileValue is a convenience wrapper resolving the config.yaml path for
// a profile via paths.ConfigFile before calling SetValue.
func SetProfileValue(profile, key, value string) error {
	return SetValue(paths.ConfigFile(profile), key, value)
}

// EnsureValue updates the key in place if present, or appends it to the end
// of the file if absent. Unlike SetValue, this guarantees the key exists
// after the call returns (assuming the file is writable). Used for 0.4.0+
// keys that may not have been seeded by the setup wizard — without append
// semantics, runtime config changes silently fail to persist.
//
// If the file does not exist, it is created with a single key:value line.
func EnsureValue(path, key, value string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// Create a minimal file with just this key.
		return safeWriteFile(path, fmt.Sprintf("%s: %q\n", key, value))
	}
	if err != nil {
		return fmt.Errorf("config.EnsureValue %q: %w", key, err)
	}

	re := regexp.MustCompile(
		`(?m)^(\s*#?\s*` + regexp.QuoteMeta(key) + `:\s*)["']?[^"'\n#]*["']?`,
	)
	content := string(data)
	if re.MatchString(content) {
		content = re.ReplaceAllString(content, `${1}"`+escapeReplacement(value)+`"`)
	} else {
		// Append. Ensure exactly one trailing newline before the append
		// so the file remains well-formed regardless of prior EOF state.
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += fmt.Sprintf("%s: %q\n", key, value)
	}
	return safeWriteFile(path, content)
}

// EnsureProfileValue resolves the profile's config.yaml path and calls
// EnsureValue. Prefer this over SetProfileValue when the key might not
// exist yet — runtime-toggle keys that default to a compiled-in value
// (e.g. office.engine) are the primary use case.
func EnsureProfileValue(profile, key, value string) error {
	return EnsureValue(paths.ConfigFile(profile), key, value)
}

// escapeReplacement prevents literal "$" in value from being interpreted as
// regexp back-references by ReplaceAllString.
func escapeReplacement(s string) string {
	return strings.ReplaceAll(s, "$", "$$")
}
