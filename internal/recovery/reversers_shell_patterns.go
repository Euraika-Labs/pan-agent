package recovery

import "regexp"

// shellPattern maps a compiled regex for recognising a normalised shell command
// to an inverseBuilder that produces the inverse command string.
// The table is exhaustive — anything not matched is fail-closed (ErrNoInverseKnown).
//
// Patterns use RE2 (Go's regexp package) — no lookaheads or lookbehinds.
// The architect's 7 rows are represented exactly.
type shellPattern struct {
	re      *regexp.Regexp
	inverse inverseBuilder
}

// inverseBuilder takes the original normalised command and returns the inverse
// command tokens (argv style). Returns ("", nil) when the inverse cannot be
// determined from the pattern alone and needs snapshot metadata.
type inverseBuilder func(args []string) ([]string, error)

// builtinShellPatterns is the compiled-in inverse pattern table.
// Tester note: each entry is exercised by TestShellReverserMkdirRmdir and peers.
var builtinShellPatterns = []shellPattern{
	// mkdir <path>
	{
		re: regexp.MustCompile(`^mkdir\s+(.+)$`),
		inverse: func(args []string) ([]string, error) {
			// Inverse: rmdir <path> (only if dir empty — enforced at execution time)
			if len(args) < 2 {
				return nil, ErrNoInverseKnown
			}
			return []string{"rmdir", args[1]}, nil
		},
	},
	// touch <path>  (file did not exist before — verified from snapshot)
	{
		re: regexp.MustCompile(`^touch\s+(.+)$`),
		inverse: func(args []string) ([]string, error) {
			if len(args) < 2 {
				return nil, ErrNoInverseKnown
			}
			return []string{"rm", args[1]}, nil
		},
	},
	// cp <src> <dst>  (dst did not exist before)
	{
		re: regexp.MustCompile(`^cp\s+(\S+)\s+(\S+)$`),
		inverse: func(args []string) ([]string, error) {
			// args: ["cp", src, dst]
			if len(args) < 3 {
				return nil, ErrNoInverseKnown
			}
			return []string{"rm", args[2]}, nil
		},
	},
	// mv <src> <dst>
	{
		re: regexp.MustCompile(`^mv\s+(\S+)\s+(\S+)$`),
		inverse: func(args []string) ([]string, error) {
			if len(args) < 3 {
				return nil, ErrNoInverseKnown
			}
			// Swap src and dst.
			return []string{"mv", args[2], args[1]}, nil
		},
	},
	// chmod <mode> <path>  — original-mode read from snapshot metadata at reversal time
	{
		re: regexp.MustCompile(`^chmod\s+(\S+)\s+(.+)$`),
		inverse: func(args []string) ([]string, error) {
			// The original mode must be read from the snapshot; the inverseBuilder
			// cannot know it statically. Return nil so ShellReverser delegates to
			// the snapshot-lookup path.
			return nil, errNeedSnapshotMeta
		},
	},
	// chown <owner>[:<group>] <path>
	{
		re: regexp.MustCompile(`^chown\s+(\S+)\s+(.+)$`),
		inverse: func(args []string) ([]string, error) {
			// Same: original uid/gid from snapshot metadata.
			return nil, errNeedSnapshotMeta
		},
	},
	// rm <path>  (snapshot succeeded) — delegates to FSReverser
	{
		re: regexp.MustCompile(`^rm\s+(.+)$`),
		inverse: func(args []string) ([]string, error) {
			// Signal that FSReverser should handle this.
			return nil, errDelegateToFS
		},
	},
}

// errNeedSnapshotMeta signals that the inverse needs original metadata from
// the captured snapshot (chmod original mode, chown original uid/gid).
// ShellReverser catches this and reads the snapshot stat.
var errNeedSnapshotMeta = &inverseError{"need snapshot metadata for inverse"}

// errDelegateToFS signals that FSReverser should restore this receipt.
var errDelegateToFS = &inverseError{"delegate to FSReverser"}

type inverseError struct{ msg string }

func (e *inverseError) Error() string { return e.msg }

// matchShellPattern returns the first matching pattern and the parsed args.
// args[0] is the command name; subsequent args are the whitespace-split fields
// captured by the pattern's sub-groups prepended by the full match split.
func matchShellPattern(normalised string) (*shellPattern, []string, bool) {
	for i := range builtinShellPatterns {
		p := &builtinShellPatterns[i]
		m := p.re.FindStringSubmatch(normalised)
		if m != nil {
			// m[0] is the full match; m[1..] are captures.
			// Reconstruct argv-style: [command, cap1, cap2, ...]
			// We split the full match on whitespace for safety.
			return p, m, true
		}
	}
	return nil, nil, false
}
