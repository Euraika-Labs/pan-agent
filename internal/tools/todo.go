package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TodoTool maintains an in-memory task list with list, add, complete, and remove
// operations. State is package-level so it persists for the lifetime of the
// agent process.
type TodoTool struct{}

// task represents a single item in the todo list.
type task struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
	Completed   bool   `json:"completed"`
}

// todoStore is the package-level state for the todo list.
var todoStore = struct {
	mu     sync.Mutex
	tasks  []task
	nextID int
}{
	nextID: 1,
}

// todoParams is the JSON-decoded parameter bag for TodoTool.
type todoParams struct {
	Operation string `json:"operation"`
	Task      string `json:"task,omitempty"`
	ID        string `json:"id,omitempty"`
}

func (TodoTool) Name() string { return "todo" }

func (TodoTool) Description() string {
	return "Manage an in-memory task list. " +
		"Operations: list (show all tasks), add (create a new task), " +
		"complete (mark a task done by id), remove (delete a task by id)."
}

func (TodoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["list", "add", "complete", "remove"],
      "description": "The operation to perform on the task list."
    },
    "task": {
      "type": "string",
      "description": "Task description. Required for the \"add\" operation."
    },
    "id": {
      "type": "string",
      "description": "Task ID. Required for \"complete\" and \"remove\" operations."
    }
  }
}`)
}

func (TodoTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p todoParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "list":
		return todoList(), nil
	case "add":
		return todoAdd(p.Task), nil
	case "complete":
		return todoComplete(p.ID), nil
	case "remove":
		return todoRemove(p.ID), nil
	default:
		return &Result{Error: fmt.Sprintf("unknown operation %q; must be list, add, complete, or remove", p.Operation)}, nil
	}
}

func todoList() *Result {
	todoStore.mu.Lock()
	defer todoStore.mu.Unlock()

	if len(todoStore.tasks) == 0 {
		return &Result{Output: "No tasks."}
	}

	var sb strings.Builder
	for _, t := range todoStore.tasks {
		status := "[ ]"
		if t.Completed {
			status = "[x]"
		}
		fmt.Fprintf(&sb, "%s #%d %s\n", status, t.ID, t.Description)
	}
	return &Result{Output: strings.TrimRight(sb.String(), "\n")}
}

func todoAdd(description string) *Result {
	if description == "" {
		return &Result{Error: "task description must not be empty"}
	}

	todoStore.mu.Lock()
	defer todoStore.mu.Unlock()

	t := task{
		ID:          todoStore.nextID,
		Description: description,
		Completed:   false,
	}
	todoStore.tasks = append(todoStore.tasks, t)
	todoStore.nextID++

	return &Result{Output: fmt.Sprintf("Added task #%d: %s", t.ID, t.Description)}
}

func todoComplete(idStr string) *Result {
	id, ok := parseID(idStr)
	if !ok {
		return &Result{Error: fmt.Sprintf("invalid id %q; must be a positive integer", idStr)}
	}

	todoStore.mu.Lock()
	defer todoStore.mu.Unlock()

	for i := range todoStore.tasks {
		if todoStore.tasks[i].ID == id {
			todoStore.tasks[i].Completed = true
			return &Result{Output: fmt.Sprintf("Completed task #%d: %s", id, todoStore.tasks[i].Description)}
		}
	}
	return &Result{Error: fmt.Sprintf("task #%d not found", id)}
}

func todoRemove(idStr string) *Result {
	id, ok := parseID(idStr)
	if !ok {
		return &Result{Error: fmt.Sprintf("invalid id %q; must be a positive integer", idStr)}
	}

	todoStore.mu.Lock()
	defer todoStore.mu.Unlock()

	for i, t := range todoStore.tasks {
		if t.ID == id {
			todoStore.tasks = append(todoStore.tasks[:i], todoStore.tasks[i+1:]...)
			return &Result{Output: fmt.Sprintf("Removed task #%d: %s", id, t.Description)}
		}
	}
	return &Result{Error: fmt.Sprintf("task #%d not found", id)}
}

// parseID converts a string to a positive integer task ID.
func parseID(s string) (int, bool) {
	var id int
	_, err := fmt.Sscanf(s, "%d", &id)
	return id, err == nil && id > 0
}

// Ensure TodoTool satisfies the Tool interface at compile time.
var _ Tool = TodoTool{}

func init() {
	Register(TodoTool{})
}
