package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// Phase 13 WS#13.D — `pan-agent tools list/describe` tests.
//
// The tool registry registers via init() — every test in this
// package runs against the real Default registry, so the tools
// shipped in internal/tools (clarify, browser, stripe, slack,
// notion, jira, etc.) are all visible.

// captureStdout runs fn with os.Stdout redirected to a buffer + returns
// the captured bytes. Used because the cmdTools helpers print to stdout
// directly.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	errCh := make(chan error, 1)
	go func() { errCh <- fn() }()

	// Closing the writer unblocks the reader. We have to do that
	// AFTER the function returns, so we drain the function first.
	runErr := <-errCh
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), runErr
}

// ---------------------------------------------------------------------------
// Top-level dispatch
// ---------------------------------------------------------------------------

func TestCmdTools_NoAction(t *testing.T) {
	if err := cmdTools(nil); err == nil {
		t.Error("expected missing-action error")
	}
}

func TestCmdTools_UnknownAction(t *testing.T) {
	if err := cmdTools([]string{"banana"}); err == nil ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("got %v, want unknown-action error", err)
	}
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func TestCmdToolsList_Plain(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return cmdToolsList(nil)
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Sanity: a few stable core tools must always appear. The WS#13.D
	// additions (stripe/slack/notion/jira) live on parallel branches
	// at the time this test was written; checking for them here would
	// couple this slice to merge order. Stick to tools that are on
	// main today.
	for _, want := range []string{"clarify", "browser"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing tool %q\n%s", want, out)
		}
	}
}

func TestCmdToolsList_SortedOutput(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return cmdToolsList(nil)
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Each line is "<name>  <description>". Pull names + verify sorted.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var names []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			names = append(names, fields[0])
		}
	}
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("output not sorted: %q before %q",
				names[i-1], names[i])
			break
		}
	}
}

func TestCmdToolsList_JSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return cmdToolsList([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}
	var arr []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if len(arr) == 0 {
		t.Fatal("JSON output has zero tools")
	}
	for _, t2 := range arr {
		if t2.Name == "" {
			t.Errorf("tool with empty name: %+v", t2)
		}
		if t2.Description == "" {
			t.Errorf("tool %q: empty description", t2.Name)
		}
	}
}

func TestCmdToolsList_DescriptionTruncation(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return cmdToolsList(nil)
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// No single output line should exceed ~140 chars (22 col name +
	// 2 sep + 100 desc + " …" + a small fudge factor).
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 200 {
			t.Errorf("line longer than expected truncation: %d chars\n%s",
				len(line), line)
		}
	}
}

// ---------------------------------------------------------------------------
// describe
// ---------------------------------------------------------------------------

func TestCmdToolsDescribe_HappyPath(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return cmdToolsDescribe([]string{"clarify"})
	})
	if err != nil {
		t.Fatalf("describe clarify: %v", err)
	}
	if !strings.Contains(out, "Tool:") || !strings.Contains(out, "clarify") {
		t.Errorf("output missing Tool: header for clarify\n%s", out)
	}
	if !strings.Contains(out, "Parameters") {
		t.Errorf("output missing Parameters block\n%s", out)
	}
	// The schema must be valid JSON. Find the section and parse it.
	idx := strings.Index(out, "{")
	if idx < 0 {
		t.Fatalf("no JSON found in output: %s", out)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(out[idx:]), &schema); err != nil {
		t.Errorf("schema is not valid JSON: %v", err)
	}
}

func TestCmdToolsDescribe_NoArg(t *testing.T) {
	if err := cmdToolsDescribe(nil); err == nil {
		t.Error("expected missing-name error")
	}
}

func TestCmdToolsDescribe_UnknownTool(t *testing.T) {
	if err := cmdToolsDescribe([]string{"no-such-tool-exists"}); err == nil {
		t.Error("expected no-such-tool error")
	}
}
