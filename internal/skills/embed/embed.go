// Package embed bundles the reviewer + curator persona markdown into the
// binary so the agent loops always have a stable behavioural contract,
// independent of whatever the user has installed in their skills directory.
package embed

import _ "embed"

//go:embed reviewer.md
var ReviewerMD string

//go:embed curator.md
var CuratorMD string
