package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/euraika-labs/pan-agent/internal/memory"
)

// MemoryTool provides read/write access to the persistent MEMORY.md and USER.md
// files for a given profile.
type MemoryTool struct{}

// memoryParams is the JSON-decoded parameter bag for MemoryTool.
type memoryParams struct {
	Operation string `json:"operation"`
	Content   string `json:"content,omitempty"`
	Index     int    `json:"index,omitempty"`
	Profile   string `json:"profile,omitempty"`
}

func (MemoryTool) Name() string { return "memory" }

func (MemoryTool) Description() string {
	return "Read and modify persistent memory entries (MEMORY.md) and the user profile (USER.md) " +
		"for a given profile. Operations: read, add, update, remove, write_profile."
}

func (MemoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["read", "add", "update", "remove", "write_profile"],
      "description": "The memory operation to perform."
    },
    "content": {
      "type": "string",
      "description": "Text content for add, update, or write_profile operations."
    },
    "index": {
      "type": "integer",
      "description": "Zero-based index of the entry to update or remove."
    },
    "profile": {
      "type": "string",
      "description": "Profile name. Defaults to the default profile when omitted."
    }
  }
}`)
}

func (t MemoryTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p memoryParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "read":
		return t.opRead(p)
	case "add":
		return t.opAdd(p)
	case "update":
		return t.opUpdate(p)
	case "remove":
		return t.opRemove(p)
	case "write_profile":
		return t.opWriteProfile(p)
	default:
		return &Result{Error: fmt.Sprintf("unknown operation %q; must be one of: read, add, update, remove, write_profile", p.Operation)}, nil
	}
}

func (MemoryTool) opRead(p memoryParams) (*Result, error) {
	state, err := memory.ReadMemory(p.Profile)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Memory (%d/%d chars):\n", state.CharCount, state.CharLimit)
	if len(state.Entries) == 0 {
		sb.WriteString("  (no entries)\n")
	} else {
		for i, e := range state.Entries {
			fmt.Fprintf(&sb, "  [%d] %s\n", i, e)
		}
	}
	fmt.Fprintf(&sb, "\nUser profile (%d/%d chars):\n", state.UserCharCount, state.UserCharLimit)
	if state.UserProfile == "" {
		sb.WriteString("  (empty)\n")
	} else {
		sb.WriteString(state.UserProfile)
		sb.WriteString("\n")
	}
	return &Result{Output: sb.String()}, nil
}

func (MemoryTool) opAdd(p memoryParams) (*Result, error) {
	if p.Content == "" {
		return &Result{Error: "content must not be empty for add"}, nil
	}
	if err := memory.AddEntry(p.Content, p.Profile); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: "entry added"}, nil
}

func (MemoryTool) opUpdate(p memoryParams) (*Result, error) {
	if p.Content == "" {
		return &Result{Error: "content must not be empty for update"}, nil
	}
	if err := memory.UpdateEntry(p.Index, p.Content, p.Profile); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("entry %d updated", p.Index)}, nil
}

func (MemoryTool) opRemove(p memoryParams) (*Result, error) {
	if err := memory.RemoveEntry(p.Index, p.Profile); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("entry %d removed", p.Index)}, nil
}

func (MemoryTool) opWriteProfile(p memoryParams) (*Result, error) {
	if err := memory.WriteUserProfile(p.Content, p.Profile); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: "user profile written"}, nil
}

// Ensure MemoryTool satisfies the Tool interface at compile time.
var _ Tool = MemoryTool{}

func init() {
	Register(MemoryTool{})
}
