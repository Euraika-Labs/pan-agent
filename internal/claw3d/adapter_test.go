package claw3d

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDispatch_StatusRoundTrip verifies the core request/response loop end-to-
// end: a status req frame produces a res frame with the expected shape. This
// is the minimum viable conformance test — M3/M4 expand into full golden
// fixtures captured from the Node reference.
func TestDispatch_StatusRoundTrip(t *testing.T) {
	// Build a test client that captures sends into a slice rather than a WS.
	sent := make([][]byte, 0, 4)
	stub := &testSink{send: func(b []byte) { sent = append(sent, b) }}

	req := marshalReq(t, "42", "status", nil)
	dispatchTest(stub, req)

	if len(sent) != 1 {
		t.Fatalf("expected 1 outbound frame, got %d", len(sent))
	}
	var env Envelope
	if err := json.Unmarshal(sent[0], &env); err != nil {
		t.Fatalf("outbound frame is not valid JSON: %v", err)
	}
	if env.Type != "res" {
		t.Fatalf("expected type=res, got %q", env.Type)
	}
	if env.ID != "42" {
		t.Fatalf("expected id=42, got %q", env.ID)
	}
	if env.OK == nil || !*env.OK {
		t.Fatalf("expected ok=true, got %+v", env.OK)
	}
	var payload StatusPayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if payload.ProtocolVersion != 3 {
		t.Errorf("expected protocolVersion=3, got %d", payload.ProtocolVersion)
	}
	if payload.AdapterType != "hermes" {
		t.Errorf("expected adapterType=hermes, got %q", payload.AdapterType)
	}
}

// TestDispatch_UnknownMethod asserts the protocol contract for bad requests.
func TestDispatch_UnknownMethod(t *testing.T) {
	sent := make([][]byte, 0, 1)
	stub := &testSink{send: func(b []byte) { sent = append(sent, b) }}

	req := marshalReq(t, "1", "not.a.real.method", nil)
	dispatchTest(stub, req)

	if len(sent) != 1 {
		t.Fatalf("expected 1 outbound frame, got %d", len(sent))
	}
	if !strings.Contains(string(sent[0]), `"unknown_method"`) {
		t.Errorf("expected unknown_method error in frame, got: %s", sent[0])
	}
}

// TestIsCritical_DropsEventsNotRPC guards against regressions in the
// backpressure policy: responses and chat deltas must never be silently
// dropped; presence/heartbeat are fair game.
func TestIsCritical_DropsEventsNotRPC(t *testing.T) {
	cases := []struct {
		name     string
		frame    string
		critical bool
	}{
		{"res frame", `{"type":"res","id":"1","ok":true}`, true},
		{"req frame", `{"type":"req","id":"2","method":"status"}`, true},
		{"chat event", `{"type":"event","event":"chat","seq":5}`, true},
		{"presence event", `{"type":"event","event":"presence","seq":5}`, false},
		{"heartbeat event", `{"type":"event","event":"heartbeat","seq":5}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isCritical([]byte(tc.frame))
			if got != tc.critical {
				t.Errorf("isCritical(%s) = %v, want %v", tc.frame, got, tc.critical)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test helpers — a minimal stand-in for adapterClient that records sends
// without touching a real WebSocket or the hub.
// ---------------------------------------------------------------------------

type testSink struct{ send func([]byte) }

// dispatchTest invokes the package dispatch with a test client, then
// synchronously drains c.out into the sink. The outbox is buffered so
// dispatch's send() enqueues without blocking; we read after the call
// returns, which makes the test deterministic (no goroutine scheduling
// required).
func dispatchTest(sink *testSink, raw []byte) {
	c := &adapterClient{out: make(chan []byte, 16)}
	dispatch(nil, c, raw)
	// Drain whatever dispatch produced — at M2 each req produces at most one
	// synchronous response frame.
	for {
		select {
		case b := <-c.out:
			sink.send(b)
		default:
			return
		}
	}
}

func marshalReq(t *testing.T, id, method string, params any) []byte {
	t.Helper()
	type reqFrame struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}
	b, err := json.Marshal(reqFrame{Type: "req", ID: id, Method: method, Params: params})
	if err != nil {
		t.Fatalf("marshalReq: %v", err)
	}
	return b
}
