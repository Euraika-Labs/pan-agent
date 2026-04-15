package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// RunDoctor performs system health checks and returns the output as a string.
// This is the same set of checks as the "pan-agent doctor" CLI command.
func RunDoctor(profile string) string {
	var b strings.Builder
	ok := true

	check := func(label string, pass bool, detail string) {
		status := "OK"
		if !pass {
			status = "FAIL"
			ok = false
		}
		if detail != "" {
			fmt.Fprintf(&b, "  [%s] %s — %s\n", status, label, detail)
		} else {
			fmt.Fprintf(&b, "  [%s] %s\n", status, label)
		}
	}

	b.WriteString("pan-agent doctor\n")
	b.WriteString("----------------\n")

	// 1. AgentHome directory
	home := paths.AgentHome()
	info, err := os.Stat(home)
	check("AgentHome exists", err == nil && info.IsDir(), home)

	// 2. Profile .env readable
	if profile == "" {
		profile = "default"
	}
	env, err := ReadProfileEnv(profile)
	check("Profile .env readable", err == nil, paths.EnvFile(profile))

	// 3. API key present
	apiKey := env["REGOLO_API_KEY"]
	if apiKey == "" {
		apiKey = env["OPENAI_API_KEY"]
	}
	if apiKey == "" {
		apiKey = env["API_KEY"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	check("API key present", apiKey != "", "REGOLO_API_KEY or OPENAI_API_KEY")

	// 4. State DB opens
	dbPath := paths.StateDB()
	db, dbErr := storage.Open(dbPath)
	check("SQLite DB opens", dbErr == nil, dbPath)
	if dbErr == nil {
		_ = db.Close()
	}

	// 5. Config file present
	cfgPath := paths.ConfigFile(profile)
	_, cfgErr := os.Stat(cfgPath)
	check("Config file present", cfgErr == nil, cfgPath)

	// 6. M5 network-policy warning — flag LAN exposure without auth.
	// pan-agent binds 127.0.0.1 by default and that's a hard assumption
	// throughout the CSP, same-origin cookie, and rate-limiter designs.
	// An operator who flips gateway.host to 0.0.0.0 (for reasons) AND
	// leaves office.access_token empty is running an unauthenticated
	// WebSocket server on their LAN — effectively giving any device on
	// the network full agent control. We can't prevent that (some
	// deployments need it) but doctor should loudly warn.
	gatewayHost, _ := GetProfileValue(profile, "gateway.host")
	gatewayHost = strings.TrimSpace(gatewayHost)
	accessToken, _ := GetProfileValue(profile, "office.access_token")
	accessToken = strings.TrimSpace(accessToken)
	if gatewayHost == "0.0.0.0" && accessToken == "" {
		check("Network policy",
			false,
			"gateway.host=0.0.0.0 without office.access_token — adapter is LAN-exposed and unauthenticated")
	} else {
		check("Network policy", true, "loopback or token-protected")
	}

	b.WriteString("\n")
	if ok {
		b.WriteString("All checks passed.\n")
	} else {
		b.WriteString("One or more checks failed — see above.\n")
	}

	return b.String()
}
