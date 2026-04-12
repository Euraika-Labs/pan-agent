package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ---------------------------------------------------------------------------
// Platform toggles (config.yaml)
// ---------------------------------------------------------------------------

// SupportedPlatforms is the authoritative list of platform names that Pan
// Agent recognises.  SetPlatformEnabled silently ignores names not in this
// list (same as the TypeScript implementation).
var SupportedPlatforms = []string{
	"telegram",
	"discord",
	"slack",
	"whatsapp",
	"signal",
}

// isSupportedPlatform returns true when name is in SupportedPlatforms.
func isSupportedPlatform(name string) bool {
	for _, p := range SupportedPlatforms {
		if p == name {
			return true
		}
	}
	return false
}

// GetPlatformEnabled reads the enabled/disabled state for all supported
// platforms from the config.yaml of the given profile.
//
// A platform is considered disabled (false) when:
//   - The config file does not exist.
//   - The platform block is absent from config.yaml.
//   - The "enabled:" field is set to anything other than "true".
func GetPlatformEnabled(profile string) map[string]bool {
	result := make(map[string]bool, len(SupportedPlatforms))

	data, err := os.ReadFile(paths.ConfigFile(profile))
	if err != nil {
		for _, p := range SupportedPlatforms {
			result[p] = false
		}
		return result
	}
	content := string(data)

	for _, platform := range SupportedPlatforms {
		// Match the two-line block:
		//   <indent>telegram:
		//   <deeper-indent>enabled: true|false
		re := regexp.MustCompile(
			`(?m)^[ \t]+` + regexp.QuoteMeta(platform) + `:\s*\n[ \t]+enabled:\s*(true|false)`,
		)
		m := re.FindStringSubmatch(content)
		result[platform] = m != nil && m[1] == "true"
	}

	return result
}

// SetPlatformEnabled updates the enabled flag for a single platform in the
// config.yaml of the given profile.
//
// Behaviour:
//   - Silently returns nil for unsupported platform names.
//   - Silently returns nil when the config file does not exist.
//   - Updates the existing block in-place if it already exists.
//   - Appends a new block under the platforms: section if it is missing
//     (inserting the platforms: section itself if necessary).
func SetPlatformEnabled(platform string, enabled bool, profile string) error {
	if !isSupportedPlatform(platform) {
		return nil
	}

	configFile := paths.ConfigFile(profile)
	data, err := os.ReadFile(configFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config.SetPlatformEnabled: %w", err)
	}
	content := string(data)
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}

	// Try to update an existing block.
	existingRe := regexp.MustCompile(
		`(?m)^([ \t]+` + regexp.QuoteMeta(platform) + `:\s*\n[ \t]+enabled:\s*)(?:true|false)`,
	)
	if existingRe.MatchString(content) {
		content = existingRe.ReplaceAllString(content, "${1}"+enabledStr)
		return safeWriteFile(configFile, content)
	}

	// Platform block absent — insert it.
	entry := fmt.Sprintf("  %s:\n    enabled: %s\n", platform, enabledStr)

	platformsIdx := strings.Index(content, "\nplatforms:")
	if platformsIdx == -1 {
		// No platforms section at all — append one.
		content += "\nplatforms:\n" + entry
		return safeWriteFile(configFile, content)
	}

	// Insert at the end of the existing platforms: block.
	// "end of block" = first line after "platforms:" that is non-empty and
	// non-indented (i.e. a new top-level key).
	afterNewline := platformsIdx + 1 // index of the 'p' in "platforms:"
	rest := content[afterNewline:]
	lines := strings.Split(rest, "\n")

	// lines[0] == "platforms:"
	insertOffset := afterNewline + len(lines[0]) + 1 // skip past "platforms:\n"

	for _, line := range lines[1:] {
		if line == "" || len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			insertOffset += len(line) + 1
		} else {
			break
		}
	}

	content = content[:insertOffset] + entry + content[insertOffset:]
	return safeWriteFile(configFile, content)
}
