// Package paths resolves all filesystem paths for the pan-agent.
// Paths are platform-specific:
//
//   - Windows:  %LOCALAPPDATA%\pan-agent
//   - macOS:    ~/Library/Application Support/pan-agent
//   - Linux:    ~/.local/share/pan-agent
//
// Directories are created lazily on first access via MkdirAll.
package paths

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

const agentName = "pan-agent"

// profileNameRe restricts profile names to a safe allowlist: letters, digits,
// dash, and underscore; 1–64 chars; must start with an alphanumeric. Anything
// else is rejected to prevent path traversal when a profile name flows into
// filesystem paths.
var profileNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// ValidateProfile reports whether profile is safe to use as a directory name.
// Empty or "default" are allowed (they resolve to AgentHome).
func ValidateProfile(profile string) bool {
	if profile == "" || profile == "default" {
		return true
	}
	return profileNameRe.MatchString(profile)
}

// agentHomeOnce ensures agentHome is computed exactly once.
var (
	agentHomeOnce  sync.Once
	agentHomeValue string
)

// AgentHome returns the root directory for all pan-agent data.
//
//   - Windows: %LOCALAPPDATA%\pan-agent
//   - macOS:   ~/Library/Application Support/pan-agent
//   - Linux:   ~/.local/share/pan-agent
//
// Test escape hatch: if PAN_AGENT_HOME is set, its value is returned
// verbatim without caching. This lets tests use t.Setenv + t.TempDir()
// for isolation; without it, tests that write through
// config.SetProfileEnvValue or memory.AddEntry would pollute the real
// LOCALAPPDATA directory (we got bitten by exactly this —
// TestConfigMasksSecrets overwrote the user's real REGOLO_API_KEY, and
// memory tests left test_* profile directories behind).
func AgentHome() string {
	if override := os.Getenv("PAN_AGENT_HOME"); override != "" {
		mustMkdir(override)
		return override
	}
	agentHomeOnce.Do(func() {
		var base string

		if runtime.GOOS == "windows" {
			// os.UserConfigDir() returns %AppData% (Roaming) on Windows; we
			// want Local instead, so read LOCALAPPDATA directly.
			local := os.Getenv("LOCALAPPDATA")
			if local == "" {
				// Fallback: use UserConfigDir if LOCALAPPDATA is unset.
				var err error
				local, err = os.UserConfigDir()
				if err != nil {
					local = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
				}
			}
			base = local
		} else {
			// os.UserConfigDir() returns:
			//   macOS: ~/Library/Application Support
			//   Linux: $XDG_CONFIG_HOME or ~/.config
			// For Linux we prefer ~/.local/share per the XDG Base Directory spec.
			if runtime.GOOS == "linux" {
				xdgData := os.Getenv("XDG_DATA_HOME")
				if xdgData != "" {
					base = xdgData
				} else {
					home, err := os.UserHomeDir()
					if err != nil {
						home = os.Getenv("HOME")
					}
					base = filepath.Join(home, ".local", "share")
				}
			} else {
				// macOS and other Unix-likes: use UserConfigDir
				// ~/Library/Application Support on macOS
				var err error
				base, err = os.UserConfigDir()
				if err != nil {
					home, _ := os.UserHomeDir()
					base = filepath.Join(home, "Library", "Application Support")
				}
			}
		}

		agentHomeValue = filepath.Join(base, agentName)
		mustMkdir(agentHomeValue)
	})
	return agentHomeValue
}

// ProfileHome returns the root directory for a named profile.
// An empty string or "default" resolves to AgentHome.
//
// Profile names must match profileNameRe (alphanumeric/dash/underscore, ≤64
// chars). Invalid names fall back to AgentHome so user-supplied taint cannot
// produce a path outside the agent's data directory, even if a caller forgets
// to call ValidateProfile first. The filepath.Rel containment check is a
// belt-and-braces guard in case the regex is ever weakened.
func ProfileHome(profile string) string {
	if !ValidateProfile(profile) {
		return AgentHome()
	}
	if profile == "" || profile == "default" {
		return AgentHome()
	}
	base := filepath.Join(AgentHome(), "profiles")
	dir := filepath.Join(base, profile)
	rel, err := filepath.Rel(base, dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.ContainsRune(rel, filepath.Separator) {
		return AgentHome()
	}
	mustMkdir(dir)
	return dir
}

// EnvFile returns the path to the .env file for the given profile.
func EnvFile(profile string) string {
	return filepath.Join(ProfileHome(profile), ".env")
}

// ConfigFile returns the path to config.yaml for the given profile.
func ConfigFile(profile string) string {
	return filepath.Join(ProfileHome(profile), "config.yaml")
}

// MemoryFile returns the path to MEMORY.md for the given profile.
func MemoryFile(profile string) string {
	return filepath.Join(ProfileHome(profile), "MEMORY.md")
}

// UserFile returns the path to USER.md for the given profile.
func UserFile(profile string) string {
	return filepath.Join(ProfileHome(profile), "USER.md")
}

// SoulFile returns the path to SOUL.md for the given profile.
func SoulFile(profile string) string {
	return filepath.Join(ProfileHome(profile), "SOUL.md")
}

// StateDB returns the path to the state.db SQLite database.
// This lives in DataDir, which equals AgentHome.
func StateDB() string {
	return filepath.Join(dataDir(), "state.db")
}

// ModelsFile returns the path to models.json.
func ModelsFile() string {
	return filepath.Join(dataDir(), "models.json")
}

// AuthFile returns the path to auth.json.
func AuthFile() string {
	return filepath.Join(dataDir(), "auth.json")
}

// PidFile returns the path to the running gateway's PID file.
//
// Written by the gateway at startup and read by:
//   - chaos tests (M5-C2) to target a known parent PID for the
//     parent-watcher kill scenario;
//   - `pan-agent doctor` (M6-C1) for the --switch-engine flag to
//     confirm a running instance before POSTing to /v1/office/engine.
//
// Sits at AgentHome root (not profile-scoped) because only one
// gateway runs per host — same scope as state.db.
func PidFile() string {
	return filepath.Join(dataDir(), "pan-agent.pid")
}

// CSPViolationsLog returns the path to the CSP violations log file
// that the /v1/office/csp-report endpoint appends to. Read by
// `pan-agent doctor --csp-violations` (M6-C1).
//
// Prior to this helper, the path was duplicated between the writer
// (internal/gateway/office_csp.go) and the would-be reader. Centralising
// it here keeps the two commits from drifting.
func CSPViolationsLog() string {
	return filepath.Join(dataDir(), "csp-violations.log")
}

// SkillsDir returns the path to the installed skills directory.
// Structure: <AgentHome>/skills/<category>/<skill-name>/SKILL.md
func SkillsDir() string {
	dir := filepath.Join(AgentHome(), "skills")
	mustMkdir(dir)
	return dir
}

// ProfileSkillsDir returns the skills directory for a named profile.
func ProfileSkillsDir(profile string) string {
	dir := filepath.Join(ProfileHome(profile), "skills")
	mustMkdir(dir)
	return dir
}

// BundledSkillsDir returns the path to the bundled skills shipped with the
// binary.  Bundled skills live next to the executable under skills/.
// Returns an empty string if the directory does not exist.
func BundledSkillsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Join(filepath.Dir(exe), "skills")
	return dir
}

// CronJobsFile returns the path to cron/jobs.json.
func CronJobsFile() string {
	dir := filepath.Join(AgentHome(), "cron")
	mustMkdir(dir)
	return filepath.Join(dir, "jobs.json")
}

// LogsDir returns the path to the logs directory.
func LogsDir() string {
	dir := filepath.Join(AgentHome(), "logs")
	mustMkdir(dir)
	return dir
}

// CacheDir returns the path to the cache directory.
func CacheDir() string {
	dir := filepath.Join(AgentHome(), "cache")
	mustMkdir(dir)
	return dir
}

// BrowserProfile returns the path to the Chromium user-data directory
// used by the browser automation tool. Ephemeral in v0.4.5 (cleared on
// agent exit); persistent from v0.5.0 onward.
func BrowserProfile() string {
	dir := filepath.Join(dataDir(), "browser-profile")
	mustMkdir(dir)
	return dir
}

// Claw3dDir returns the path to the pan-office / Claw3D directory.
func Claw3dDir() string {
	dir := filepath.Join(AgentHome(), "pan-office")
	mustMkdir(dir)
	return dir
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// dataDir returns the data directory, which is AgentHome by design.
// It is kept as a named function so that the separation between ConfigDir and
// DataDir is explicit and easy to change in the future.
func dataDir() string {
	return AgentHome()
}

// mustMkdir creates dir (and any missing parents) with mode 0700.
// It panics only if the call fails for a reason other than "already exists",
// which os.MkdirAll handles gracefully already.
func mustMkdir(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		// Non-fatal: callers that need the directory to exist will fail on
		// their own when they try to open/create files inside it. Panic here
		// would crash the agent on read-only filesystems for no benefit.
		_ = err
	}
}
