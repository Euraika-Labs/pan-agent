package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// terminalDefaultTimeout is the maximum duration a shell command may run.
const terminalDefaultTimeout = 30 * time.Second

// TerminalTool executes arbitrary shell commands.
// On Windows the command is passed to cmd.exe /C; everywhere else to
// /bin/bash -c.
type TerminalTool struct{}

// terminalParams is the JSON-decoded parameter bag for TerminalTool.
type terminalParams struct {
	Command string `json:"command"`
	Workdir string `json:"workdir,omitempty"`
}

func (TerminalTool) Name() string { return "terminal" }

func (TerminalTool) Description() string {
	return "Execute a shell command and return its combined stdout+stderr output. " +
		"Commands run inside cmd.exe on Windows or /bin/bash on Unix. " +
		"Maximum execution time is 30 seconds."
}

func (TerminalTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {
      "type": "string",
      "description": "The shell command to execute."
    },
    "workdir": {
      "type": "string",
      "description": "Optional working directory for the command. Defaults to the agent process directory."
    }
  }
}`)
}

func (t TerminalTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p terminalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: fmt.Sprintf("invalid parameters: %v", err)}, nil
	}
	if p.Command == "" {
		return &Result{Error: "command must not be empty"}, nil
	}

	// Honour an externally imposed deadline but cap at terminalDefaultTimeout.
	ctx, cancel := context.WithTimeout(ctx, terminalDefaultTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", p.Command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/bash", "-c", p.Command)
	}

	if p.Workdir != "" {
		cmd.Dir = p.Workdir
	}

	// Combine stdout and stderr so callers see exactly what a terminal would.
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()

	out := buf.String()

	if runErr != nil {
		// Include the error message but still return whatever output was produced.
		if ctx.Err() != nil {
			return &Result{
				Output: out,
				Error:  fmt.Sprintf("command timed out after %s", terminalDefaultTimeout),
			}, nil
		}
		// Non-zero exit codes are surfaced as Error, not as a Go error, so the
		// agent loop can inspect output rather than treating it as a hard failure.
		return &Result{
			Output: out,
			Error:  runErr.Error(),
		}, nil
	}

	return &Result{Output: out}, nil
}

// Ensure TerminalTool satisfies the Tool interface at compile time.
var _ Tool = TerminalTool{}

func init() {
	Register(TerminalTool{})
}
