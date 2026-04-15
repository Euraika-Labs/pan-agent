package claw3d

import (
	"context"
	"encoding/json"
	"sync/atomic"
)

// Envelope is the tolerant-decode shape for any inbound frame on the Claw3D
// gateway protocol (v3). The wire format has three frame types: "req" sent by
// the client, "res" returned by us, "event" pushed by us. A single struct is
// simpler than type-specific decoders at the cost of a few empty fields per
// frame — acceptable for a protocol of this size.
//
// OK is a pointer so the outbound encoder can distinguish "this is a res frame
// that failed" from "no OK field at all" (event/req frames). Inbound, we do
// not rely on OK for dispatch; the Type discriminator carries that.
type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Event   string          `json:"event,omitempty"`
	Seq     uint64          `json:"seq,omitempty"`
	OK      *bool           `json:"ok,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *WireError      `json:"error,omitempty"`
}

// WireError is the stable error shape returned on failed req handlers.
type WireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Handler runs inside the client's read goroutine. The returned value is
// JSON-marshalled into the response payload; a returned error becomes a wire
// error frame via dispatch().
type Handler func(ctx context.Context, c *adapterClient, params json.RawMessage) (any, error)

// methods is the global handler registry populated from init() in
// handlers_*.go files. Lookup is O(1) on each req frame.
var methods = map[string]Handler{}

func registerMethod(name string, h Handler) {
	if _, dup := methods[name]; dup {
		panic("claw3d: duplicate method: " + name)
	}
	methods[name] = h
}

var seqCounter uint64

func nextSeq() uint64 { return atomic.AddUint64(&seqCounter, 1) }

// dispatch decodes one inbound frame and routes it. Runs on the client's read
// goroutine. Malformed or unknown frames get a wire-error response; panics in
// handlers are NOT recovered here — let the process supervisor catch them.
func dispatch(ctx context.Context, c *adapterClient, raw []byte) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		c.send(marshalErrorFrame("", "bad_frame", err.Error()))
		return
	}
	if env.Type != "req" || env.ID == "" || env.Method == "" {
		c.send(marshalErrorFrame(env.ID, "bad_envelope", `"type":"req" + id + method required`))
		return
	}
	h, ok := methods[env.Method]
	if !ok {
		c.send(marshalErrorFrame(env.ID, "unknown_method", env.Method))
		return
	}
	payload, err := h(ctx, c, env.Params)
	if err != nil {
		c.send(marshalErrorFrame(env.ID, "handler_error", err.Error()))
		return
	}
	raw2, _ := json.Marshal(payload)
	c.send(marshalResFrame(env.ID, true, raw2, nil))
}

// marshalResFrame returns a response envelope. OK is always set explicitly —
// never omitted — so clients can trust that presence of OK distinguishes a
// response from a push event.
func marshalResFrame(id string, ok bool, payload json.RawMessage, werr *WireError) []byte {
	type resOut struct {
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		OK      bool            `json:"ok"`
		Payload json.RawMessage `json:"payload,omitempty"`
		Error   *WireError      `json:"error,omitempty"`
	}
	b, _ := json.Marshal(resOut{Type: "res", ID: id, OK: ok, Payload: payload, Error: werr})
	return b
}

func marshalErrorFrame(id, code, msg string) []byte {
	return marshalResFrame(id, false, nil, &WireError{Code: code, Message: msg})
}

// marshalEventFrame builds a server-pushed event with a fresh monotonic seq.
// Clients use seq to detect presence gaps and issue resync calls (see Gate-2
// refinement — full presence resync semantics land at M2).
func marshalEventFrame(name string, payload any) []byte {
	type evtOut struct {
		Type    string          `json:"type"`
		Event   string          `json:"event"`
		Seq     uint64          `json:"seq"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	raw, _ := json.Marshal(payload)
	b, _ := json.Marshal(evtOut{Type: "event", Event: name, Seq: nextSeq(), Payload: raw})
	return b
}
