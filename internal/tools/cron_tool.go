package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/euraika-labs/pan-agent/internal/cron"
)

// CronTool manages scheduled jobs stored in cron/jobs.json.
type CronTool struct{}

// cronParams is the JSON-decoded parameter bag for CronTool.
type cronParams struct {
	Operation string `json:"operation"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Schedule  string `json:"schedule,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}

func (CronTool) Name() string { return "cron" }

func (CronTool) Description() string {
	return "Manage scheduled cron jobs. Operations: list, create, remove, pause, resume, trigger. " +
		"Jobs are stored in cron/jobs.json and executed by the pan-agent scheduler."
}

func (CronTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["operation"],
  "properties": {
    "operation": {
      "type": "string",
      "enum": ["list", "create", "remove", "pause", "resume", "trigger"],
      "description": "The cron operation to perform."
    },
    "id": {
      "type": "string",
      "description": "Job ID — required for remove, pause, resume, and trigger."
    },
    "name": {
      "type": "string",
      "description": "Human-readable job name — used by create."
    },
    "schedule": {
      "type": "string",
      "description": "Cron schedule expression (e.g. '0 9 * * 1-5') — required for create."
    },
    "prompt": {
      "type": "string",
      "description": "Prompt text the agent will run on each schedule tick — required for create."
    }
  }
}`)
}

func (t CronTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p cronParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}

	switch p.Operation {
	case "list":
		return t.opList()
	case "create":
		return t.opCreate(p)
	case "remove":
		return t.opRemove(p)
	case "pause":
		return t.opPause(p)
	case "resume":
		return t.opResume(p)
	case "trigger":
		return t.opTrigger(p)
	default:
		return &Result{Error: fmt.Sprintf("unknown operation %q; must be one of: list, create, remove, pause, resume, trigger", p.Operation)}, nil
	}
}

func (CronTool) opList() (*Result, error) {
	jobs, err := cron.List()
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	if len(jobs) == 0 {
		return &Result{Output: "no cron jobs configured"}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d job(s):\n\n", len(jobs))
	for _, j := range jobs {
		fmt.Fprintf(&sb, "ID:       %s\n", j.ID)
		fmt.Fprintf(&sb, "  Name:     %s\n", j.Name)
		fmt.Fprintf(&sb, "  Schedule: %s\n", j.Schedule)
		fmt.Fprintf(&sb, "  State:    %s\n", j.State)
		fmt.Fprintf(&sb, "  Enabled:  %v\n", j.Enabled)
		if j.LastRun != nil {
			fmt.Fprintf(&sb, "  Last run: %s\n", time.UnixMilli(*j.LastRun).Format("2006-01-02 15:04:05"))
		}
		if j.NextRun != nil {
			fmt.Fprintf(&sb, "  Next run: %s\n", time.UnixMilli(*j.NextRun).Format("2006-01-02 15:04:05"))
		}
		if j.LastStatus != "" {
			fmt.Fprintf(&sb, "  Status:   %s\n", j.LastStatus)
		}
		if j.Prompt != "" {
			prompt := j.Prompt
			if len(prompt) > 80 {
				prompt = prompt[:80] + "..."
			}
			fmt.Fprintf(&sb, "  Prompt:   %s\n", prompt)
		}
		sb.WriteString("\n")
	}
	return &Result{Output: sb.String()}, nil
}

func (CronTool) opCreate(p cronParams) (*Result, error) {
	if p.Schedule == "" {
		return &Result{Error: "schedule must not be empty for create"}, nil
	}
	if p.Prompt == "" {
		return &Result{Error: "prompt must not be empty for create"}, nil
	}

	job, err := cron.Create(p.Name, p.Schedule, p.Prompt)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("job created: id=%s name=%q schedule=%q", job.ID, job.Name, job.Schedule)}, nil
}

func (CronTool) opRemove(p cronParams) (*Result, error) {
	if p.ID == "" {
		return &Result{Error: "id must not be empty for remove"}, nil
	}
	if err := cron.Remove(p.ID); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("job %s removed", p.ID)}, nil
}

func (CronTool) opPause(p cronParams) (*Result, error) {
	if p.ID == "" {
		return &Result{Error: "id must not be empty for pause"}, nil
	}
	if err := cron.Pause(p.ID); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("job %s paused", p.ID)}, nil
}

func (CronTool) opResume(p cronParams) (*Result, error) {
	if p.ID == "" {
		return &Result{Error: "id must not be empty for resume"}, nil
	}
	if err := cron.Resume(p.ID); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("job %s resumed", p.ID)}, nil
}

func (CronTool) opTrigger(p cronParams) (*Result, error) {
	if p.ID == "" {
		return &Result{Error: "id must not be empty for trigger"}, nil
	}
	if err := cron.Trigger(p.ID); err != nil {
		return &Result{Error: err.Error()}, nil
	}
	return &Result{Output: fmt.Sprintf("job %s triggered (next_run_at set to now)", p.ID)}, nil
}

// Ensure CronTool satisfies the Tool interface at compile time.
var _ Tool = CronTool{}

func init() {
	Register(CronTool{})
}
