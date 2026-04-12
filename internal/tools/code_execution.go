package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func init() {
	Register(&codeExecTool{})
}

// codeExecTool implements Tool for sandboxed code execution.
type codeExecTool struct{}

func (c *codeExecTool) Name() string { return "code_execution" }

func (c *codeExecTool) Description() string {
	return "Execute a snippet of Python, JavaScript, or Bash/Shell code in a " +
		"sandboxed temporary directory. Returns the combined stdout+stderr output " +
		"and the exit code. Execution is killed after 30 seconds."
}

func (c *codeExecTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"language": {
				"type": "string",
				"enum": ["python", "javascript", "bash"],
				"description": "The language of the code snippet."
			},
			"code": {
				"type": "string",
				"description": "The source code to execute."
			}
		},
		"required": ["language", "code"]
	}`)
}

// codeExecParams mirrors the JSON parameters accepted by this tool.
type codeExecParams struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// codeExecResult is serialised into Result.Output.
type codeExecResult struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

func (c *codeExecTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p codeExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}

	lang := strings.ToLower(strings.TrimSpace(p.Language))
	if p.Code == "" {
		return &Result{Error: "code must not be empty"}, nil
	}

	// Create an isolated working directory that is cleaned up afterwards.
	workDir, err := os.MkdirTemp("", "pan-exec-*")
	if err != nil {
		return &Result{Error: "failed to create temp dir: " + err.Error()}, nil
	}
	defer os.RemoveAll(workDir)

	// Enforce a hard 30-second deadline regardless of what the caller passes.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd, err := buildCommand(ctx, lang, p.Code, workDir)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	// Capture combined stdout + stderr so the model sees everything.
	out, runErr := cmd.CombinedOutput()

	exitCode := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
			if exitCode == -1 {
				// Process was killed (e.g. timeout).
				exitCode = 124
			}
		} else {
			// The command itself could not be started.
			return &Result{Error: "execution failed: " + runErr.Error()}, nil
		}
	}

	res := codeExecResult{
		Output:   string(out),
		ExitCode: exitCode,
	}
	encoded, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return &Result{Error: "failed to encode result: " + err.Error()}, nil
	}
	return &Result{Output: string(encoded)}, nil
}

// buildCommand constructs an *exec.Cmd for the requested language and code.
func buildCommand(ctx context.Context, lang, code, workDir string) (*exec.Cmd, error) {
	var cmd *exec.Cmd

	switch lang {
	case "python":
		interpreter, err := findInterpreter("python3", "python")
		if err != nil {
			return nil, fmt.Errorf("python interpreter not found: %w", err)
		}
		cmd = exec.CommandContext(ctx, interpreter, "-c", code)

	case "javascript", "js":
		interpreter, err := findInterpreter("node")
		if err != nil {
			return nil, fmt.Errorf("node interpreter not found: %w", err)
		}
		cmd = exec.CommandContext(ctx, interpreter, "-e", code)

	case "bash", "sh", "shell":
		cmd = shellCommand(ctx, code)

	default:
		return nil, fmt.Errorf("unsupported language %q; use python, javascript, or bash", lang)
	}

	cmd.Dir = workDir
	return cmd, nil
}

// shellCommand returns a shell exec.Cmd appropriate for the current OS.
func shellCommand(ctx context.Context, code string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// On Windows prefer PowerShell Core (pwsh) or fall back to cmd.exe.
		if path, err := exec.LookPath("bash"); err == nil {
			// Git Bash / WSL bash available.
			return exec.CommandContext(ctx, path, "-c", code)
		}
		if path, err := exec.LookPath("pwsh"); err == nil {
			return exec.CommandContext(ctx, path, "-Command", code)
		}
		return exec.CommandContext(ctx, "cmd", "/C", code)
	}
	// POSIX systems: use /bin/sh for portability.
	return exec.CommandContext(ctx, "/bin/sh", "-c", code)
}

// findInterpreter returns the path to the first candidate found in PATH.
func findInterpreter(candidates ...string) (string, error) {
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("none of %v found in PATH", candidates)
}
