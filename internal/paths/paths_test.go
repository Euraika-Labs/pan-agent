package paths

import (
	"strings"
	"testing"
)

func TestAgentHomeNonEmpty(t *testing.T) {
	home := AgentHome()
	if home == "" {
		t.Fatal("AgentHome() returned empty string")
	}
}

func TestAgentHomeContainsAgentName(t *testing.T) {
	home := AgentHome()
	if !strings.Contains(home, "pan-agent") {
		t.Fatalf("AgentHome() = %q, expected to contain 'pan-agent'", home)
	}
}

func TestAgentHomeIdempotent(t *testing.T) {
	h1 := AgentHome()
	h2 := AgentHome()
	if h1 != h2 {
		t.Fatalf("AgentHome() not idempotent: %q != %q", h1, h2)
	}
}

func TestProfileHomeDefault(t *testing.T) {
	// "" and "default" should both return AgentHome
	if got := ProfileHome(""); got != AgentHome() {
		t.Errorf("ProfileHome(\"\") = %q, want AgentHome() = %q", got, AgentHome())
	}
	if got := ProfileHome("default"); got != AgentHome() {
		t.Errorf("ProfileHome(\"default\") = %q, want AgentHome() = %q", got, AgentHome())
	}
}

func TestProfileHomeSubdirectory(t *testing.T) {
	profile := "testprofile"
	ph := ProfileHome(profile)
	home := AgentHome()

	if !strings.HasPrefix(ph, home) {
		t.Errorf("ProfileHome(%q) = %q, expected prefix of AgentHome %q", profile, ph, home)
	}
	if !strings.Contains(ph, profile) {
		t.Errorf("ProfileHome(%q) = %q, expected to contain profile name", profile, ph)
	}
	if !strings.Contains(ph, "profiles") {
		t.Errorf("ProfileHome(%q) = %q, expected to contain 'profiles'", profile, ph)
	}
}

func TestFileFunctionsUnderAgentHome(t *testing.T) {
	home := AgentHome()

	checks := []struct {
		name string
		got  string
	}{
		{"EnvFile", EnvFile("default")},
		{"ConfigFile", ConfigFile("default")},
		{"MemoryFile", MemoryFile("default")},
		{"UserFile", UserFile("default")},
		{"SoulFile", SoulFile("default")},
		{"StateDB", StateDB()},
		{"ModelsFile", ModelsFile()},
		{"AuthFile", AuthFile()},
		{"SkillsDir", SkillsDir()},
		{"CronJobsFile", CronJobsFile()},
		{"LogsDir", LogsDir()},
		{"CacheDir", CacheDir()},
		{"Claw3dDir", Claw3dDir()},
	}

	for _, c := range checks {
		if !strings.HasPrefix(c.got, home) {
			t.Errorf("%s = %q, expected prefix of AgentHome %q", c.name, c.got, home)
		}
	}
}

func TestFileFunctionsNamed(t *testing.T) {
	profile := "myprofile"
	ph := ProfileHome(profile)

	checks := []struct {
		name string
		got  string
	}{
		{"EnvFile", EnvFile(profile)},
		{"ConfigFile", ConfigFile(profile)},
		{"MemoryFile", MemoryFile(profile)},
		{"UserFile", UserFile(profile)},
		{"SoulFile", SoulFile(profile)},
	}

	for _, c := range checks {
		if !strings.HasPrefix(c.got, ph) {
			t.Errorf("%s(%q) = %q, expected prefix of ProfileHome %q", c.name, profile, c.got, ph)
		}
	}
}

func TestBrowserProfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PAN_AGENT_HOME", tmp)

	got := BrowserProfile()
	if !strings.HasPrefix(got, tmp) {
		t.Errorf("BrowserProfile() = %q, want prefix %q", got, tmp)
	}
	if !strings.HasSuffix(got, "browser-profile") {
		t.Errorf("BrowserProfile() = %q, want suffix 'browser-profile'", got)
	}
}

func TestDirFunctionsUnderAgentHome(t *testing.T) {
	home := AgentHome()

	dirs := []struct {
		name string
		got  string
	}{
		{"SkillsDir", SkillsDir()},
		{"ProfileSkillsDir(default)", ProfileSkillsDir("default")},
		{"LogsDir", LogsDir()},
		{"CacheDir", CacheDir()},
		{"Claw3dDir", Claw3dDir()},
	}

	for _, d := range dirs {
		if !strings.HasPrefix(d.got, home) {
			t.Errorf("%s = %q, expected prefix of AgentHome %q", d.name, d.got, home)
		}
		if d.got == "" {
			t.Errorf("%s returned empty string", d.name)
		}
	}
}
