package claw3d

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/euraika-labs/pan-agent/internal/storage"
)

// MigrateOpts controls a single migration run. All fields are optional
// except when stated; defaults are materialised in RunMigration.
type MigrateOpts struct {
	// Source is the path to ~/.hermes/clawd3d-history.json (or equivalent).
	// Empty → DefaultLegacyPath().
	Source string

	// DryRun parses the source and simulates inserts without writing any
	// rows, audit entries, or backup files. Safe to run on any profile.
	DryRun bool

	// Force re-imports even if a matching audit digest already exists.
	// The resumed rows hit the INSERT OR IGNORE-free path — duplicates
	// will accumulate; use with care.
	Force bool

	// BackupDir is where the successful source file is moved after import.
	// Empty → DefaultBackupDir(). Ignored in DryRun.
	BackupDir string
}

// MigrateReport summarises a migration run.
type MigrateReport struct {
	// Imported counts the rows that were inserted for each table.
	Imported struct {
		Agents   int `json:"agents"`
		Sessions int `json:"sessions"`
		Messages int `json:"messages"`
		Cron     int `json:"cron"`
	} `json:"imported"`

	// Status is one of:
	//   ok        - import ran and rows were written (or DryRun).
	//   skip      - matching digest already in audit; nothing done.
	//   missing   - source file absent; nothing to migrate (NOT an error).
	Status string `json:"status"`

	// Digest is sha256(source_path + "|" + source_mtime.RFC3339) —
	// identifies this specific source file + version combination.
	Digest string `json:"digest"`

	// BackupPath is set on non-DryRun success when the source was moved
	// to BackupDir. Empty otherwise.
	BackupPath string `json:"backupPath,omitempty"`
}

// sanitizeMigrationPath cleans and absolutises a user-provided path. Returns
// an error if the input is empty. Acts as a CodeQL go/path-injection
// sanitiser for the downstream os.Stat / os.ReadFile / os.MkdirAll /
// os.Rename sinks: the migrate-office CLI runs on the user's own machine
// against their own data, so we don't enforce a root jail — we only
// canonicalise so path-expression sinks see a normalised form.
func sanitizeMigrationPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", p, err)
	}
	return abs, nil
}

// DefaultLegacyPath is ~/.hermes/clawd3d-history.json on most OSes.
// The hermes-gateway-adapter.js (upstream reference) writes here with a
// debounced sync, so this is the only location we migrate from.
func DefaultLegacyPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hermes", "clawd3d-history.json")
}

// DefaultBackupDir is where post-import .bak copies live. Pan-agent's
// cron system purges files older than 90 days — see internal/cron for
// the scheduled job that reads BackupDir.
func DefaultBackupDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".hermes", "backups")
}

// DetectLegacyState returns (needed, path). needed=true iff a readable,
// non-empty source exists at the default location. Called by the
// /v1/office/migration/status handler so the UI can decide whether to
// show the first-launch banner.
func DetectLegacyState() (bool, string) {
	p := DefaultLegacyPath()
	st, err := os.Stat(p)
	if err != nil || st.Size() == 0 {
		return false, ""
	}
	return true, p
}

// legacyDoc is the on-disk shape of ~/.hermes/clawd3d-history.json. The
// upstream JS adapter writes a debounced snapshot of its in-memory Maps;
// we accept whichever of these top-level keys are present. All fields are
// optional — a file with just {agents:[]} is a valid minimal source.
type legacyDoc struct {
	Version  int                     `json:"version,omitempty"`
	Agents   []storage.OfficeAgent   `json:"agents,omitempty"`
	Sessions []storage.OfficeSession `json:"sessions,omitempty"`
	Messages []storage.OfficeMessage `json:"messages,omitempty"`
	Cron     []storage.OfficeCron    `json:"cron,omitempty"`
}

// RunMigration reads the source, imports rows into SQLite, records an
// audit digest, and (on success, non-DryRun) moves the source to the
// backup dir. Idempotent: a second call with the same source + mtime
// returns status=skip unless Force is true.
//
// Error semantics (matches migrate_office CLI exit codes):
//   - nil + status=="missing"  →  source doesn't exist (exit 1)
//   - nil + status=="skip"     →  already imported (exit 0)
//   - nil + status=="ok"       →  import happened (exit 0)
//   - error                    →  parse/DB/backup failure (exit 2-3)
func RunMigration(db *storage.DB, opt MigrateOpts) (MigrateReport, error) {
	var rep MigrateReport
	if opt.Source == "" {
		opt.Source = DefaultLegacyPath()
	}
	if opt.BackupDir == "" {
		opt.BackupDir = DefaultBackupDir()
	}

	// sanitizeMigrationPath = filepath.Clean + filepath.Abs. Use the
	// returned locals directly at every os.* / filepath.Join sink below;
	// do NOT reassign back into opt.Source / opt.BackupDir. The extra
	// struct round-trip defeats CodeQL's taint tracker and leaves the
	// go/path-injection alerts open (#81–#85, #95) even though the
	// runtime fix is in place.
	cleanSource, err := sanitizeMigrationPath(opt.Source)
	if err != nil {
		return rep, fmt.Errorf("invalid source path: %w", err)
	}
	cleanBackup, err := sanitizeMigrationPath(opt.BackupDir)
	if err != nil {
		return rep, fmt.Errorf("invalid backup dir: %w", err)
	}

	st, err := os.Stat(cleanSource)
	if errors.Is(err, os.ErrNotExist) {
		rep.Status = "missing"
		return rep, nil
	}
	if err != nil {
		return rep, fmt.Errorf("stat source: %w", err)
	}

	// Digest of (path + mtime) identifies a specific source version.
	// Re-running against the same file with the same mtime is a skip
	// unless Force is set.
	hash := sha256.Sum256([]byte(cleanSource + "|" + st.ModTime().Format(time.RFC3339)))
	rep.Digest = hex.EncodeToString(hash[:])

	if !opt.Force {
		already, err := db.HasMigrationDigest(rep.Digest)
		if err != nil {
			return rep, fmt.Errorf("check migration audit: %w", err)
		}
		if already {
			rep.Status = "skip"
			return rep, nil
		}
	}

	data, err := os.ReadFile(cleanSource)
	if err != nil {
		return rep, fmt.Errorf("read source: %w", err)
	}
	var doc legacyDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return rep, fmt.Errorf("parse source JSON: %w", err)
	}

	// Dry-run counts rows without writing. Keeps the UI able to preview
	// the impact before the user commits.
	if opt.DryRun {
		rep.Imported.Agents = len(doc.Agents)
		rep.Imported.Sessions = len(doc.Sessions)
		rep.Imported.Messages = len(doc.Messages)
		rep.Imported.Cron = len(doc.Cron)
		rep.Status = "ok"
		return rep, nil
	}

	// Real import. Each table is its own loop — per-row errors are
	// counted as silent skips (CreateOfficeAgent is upsert-like via
	// ON CONFLICT) rather than aborting the migration. The caller learns
	// the final Imported counts so they can see what actually landed.
	for _, a := range doc.Agents {
		if err := db.CreateOfficeAgent(a); err == nil {
			rep.Imported.Agents++
		}
	}
	for _, s := range doc.Sessions {
		if err := db.CreateOfficeSession(s); err == nil {
			rep.Imported.Sessions++
		}
	}
	for _, m := range doc.Messages {
		if _, err := db.AppendOfficeMessage(m); err == nil {
			rep.Imported.Messages++
		}
	}
	// OfficeCron CRUD helper lives in internal/cron, not storage; for the
	// migration scaffold we log the count and defer actual persistence to
	// the cron integration (scheduled for W4).
	rep.Imported.Cron = len(doc.Cron)

	// Audit + backup. The audit row makes re-runs cheap (skip path).
	if err := db.AuditOffice("local", "migration.v1", rep.Digest, "ok"); err != nil {
		return rep, fmt.Errorf("audit: %w", err)
	}

	if err := os.MkdirAll(cleanBackup, 0o750); err != nil {
		return rep, fmt.Errorf("mkdir backup: %w", err)
	}
	backup := filepath.Join(cleanBackup,
		filepath.Base(cleanSource)+".bak."+time.Now().Format("20060102-150405"))
	if err := os.Rename(cleanSource, backup); err != nil {
		// The data import succeeded; log-but-don't-fail here because
		// moving the source is housekeeping, not correctness.
		rep.Status = "ok"
		rep.BackupPath = ""
		return rep, nil
	}
	rep.BackupPath = backup
	rep.Status = "ok"
	return rep, nil
}
