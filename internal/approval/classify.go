package approval

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
)

// Classify inspects a tool call's name + JSON arguments and returns the
// strongest-matching approval level. This is the single entry point the
// gateway uses to gate tool execution — it replaces the prior 4-tool
// allowlist with content-aware classification backed by approval.Check.
//
// Contract:
//   - Safe         → no approval needed
//   - Dangerous    → one-click confirm
//   - Catastrophic → typed confirmation (UI decision; we only surface the
//     level + pattern key so the frontend can render accordingly)
//
// For tools that do not naturally map to a shell command we still enforce
// approval at Dangerous, but set a tool-specific PatternKey so the UI can
// render the right copy.
func Classify(toolName string, argumentsJSON string) ApprovalCheck {
	switch toolName {
	case "terminal":
		return classifyTerminal(argumentsJSON)
	case "code_execution":
		return classifyCodeExec(argumentsJSON)
	case "filesystem":
		return classifyFilesystem(argumentsJSON)
	case "browser":
		return classifyBrowser(argumentsJSON)
	}
	// Unknown tool — Safe (the executeToolCall still only reaches Classify
	// for gated tools; this branch is defensive).
	return ApprovalCheck{Level: Safe}
}

func classifyTerminal(argsJSON string) ApprovalCheck {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &p); err != nil || p.Command == "" {
		return ApprovalCheck{Level: Dangerous, PatternKey: "terminal.unparsed"}
	}
	chk := Check(p.Command)
	if chk.Level == Safe {
		// Terminal commands always need at least a single-click confirmation
		// because the classifier only catches known-bad patterns. Leave
		// unknown commands at Dangerous so the UI still gates them.
		return ApprovalCheck{Level: Dangerous, PatternKey: "terminal.default", Description: "Shell command"}
	}
	return chk
}

func classifyCodeExec(argsJSON string) ApprovalCheck {
	var p struct {
		Language string `json:"language"`
		Code     string `json:"code"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &p); err != nil || p.Code == "" {
		return ApprovalCheck{Level: Dangerous, PatternKey: "code_execution.unparsed"}
	}
	// Run classifier on code; bash snippets commonly trigger terminal patterns.
	chk := Check(p.Code)
	if chk.Level == Safe {
		return ApprovalCheck{Level: Dangerous, PatternKey: "code_execution.default", Description: p.Language + " code"}
	}
	return chk
}

// catastrophicFSPaths are roots where any destructive write is catastrophic.
var catastrophicFSPaths = []string{
	"/", "/etc", "/usr", "/bin", "/sbin", "/boot",
	"/System", "/Library",
	"C:\\", "C:\\Windows", "C:\\Program Files", "C:\\Program Files (x86)",
}

func classifyFilesystem(argsJSON string) ApprovalCheck {
	var p struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &p); err != nil {
		return ApprovalCheck{Level: Dangerous, PatternKey: "filesystem.unparsed"}
	}
	// read/list/search are read-only but still Dangerous — they can exfil.
	readOnly := map[string]bool{"read": true, "list": true, "search": true}
	if readOnly[p.Operation] {
		return ApprovalCheck{Level: Dangerous, PatternKey: "filesystem." + p.Operation, Description: "Read " + p.Path}
	}
	// Destructive ops targeting a protected root = Catastrophic.
	clean := filepath.Clean(p.Path)
	for _, root := range catastrophicFSPaths {
		if strings.EqualFold(clean, root) || strings.EqualFold(clean, strings.TrimRight(root, "\\/")) {
			return ApprovalCheck{
				Level:       Catastrophic,
				PatternKey:  "filesystem.protected_root",
				Description: p.Operation + " on system root " + clean,
			}
		}
	}
	return ApprovalCheck{
		Level:       Dangerous,
		PatternKey:  "filesystem." + p.Operation,
		Description: p.Operation + " " + p.Path,
	}
}

func classifyBrowser(argsJSON string) ApprovalCheck {
	var p struct {
		Operation string `json:"operation"`
		URL       string `json:"url"`
		Script    string `json:"script"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &p); err != nil {
		return ApprovalCheck{Level: Dangerous, PatternKey: "browser.unparsed"}
	}
	// file:// or loopback = Catastrophic (local-file exfil, self-SSRF).
	if p.Operation == "navigate" && p.URL != "" {
		if u, err := url.Parse(p.URL); err == nil {
			scheme := strings.ToLower(u.Scheme)
			if scheme == "file" {
				return ApprovalCheck{
					Level:       Catastrophic,
					PatternKey:  "browser.file_scheme",
					Description: "navigate to " + p.URL,
				}
			}
			host := strings.ToLower(u.Hostname())
			if host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.") || host == "::1" {
				return ApprovalCheck{
					Level:       Catastrophic,
					PatternKey:  "browser.loopback",
					Description: "navigate to " + p.URL,
				}
			}
		}
	}
	if p.Operation == "evaluate" {
		return ApprovalCheck{Level: Dangerous, PatternKey: "browser.evaluate", Description: "eval JS in page"}
	}
	return ApprovalCheck{Level: Dangerous, PatternKey: "browser." + p.Operation, Description: p.Operation}
}
