package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/euraika-labs/pan-agent/internal/cron"
)

// TestBuildCronPlanJSON_Roundtrips verifies that the cron task-plan
// serialiser produces well-formed JSON for the happy path. The fields
// land in the structure the runner expects — id / tool / params.prompt /
// description — and the byte stream parses back into a generic map.
func TestBuildCronPlanJSON_Roundtrips(t *testing.T) {
	out, err := buildCronPlanJSON(cron.Job{
		ID:     "abc123",
		Name:   "nightly",
		Prompt: "Summarise yesterday's emails.",
	})
	if err != nil {
		t.Fatalf("buildCronPlanJSON: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nout=%s", err, out)
	}

	steps, ok := parsed["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("expected one step, got %v", parsed["steps"])
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("step is not a JSON object: %T", steps[0])
	}

	if got, want := step["id"], "cron-abc123"; got != want {
		t.Errorf("id = %v, want %q", got, want)
	}
	if got, want := step["tool"], "chat"; got != want {
		t.Errorf("tool = %v, want %q", got, want)
	}
	if got, want := step["description"], "Cron: nightly"; got != want {
		t.Errorf("description = %v, want %q", got, want)
	}
	params, ok := step["params"].(map[string]any)
	if !ok {
		t.Fatalf("params is not an object: %T", step["params"])
	}
	if got, want := params["prompt"], "Summarise yesterday's emails."; got != want {
		t.Errorf("prompt = %v, want %q", got, want)
	}
}

// TestBuildCronPlanJSON_EscapesAdversarialQuotes is the regression test
// for the original CodeQL go/unsafe-quoting alert. The previous
// fmt.Sprintf-based serialiser was *technically* safe because its
// helper used json.Marshal per field, but the static analyser could
// not prove that and a future edit to the format string could regress.
// Now that we Marshal a typed struct, an adversarial input with raw
// double quotes, backslashes, newlines, and unicode round-trips
// without breaking the surrounding JSON.
func TestBuildCronPlanJSON_EscapesAdversarialQuotes(t *testing.T) {
	hostile := `"; DROP TABLE tasks; --` + "\n\\\"\xe2\x98\x83 ☃"
	out, err := buildCronPlanJSON(cron.Job{
		ID:     `id"with"quotes`,
		Name:   "name\nwith\nnewlines",
		Prompt: hostile,
	})
	if err != nil {
		t.Fatalf("buildCronPlanJSON: %v", err)
	}

	// Sanity: it must parse, and the prompt must round-trip exactly —
	// not chopped off at the embedded `"` or interpreted as JSON syntax.
	var parsed struct {
		Steps []struct {
			ID          string         `json:"id"`
			Tool        string         `json:"tool"`
			Params      map[string]any `json:"params"`
			Description string         `json:"description"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nout=%s", err, out)
	}
	if len(parsed.Steps) != 1 {
		t.Fatalf("expected one step, got %d", len(parsed.Steps))
	}
	if got, want := parsed.Steps[0].Params["prompt"], hostile; got != want {
		t.Errorf("prompt did not round-trip:\n  got  %q\n  want %q", got, want)
	}
	if got, want := parsed.Steps[0].ID, `cron-id"with"quotes`; got != want {
		t.Errorf("id did not round-trip:\n  got  %q\n  want %q", got, want)
	}
	if !strings.Contains(parsed.Steps[0].Description, "name\nwith\nnewlines") {
		t.Errorf("newlines stripped from description: %q", parsed.Steps[0].Description)
	}
}
