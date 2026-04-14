# Profile System

Profiles are isolated agent environments. Each profile has its own configuration, memory, persona, and skills.

## Profile API

`internal/config/profiles.go`:

```go
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

func ListProfiles(activeProfile string) []ProfileInfo
func CreateProfile(name, cloneFrom string) error
func DeleteProfile(name string) error
```

## HTTP endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/v1/config/profiles` | List all profiles with metadata |
| POST | `/v1/config/profiles` | Create. Body: `{name, cloneConfig: bool}` |
| DELETE | `/v1/config/profiles/{name}` | Delete (cannot delete "default") |

## Filesystem layout

```
<AgentHome>/                   ← default profile lives here
├── .env
├── config.yaml
├── MEMORY.md
├── USER.md
├── SOUL.md
├── skills/
└── profiles/                  ← named profiles live under here
    ├── work/
    │   ├── .env
    │   ├── config.yaml
    │   ├── MEMORY.md
    │   └── ...
    └── personal/
        └── ...
```

The default profile resolves to `AgentHome()` directly. Named profiles resolve to `AgentHome()/profiles/<name>/`.

## Path resolution

`internal/paths/paths.go` exposes profile-aware path helpers:

```go
func ProfileHome(profile string) string  // "" or "default" → AgentHome
func EnvFile(profile string) string       // <ProfileHome>/.env
func ConfigFile(profile string) string    // <ProfileHome>/config.yaml
func MemoryFile(profile string) string    // <ProfileHome>/MEMORY.md
func UserFile(profile string) string      // <ProfileHome>/USER.md
func SoulFile(profile string) string      // <ProfileHome>/SOUL.md
func ProfileSkillsDir(profile string) string  // <ProfileHome>/skills/
```

`ProfileHome` lazily creates the directory with mode `0700`.

## Validation

Profile names must match `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`. This is enforced in both `CreateProfile` and `DeleteProfile` to prevent path traversal attacks via `os.RemoveAll`.

The "default" profile name is reserved — `CreateProfile("default", ...)` and `DeleteProfile("default")` both error out.

## Cloning

When `CreateProfile(name, cloneFrom)` is called with a non-empty `cloneFrom`:

1. The new profile directory is created.
2. `.env` is copied from `cloneFrom` to the new profile.
3. `config.yaml` is copied from `cloneFrom` to the new profile.
4. Other files (MEMORY, SOUL, skills) are NOT copied — the new profile starts with a clean slate.

## Active profile

The "active profile" is set at server startup via the `--profile` CLI flag, defaulting to the empty string (which resolves to "default").

The active profile determines:
- Which `.env` and `config.yaml` are loaded for the LLM client
- Which `MEMORY.md` and `SOUL.md` are read by the chat handler
- Which skills are available
- Which platform toggles control the gateway bots

The active profile is NOT changed by frontend actions — switching profiles in the UI just changes which profile the frontend's API calls target via the `?profile=` query parameter. The server's startup profile remains the same.

## Operator rule
There is no "switch profile" API. To change the server's active profile, restart `pan-agent serve --profile <name>`. The frontend's profile switching only affects what the frontend reads/writes, not what the server uses for its singleton LLM client.

## Read next
- [[04 - Configuration Reference]]
- [[03 - Profiles]]
