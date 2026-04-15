package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/euraika-labs/pan-agent/internal/claw3d"
	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// cmdMigrateOffice implements `pan-agent migrate-office` — imports the
// legacy ~/.hermes/clawd3d-history.json into pan-agent's SQLite store,
// idempotently.
//
// Exit codes (documented in runbook.md):
//
//	0  source imported (or already imported, or dry-run)
//	1  source file missing (NOT an error — nothing to migrate)
//	2  parse error on source JSON
//	3  DB / backup error
//
// Flags:
//
//	--from <path>        source JSON (default: ~/.hermes/clawd3d-history.json)
//	--dry-run            parse + count rows, do not write anything
//	--force              re-import even if a matching digest is in the audit log
//	--backup-dir <path>  destination for the post-import .bak copy
//	--json               emit the MigrateReport as JSON on stdout
func cmdMigrateOffice(args []string) error {
	fs := flag.NewFlagSet("migrate-office", flag.ContinueOnError)
	source := fs.String("from", "", "source JSON (default ~/.hermes/clawd3d-history.json)")
	dryRun := fs.Bool("dry-run", false, "parse + count rows, do not write")
	force := fs.Bool("force", false, "re-import even if already migrated")
	backupDir := fs.String("backup-dir", "", "override default backup directory")
	asJSON := fs.Bool("json", false, "emit MigrateReport as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, err := storage.Open(paths.StateDB())
	if err != nil {
		return exitf(3, "open database: %v", err)
	}
	defer db.Close()

	opt := claw3d.MigrateOpts{
		Source:    *source,
		DryRun:    *dryRun,
		Force:     *force,
		BackupDir: *backupDir,
	}

	rep, err := claw3d.RunMigration(db, opt)
	if err != nil {
		// Distinguish parse errors (code 2) from everything else (3).
		// RunMigration wraps the parse failure with "parse source JSON"
		// so we pattern-match on that rather than plumb typed errors
		// through the API — this is a CLI convenience, not a production
		// dispatcher.
		if containsErr(err, "parse source JSON") {
			return exitf(2, "%v", err)
		}
		return exitf(3, "%v", err)
	}

	if *asJSON {
		out, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(out))
	} else {
		printMigrationHuman(rep, *dryRun)
	}

	switch rep.Status {
	case "missing":
		// Not an error for scripts — "nothing to migrate" is a clean
		// outcome. CI can distinguish via --json output if needed.
		os.Exit(1)
	}
	return nil
}

// exitf prints to stderr and exits with the given code. Returns a sentinel
// error so the caller's `return` expression in main() stays uniform.
func exitf(code int, format string, args ...any) error {
	fmt.Fprintf(os.Stderr, "pan-agent: migrate-office: "+format+"\n", args...)
	os.Exit(code)
	return errors.New("unreachable")
}

func containsErr(err error, needle string) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), needle)
}

// contains is a local Stringer wrapper to avoid importing strings just for
// one predicate. Keeps migrate_office.go's import surface tight.
func contains(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func printMigrationHuman(rep claw3d.MigrateReport, dryRun bool) {
	switch rep.Status {
	case "missing":
		fmt.Println("No legacy source found — nothing to migrate.")
		return
	case "skip":
		fmt.Printf("Skip: source already imported (digest %s).\n", rep.Digest[:12])
		fmt.Println("Re-run with --force to import again (may create duplicates).")
		return
	}
	mode := "imported"
	if dryRun {
		mode = "would import"
	}
	fmt.Printf("%s: %d agents, %d sessions, %d messages, %d cron jobs\n",
		mode, rep.Imported.Agents, rep.Imported.Sessions,
		rep.Imported.Messages, rep.Imported.Cron)
	if rep.BackupPath != "" {
		fmt.Printf("source moved to %s\n", rep.BackupPath)
	}
	fmt.Printf("digest: %s\n", rep.Digest)
}
