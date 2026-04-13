package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
)

// ProfileInfo holds metadata about a single profile for the API response.
type ProfileInfo struct {
	Name           string `json:"name"`
	IsDefault      bool   `json:"isDefault"`
	IsActive       bool   `json:"isActive"`
	Model          string `json:"model"`
	Provider       string `json:"provider"`
	HasEnv         bool   `json:"hasEnv"`
	HasSoul        bool   `json:"hasSoul"`
	SkillCount     int    `json:"skillCount"`
	GatewayRunning bool   `json:"gatewayRunning"`
}

// ListProfiles returns metadata for all profiles.
// The "default" profile is always first.
func ListProfiles(activeProfile string) []ProfileInfo {
	names := listProfileNames()
	profiles := make([]ProfileInfo, 0, len(names))
	for _, name := range names {
		mc := GetModelConfig(name)
		env, _ := ReadProfileEnv(name)
		hasEnv := len(env) > 0
		_, soulErr := os.Stat(paths.SoulFile(name))
		hasSoul := soulErr == nil
		profiles = append(profiles, ProfileInfo{
			Name:       name,
			IsDefault:  name == "default",
			IsActive:   name == activeProfile || (name == "default" && (activeProfile == "" || activeProfile == "default")),
			Model:      mc.Model,
			Provider:   mc.Provider,
			HasEnv:     hasEnv,
			HasSoul:    hasSoul,
			SkillCount: countSkills(name),
		})
	}
	return profiles
}

func listProfileNames() []string {
	names := []string{"default"}
	profilesDir := filepath.Join(paths.AgentHome(), "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return names
	}
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func countSkills(profile string) int {
	dir := filepath.Join(paths.ProfileHome(profile), "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	return count
}

var validProfileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// CreateProfile creates a new named profile directory.
// If cloneFrom is non-empty, .env and config.yaml are copied from that profile.
func CreateProfile(name, cloneFrom string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "default" {
		return fmt.Errorf("invalid profile name %q", name)
	}
	if !validProfileName.MatchString(name) {
		return fmt.Errorf("profile name must be alphanumeric with hyphens/underscores")
	}
	dir := filepath.Join(paths.AgentHome(), "profiles", name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("profile %q already exists", name)
	}
	// paths.ProfileHome creates the dir via mustMkdir.
	_ = paths.ProfileHome(name)

	if cloneFrom != "" {
		copyFileIfExists(paths.EnvFile(cloneFrom), paths.EnvFile(name))
		copyFileIfExists(paths.ConfigFile(cloneFrom), paths.ConfigFile(name))
	}
	return nil
}

// DeleteProfile removes a named profile directory.
func DeleteProfile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == "default" {
		return fmt.Errorf("cannot delete the default profile")
	}
	if !validProfileName.MatchString(name) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	dir := filepath.Join(paths.AgentHome(), "profiles", name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("profile %q not found", name)
	}
	invalidatePrefix("env:" + paths.EnvFile(name))
	return os.RemoveAll(dir)
}

func copyFileIfExists(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = safeWriteFile(dst, string(data))
}
