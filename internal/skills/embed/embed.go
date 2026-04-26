// Package embed bundles the reviewer + curator persona markdown into the
// binary so the agent loops always have a stable behavioural contract,
// independent of whatever the user has installed in their skills directory.
//
// Phase 13 WS#13.E adds the cookbook subdirectory: starter skills that
// demonstrate the canonical "interact tool + ARIA-YAML accessibility
// tree" pattern. The cookbook ships in the binary and the skills
// runtime seeds them into the user's `<ProfileSkillsDir>/desktop/` on
// first run (so users can edit/delete them without disrupting the
// authoritative version we shipped).
package embed

import (
	"embed"
	_ "embed"
)

//go:embed reviewer.md
var ReviewerMD string

//go:embed curator.md
var CuratorMD string

// CookbookFS exposes the cookbook/ subdirectory as an `embed.FS` so
// the runtime can iterate every skill file without hard-coding names
// here. New skills added to the directory get bundled by re-running
// `go build`; the runtime walker (skills.SeedCookbook, future) reads
// the FS and writes each entry into the user profile if not already
// present.
//
//go:embed cookbook/*.md
var CookbookFS embed.FS

// CookbookRoot is the directory inside CookbookFS where skill markdown
// files live. Exposed so the walker doesn't have to keep the literal
// "cookbook" string in sync across multiple files.
const CookbookRoot = "cookbook"
