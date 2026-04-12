// Package config manages .env files, config.yaml, model configuration,
// credential pools, and platform toggles for pan-agent profiles.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// cacheTTL is the duration after which a cached value is considered stale.
const cacheTTL = 5 * time.Second

// cacheEntry holds a cached value and the time it was stored.
type cacheEntry struct {
	data      any
	timestamp time.Time
}

// cache is the global in-memory store shared across all config functions.
var (
	cacheMu sync.RWMutex
	cache   = make(map[string]cacheEntry)
)

// getCached retrieves a typed value from the cache if it exists and is fresh.
// Returns the zero value and false when the entry is absent or expired.
func getCached[T any](key string) (T, bool) {
	cacheMu.RLock()
	entry, ok := cache[key]
	cacheMu.RUnlock()

	var zero T
	if !ok {
		return zero, false
	}
	if time.Since(entry.timestamp) > cacheTTL {
		cacheMu.Lock()
		delete(cache, key)
		cacheMu.Unlock()
		return zero, false
	}
	v, ok := entry.data.(T)
	return v, ok
}

// setCache stores a value in the cache under the given key.
func setCache(key string, data any) {
	cacheMu.Lock()
	cache[key] = cacheEntry{data: data, timestamp: time.Now()}
	cacheMu.Unlock()
}

// invalidatePrefix deletes all cache entries whose key starts with prefix.
func invalidatePrefix(prefix string) {
	cacheMu.Lock()
	for k := range cache {
		if strings.HasPrefix(k, prefix) {
			delete(cache, k)
		}
	}
	cacheMu.Unlock()
}

// ---------------------------------------------------------------------------
// .env parsing and writing
// ---------------------------------------------------------------------------

// ReadEnv parses the .env file at path and returns a map of key→value pairs.
//
// Parsing rules (matching the TypeScript reference implementation):
//   - Lines that start with '#' (after trimming) are skipped.
//   - Lines without '=' are skipped.
//   - The first '=' separates the key from the value.
//   - Values wrapped in matching double or single quotes are unquoted.
//   - Empty values (after unquoting) are omitted from the result.
//
// Results are cached for cacheTTL. The cache key is derived from path.
func ReadEnv(path string) (map[string]string, error) {
	cacheKey := "env:" + path
	if v, ok := getCached[map[string]string](cacheKey); ok {
		return v, nil
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		empty := map[string]string{}
		setCache(cacheKey, empty)
		return empty, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config.ReadEnv: %w", err)
	}

	result := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			continue
		}

		eqIdx := strings.Index(trimmed, "=")
		key := strings.TrimSpace(trimmed[:eqIdx])
		value := strings.TrimSpace(trimmed[eqIdx+1:])

		// Strip matching surrounding quotes.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if value != "" {
			result[key] = value
		}
	}

	setCache(cacheKey, result)
	return result, nil
}

// SetEnvValue updates or appends key=value in the .env file at path.
//
// Behaviour:
//   - If the file does not exist it is created with a single "key=value\n" line.
//   - If a line matching "^#?\s*<key>\s*=" already exists it is replaced
//     (including commented-out entries).
//   - Otherwise the pair is appended on a new line.
//
// The cache entry for path is invalidated regardless of success.
func SetEnvValue(path, key, value string) error {
	invalidatePrefix("env:" + path)

	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return safeWriteFile(path, key+"="+value+"\n")
	}
	if err != nil {
		return fmt.Errorf("config.SetEnvValue: %w", err)
	}

	pattern := regexp.MustCompile(`(?i)^#?\s*` + regexp.QuoteMeta(key) + `\s*=`)
	lines := strings.Split(string(existing), "\n")
	found := false

	for i, line := range lines {
		if pattern.MatchString(line) {
			lines[i] = key + "=" + value
			found = true
			break
		}
	}

	if !found {
		lines = append(lines, key+"="+value)
	}

	return safeWriteFile(path, strings.Join(lines, "\n"))
}

// ReadProfileEnv is a convenience wrapper that uses paths.EnvFile to resolve
// the .env path for a given profile name.
func ReadProfileEnv(profile string) (map[string]string, error) {
	return ReadEnv(paths.EnvFile(profile))
}

// SetProfileEnvValue is a convenience wrapper that resolves the .env path for
// a profile before calling SetEnvValue.
func SetProfileEnvValue(profile, key, value string) error {
	return SetEnvValue(paths.EnvFile(profile), key, value)
}
