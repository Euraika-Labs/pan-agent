package config

import (
	"fmt"
	"os"
	"strings"
)

// OfficeConfig is the per-profile `office:` section in config.yaml. It lives
// alongside ModelConfig (same profile scoping). 0.4.0 introduces the runtime
// engine toggle + migration-ack flag; the remaining fields are staged for
// M5/M6 features that need a stable config surface starting now.
type OfficeConfig struct {
	// Engine selects the Claw3D backend: "go" (embedded adapter + static
	// bundle) or "node" (legacy sidecar). Default "go". Flipped at runtime
	// via POST /v1/office/engine or `pan-agent doctor --switch-engine`.
	Engine string

	// NodePort is the dev-server port when Engine=="node". Ignored when
	// Engine=="go". Default 3000.
	NodePort int

	// MigrationAck is the one-shot flag set when the user dismisses or
	// acts on the "Claw3D is now built-in" banner. Persisted so the
	// banner never re-appears for the same user after acknowledgement.
	MigrationAck bool

	// StrictOrigin, when true, rejects WS upgrades with empty Origin
	// headers. Default false preserves CLI/curl probes for local dev.
	// See internal/claw3d/auth.go originAllowed.
	StrictOrigin bool

	// UsageLog controls whether /v1/office/* hits are appended to the
	// local-only office_usage.log under AgentHome. Read by the M6
	// `pan-agent doctor --deprecated-usage` scanner. Default true.
	UsageLog bool

	// WindowsFallback selects behaviour when WebView2 fails the WebGL2
	// probe: "browser" opens the system browser via Tauri shell.open,
	// "none" leaves the user with a broken scene. Default "browser".
	WindowsFallback string

	// BrowserFallbackUntil is an RFC3339 timestamp; the WebView2 probe is
	// skipped while now<until so users who accepted the fallback are not
	// re-prompted daily. Set by the /v1/office/fallback-detected endpoint.
	BrowserFallbackUntil string
}

// officeEngineValid is the whitelist for Engine. Invalid values are rejected
// at load-time with an explicit error — a typo in config.yaml should be
// loud, not silently smuggled as "go".
var officeEngineValid = map[string]bool{"go": true, "node": true}

// ResolveOfficeEngine returns the effective engine name for profile.
//
// Priority: PAN_OFFICE_ENGINE env var > config.yaml office.engine > "go".
//
// The env override exists for power users who want to flip quickly without
// editing yaml (e.g., one-shot `PAN_OFFICE_ENGINE=node pan-agent serve`).
// Invalid env values are fatal; invalid yaml values are also fatal because
// silently coercing to "go" would mask a genuine misconfiguration.
func ResolveOfficeEngine(profile string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("PAN_OFFICE_ENGINE")); v != "" {
		if officeEngineValid[v] {
			return v, nil
		}
		return "", fmt.Errorf("PAN_OFFICE_ENGINE must be go|node, got %q", v)
	}
	v, _ := GetProfileValue(profile, "office.engine")
	v = strings.TrimSpace(v)
	if v == "" {
		return "go", nil
	}
	if !officeEngineValid[v] {
		return "", fmt.Errorf("office.engine must be go|node, got %q", v)
	}
	return v, nil
}

// GetOfficeConfig reads the full office.* section from the profile's
// config.yaml. Defaults are materialised here so callers never confuse
// "absent" with "empty string". Keep this function total: an unreadable
// config returns the defaults, not an error — the gateway must always boot.
func GetOfficeConfig(profile string) OfficeConfig {
	oc := OfficeConfig{
		Engine:          "go",
		NodePort:        3000,
		UsageLog:        true,
		WindowsFallback: "browser",
	}
	if v, _ := GetProfileValue(profile, "office.engine"); v != "" && officeEngineValid[v] {
		oc.Engine = v
	}
	if v, _ := GetProfileValue(profile, "office.migration_ack"); v == "true" {
		oc.MigrationAck = true
	}
	if v, _ := GetProfileValue(profile, "office.strict_origin"); v == "true" {
		oc.StrictOrigin = true
	}
	if v, _ := GetProfileValue(profile, "office.usage_log"); v == "false" {
		oc.UsageLog = false
	}
	if v, _ := GetProfileValue(profile, "office.windows_fallback"); v != "" {
		oc.WindowsFallback = v
	}
	if v, _ := GetProfileValue(profile, "office.browser_fallback_until"); v != "" {
		oc.BrowserFallbackUntil = v
	}
	return oc
}

// WriteOfficeEngine persists the engine setting to config.yaml. Called by
// /v1/office/engine POST after a successful drain, and by
// `pan-agent doctor --switch-engine`. The underlying writer is atomic
// (temp file + rename), so a crash mid-write can't leave a half-valid yaml.
//
// Uses EnsureProfileValue rather than SetProfileValue: office.engine is a
// 0.4.0-era key that won't exist in yamls written by older pan-agent
// versions, and SetValue silently skips missing keys. That would leave
// users unable to persist a runtime swap — the in-memory state would flip
// but the yaml would stay pristine, reverting on restart. Smoke test
// surfaced this exact bug in M4 W1 review; EnsureValue appends if missing.
func WriteOfficeEngine(profile, engine string) error {
	if !officeEngineValid[engine] {
		return fmt.Errorf("WriteOfficeEngine: engine must be go|node, got %q", engine)
	}
	return EnsureProfileValue(profile, "office.engine", engine)
}

// SetMigrationAck flips the one-shot banner-dismissal flag. Used by any of
// the three banner buttons (Import / Keep as backup / Dismiss) — all three
// converge on the same side-effect, only "Import" additionally triggers
// the migration.
func SetMigrationAck(profile string, ack bool) error {
	v := "false"
	if ack {
		v = "true"
	}
	return SetProfileValue(profile, "office.migration_ack", v)
}
