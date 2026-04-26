package gateway

import (
	"testing"
)

// Phase 13 WS#13.G — gateway-level test for the env-var gate that
// controls prompt-redaction wiring. The full streaming round-trip is
// covered by internal/llm/redact_test.go (which exercises the bridge
// helpers); this test just pins the on/off contract of the gate.
func TestPromptRedactionEnabled_Gate(t *testing.T) {
	cases := []struct {
		env  string
		set  bool
		want bool
	}{
		{set: false, want: false}, // unset → off (default)
		{env: "", set: true, want: false},
		{env: "0", set: true, want: false},
		{env: "false", set: true, want: false},
		{env: "FALSE", set: true, want: false},
		{env: "1", set: true, want: true},
		{env: "true", set: true, want: true},
		{env: "yes", set: true, want: true}, // any other truthy non-empty
	}
	for _, tc := range cases {
		name := "unset"
		if tc.set {
			name = tc.env
			t.Setenv("PAN_AGENT_REDACT_PROMPTS", tc.env)
		} else {
			// t.Setenv with an unset is awkward; force-clear by
			// setting then deleting via a fresh subtest is enough
			// because t.Setenv restores after the test.
			t.Setenv("PAN_AGENT_REDACT_PROMPTS", "")
		}
		t.Run(name, func(t *testing.T) {
			got := promptRedactionEnabled()
			if got != tc.want {
				t.Errorf("promptRedactionEnabled() = %v, want %v (env=%q)",
					got, tc.want, tc.env)
			}
		})
	}
}
