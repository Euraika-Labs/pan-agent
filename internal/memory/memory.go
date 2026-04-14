// Package memory manages persistent MEMORY.md and USER.md files for a profile.
//
// MEMORY.md stores a list of free-text entries separated by the delimiter "\n§\n"
// and is capped at 2200 characters.  USER.md stores a single block of user
// profile text capped at 1375 characters.
package memory

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

const (
	entryDelimiter  = "\n§\n"
	memoryCharLimit = 2200
	userCharLimit   = 1375
)

// EntryDelimiter is the memory-file separator between entries. Exported so
// the gateway's handleMemoryGet can reconstruct the raw file body from the
// parsed entries list when composing its {memory, user, stats} response.
const EntryDelimiter = entryDelimiter

// MemoryState holds the current contents and metadata of both memory files.
//
// The raw state returned from here goes to the CLI's `pan-agent doctor` output
// and to tests. For the HTTP API, the gateway composes a different shape —
// {memory, user, stats} — via handleMemoryGet using these fields plus a DB
// session/message count. See internal/gateway/routes.go:handleMemoryGet.
type MemoryState struct {
	Entries       []string `json:"entries"`
	CharCount     int      `json:"charCount"`
	CharLimit     int      `json:"charLimit"` // always 2200
	UserProfile   string   `json:"userProfile"`
	UserCharCount int      `json:"userCharCount"`
	UserCharLimit int      `json:"userCharLimit"` // always 1375
}

// ReadMemory reads MEMORY.md and USER.md for the given profile and returns a
// MemoryState.  Missing files are treated as empty — no error is returned.
func ReadMemory(profile string) (*MemoryState, error) {
	memContent, err := readFileSafe(paths.MemoryFile(profile))
	if err != nil {
		return nil, fmt.Errorf("memory: read MEMORY.md: %w", err)
	}

	userContent, err := readFileSafe(paths.UserFile(profile))
	if err != nil {
		return nil, fmt.Errorf("memory: read USER.md: %w", err)
	}

	return &MemoryState{
		Entries:       parseEntries(memContent),
		CharCount:     len(memContent),
		CharLimit:     memoryCharLimit,
		UserProfile:   userContent,
		UserCharCount: len(userContent),
		UserCharLimit: userCharLimit,
	}, nil
}

// AddEntry appends a new entry to MEMORY.md for the given profile.
// Returns an error if the resulting file would exceed the character limit.
func AddEntry(content, profile string) error {
	filePath := paths.MemoryFile(profile)

	existing, err := readFileSafe(filePath)
	if err != nil {
		return fmt.Errorf("memory: read MEMORY.md: %w", err)
	}

	entries := parseEntries(existing)
	entries = append(entries, strings.TrimSpace(content))
	newContent := serialize(entries)

	if len(newContent) > memoryCharLimit {
		return fmt.Errorf("memory: would exceed limit (%d/%d chars)", len(newContent), memoryCharLimit)
	}

	return writeFile(filePath, newContent)
}

// UpdateEntry replaces the entry at the given zero-based index in MEMORY.md.
// Returns an error if the index is out of range or the result would exceed the
// character limit.
func UpdateEntry(index int, content, profile string) error {
	filePath := paths.MemoryFile(profile)

	existing, err := readFileSafe(filePath)
	if err != nil {
		return fmt.Errorf("memory: read MEMORY.md: %w", err)
	}

	entries := parseEntries(existing)
	if index < 0 || index >= len(entries) {
		return errors.New("memory: entry not found")
	}

	entries[index] = strings.TrimSpace(content)
	newContent := serialize(entries)

	if len(newContent) > memoryCharLimit {
		return fmt.Errorf("memory: would exceed limit (%d/%d chars)", len(newContent), memoryCharLimit)
	}

	return writeFile(filePath, newContent)
}

// RemoveEntry deletes the entry at the given zero-based index from MEMORY.md
// and rewrites the file with the remaining entries joined by the delimiter.
// Returns an error if the index is out of range.
func RemoveEntry(index int, profile string) error {
	filePath := paths.MemoryFile(profile)

	existing, err := readFileSafe(filePath)
	if err != nil {
		return fmt.Errorf("memory: read MEMORY.md: %w", err)
	}

	entries := parseEntries(existing)
	if index < 0 || index >= len(entries) {
		return errors.New("memory: entry not found")
	}

	entries = append(entries[:index], entries[index+1:]...)
	return writeFile(filePath, serialize(entries))
}

// WriteUserProfile writes content to USER.md for the given profile.
// Returns an error if content exceeds the character limit.
func WriteUserProfile(content, profile string) error {
	if len(content) > userCharLimit {
		return fmt.Errorf("memory: user profile exceeds limit (%d/%d chars)", len(content), userCharLimit)
	}
	return writeFile(paths.UserFile(profile), content)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// readFileSafe returns the file contents, or an empty string if the file does
// not exist.  Any other OS error is returned to the caller.
func readFileSafe(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// writeFile writes content to filePath using safe 0600 permissions.
func writeFile(filePath, content string) error {
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("memory: write %s: %w", filePath, err)
	}
	return nil
}

// parseEntries splits raw file content on the entry delimiter and returns
// non-empty, trimmed strings.
func parseEntries(content string) []string {
	if strings.TrimSpace(content) == "" {
		return []string{}
	}
	parts := strings.Split(content, entryDelimiter)
	entries := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			entries = append(entries, t)
		}
	}
	return entries
}

// serialize joins entries with the delimiter, mirroring serializeEntries in
// the TypeScript original.
func serialize(entries []string) string {
	return strings.Join(entries, entryDelimiter)
}
