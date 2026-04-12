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
	"runtime"
	"sync"
)

const agentName = "pan-agent"

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
func AgentHome() string {
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
func ProfileHome(profile string) string {
	if profile == "" || profile == "default" {
		return AgentHome()
	}
	dir := filepath.Join(AgentHome(), "profiles", profile)
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

// SkillsDir returns the path to the skills directory.
func SkillsDir() string {
	dir := filepath.Join(AgentHome(), "skills")
	mustMkdir(dir)
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

// Claw3dDir returns the path to the hermes-office directory.
func Claw3dDir() string {
	dir := filepath.Join(AgentHome(), "hermes-office")
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
