package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// filesystemMaxRead is the maximum number of bytes returned by a read operation.
const filesystemMaxRead = 100 * 1024 // 100 KB

// FilesystemTool provides read, write, append, delete, list, search and mkdir
// operations on the local file system.
type FilesystemTool struct{}

// filesystemParams is the JSON-decoded parameter bag for FilesystemTool.
type filesystemParams struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`   // used by write / append
	Pattern   string `json:"pattern,omitempty"`   // used by search (regexp)
	Recursive bool   `json:"recursive,omitempty"` // used by list / search
}

func (FilesystemTool) Name() string { return "filesystem" }

func (FilesystemTool) Description() string {
	return "Perform file-system operations: read, write, append, delete, list, search, mkdir. " +
		"Read returns up to 100 KB of file content. " +
		"Search performs a regexp match across files in a directory."
}

func (FilesystemTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["operation", "path"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["read", "write", "append", "delete", "list", "search", "mkdir"],
      "description": "The file-system operation to perform."
    },
    "path": {
      "type": "string",
      "description": "Absolute or relative path to a file or directory."
    },
    "content": {
      "type": "string",
      "description": "Text content used by write and append operations."
    },
    "pattern": {
      "type": "string",
      "description": "Regular expression used by the search operation."
    },
    "recursive": {
      "type": "boolean",
      "description": "When true, list and search descend into subdirectories. Defaults to false."
    }
  }
}`)
}

func (t FilesystemTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p filesystemParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	if p.Path == "" {
		return &Result{Error: "path must not be empty"}, nil
	}

	switch p.Operation {
	case "read":
		return t.opRead(p)
	case "write":
		return t.opWrite(p, false)
	case "append":
		return t.opWrite(p, true)
	case "delete":
		return t.opDelete(p)
	case "list":
		return t.opList(p)
	case "search":
		return t.opSearch(p)
	case "mkdir":
		return t.opMkdir(p)
	default:
		return &Result{Error: fmt.Sprintf("unknown operation %q; must be one of: read, write, append, delete, list, search, mkdir", p.Operation)}, nil
	}
}

// ---------------------------------------------------------------------------
// individual operations
// ---------------------------------------------------------------------------

func (FilesystemTool) opRead(p filesystemParams) (*Result, error) {
	f, err := os.Open(p.Path)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	defer f.Close()

	buf := make([]byte, filesystemMaxRead)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return &Result{Error: err.Error()}, nil
	}

	out := string(buf[:n])
	// If the file was larger than the cap, say so.
	fi, statErr := f.Stat()
	if statErr == nil && fi.Size() > filesystemMaxRead {
		out += fmt.Sprintf("\n\n[truncated: file is %d bytes, only first %d bytes returned]",
			fi.Size(), filesystemMaxRead)
	}
	return &Result{Output: out}, nil
}

func (FilesystemTool) opWrite(p filesystemParams, append_ bool) (*Result, error) {
	// Ensure parent directory exists.
	if dir := filepath.Dir(p.Path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return &Result{Error: fmt.Sprintf("cannot create parent directory: %v", err)}, nil
		}
	}

	flag := os.O_WRONLY | os.O_CREATE
	if append_ {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}

	f, err := os.OpenFile(p.Path, flag, 0o600)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	defer f.Close()

	n, err := f.WriteString(p.Content)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	verb := "written"
	if append_ {
		verb = "appended"
	}
	return &Result{Output: fmt.Sprintf("%d bytes %s to %s", n, verb, p.Path)}, nil
}

func (FilesystemTool) opDelete(p filesystemParams) (*Result, error) {
	if err := os.Remove(p.Path); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("deleted %s", p.Path)}, nil
}

func (FilesystemTool) opList(p filesystemParams) (*Result, error) {
	var sb strings.Builder

	if p.Recursive {
		err := filepath.WalkDir(p.Path, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Log the error inline but keep walking.
				fmt.Fprintf(&sb, "[error accessing %s: %v]\n", path, err)
				return nil
			}
			rel, _ := filepath.Rel(p.Path, path)
			if d.IsDir() {
				fmt.Fprintf(&sb, "%s/\n", rel)
			} else {
				info, infoErr := d.Info()
				if infoErr != nil {
					fmt.Fprintf(&sb, "%s [size unknown]\n", rel)
				} else {
					fmt.Fprintf(&sb, "%s  %d bytes\n", rel, info.Size())
				}
			}
			return nil
		})
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
	} else {
		entries, err := os.ReadDir(p.Path)
		if err != nil {
			return &Result{Error: err.Error()}, nil
		}
		for _, e := range entries {
			if e.IsDir() {
				fmt.Fprintf(&sb, "%s/\n", e.Name())
			} else {
				info, infoErr := e.Info()
				if infoErr != nil {
					fmt.Fprintf(&sb, "%s [size unknown]\n", e.Name())
				} else {
					fmt.Fprintf(&sb, "%s  %d bytes\n", e.Name(), info.Size())
				}
			}
		}
	}

	return &Result{Output: sb.String()}, nil
}

func (FilesystemTool) opSearch(p filesystemParams) (*Result, error) {
	if p.Pattern == "" {
		return &Result{Error: "pattern must not be empty for search"}, nil
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return &Result{Error: fmt.Sprintf("invalid regexp: %v", err)}, nil
	}

	var sb strings.Builder
	matchCount := 0

	walker := func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			fmt.Fprintf(&sb, "[error accessing %s: %v]\n", path, walkErr)
			return nil
		}
		if d.IsDir() {
			// If not recursive, skip subdirectories other than the root.
			if !p.Recursive && path != p.Path {
				return fs.SkipDir
			}
			return nil
		}

		// #nosec G122 -- agent-driven recursive grep across an
		// already-resolved root. Symlink TOCTOU is not in the threat
		// model: an attacker who can swap files mid-walk has already
		// won. The approval gate on the filesystem tool is the safety
		// boundary.
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			// Binary or unreadable files are skipped silently.
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for lineNum, line := range lines {
			if re.MatchString(line) {
				fmt.Fprintf(&sb, "%s:%d: %s\n", path, lineNum+1, line)
				matchCount++
			}
		}
		return nil
	}

	// Determine whether path is a file or directory.
	fi, statErr := os.Stat(p.Path)
	if statErr != nil {
		return &Result{Error: statErr.Error()}, nil
	}

	if fi.IsDir() {
		if walkErr := filepath.WalkDir(p.Path, walker); walkErr != nil {
			return &Result{Error: walkErr.Error()}, nil
		}
	} else {
		// Single-file search.
		data, readErr := os.ReadFile(p.Path)
		if readErr != nil {
			return &Result{Error: readErr.Error()}, nil
		}
		lines := strings.Split(string(data), "\n")
		for lineNum, line := range lines {
			if re.MatchString(line) {
				fmt.Fprintf(&sb, "%s:%d: %s\n", p.Path, lineNum+1, line)
				matchCount++
			}
		}
	}

	if matchCount == 0 {
		return &Result{Output: "no matches found"}, nil
	}
	return &Result{Output: sb.String()}, nil
}

func (FilesystemTool) opMkdir(p filesystemParams) (*Result, error) {
	if err := os.MkdirAll(p.Path, 0o750); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("directory created: %s", p.Path)}, nil
}

// Ensure FilesystemTool satisfies the Tool interface at compile time.
var _ Tool = FilesystemTool{}

func init() {
	Register(FilesystemTool{})
}
