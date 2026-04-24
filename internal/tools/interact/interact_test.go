package interact

import (
	"context"
	"testing"
)

func TestRouterEmptyIntent(t *testing.T) {
	r := NewRouter()
	resp := r.Route(context.Background(), Request{})
	if resp.Error == "" {
		t.Error("expected error for empty intent")
	}
	if resp.Layer != LayerUnsupported {
		t.Errorf("layer = %q, want %q", resp.Layer, LayerUnsupported)
	}
}

func TestToolParameters(t *testing.T) {
	params := ToolParameters()
	if len(params) == 0 {
		t.Error("ToolParameters returned empty")
	}
	if params[0] != '{' {
		t.Error("ToolParameters not valid JSON object")
	}
}

func TestSafeAppName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"Safari", true},
		{"Google Chrome", true},
		{"Mail.app", true},
		{"VS Code", true},
		{"my-app", true},
		{"", false},
		{"; rm -rf /", false},
		{"app\nname", false},
		{"../../../etc/passwd", false},
		{"$(whoami)", false},
		{"`whoami`", false},
		{"app|cat", false},
		{"a" + string(make([]byte, 200)), false},
	}

	for _, tt := range tests {
		got := safeAppName.MatchString(tt.name)
		if got != tt.ok {
			t.Errorf("safeAppName(%q) = %v, want %v", tt.name, got, tt.ok)
		}
	}
}

func TestDirectAPIAvailable(t *testing.T) {
	d := NewDirectAPI()
	_ = d.Available()
}

func TestVisionNewVision(t *testing.T) {
	v := NewVision()
	if v == nil {
		t.Error("NewVision returned nil")
	}
}
