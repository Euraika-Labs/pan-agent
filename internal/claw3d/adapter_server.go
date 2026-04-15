package claw3d

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"sync"

	"github.com/euraika-labs/pan-agent/internal/storage"
	"github.com/gorilla/websocket"
)

// remoteIP extracts the client IP from an http.Request for auth-gate
// keying. Prefers RemoteAddr (set by the transport layer); the port is
// stripped so the bucket/lockout state keys on host alone. Falls back
// to the raw RemoteAddr when SplitHostPort fails (IPv6 edge cases).
//
// NOTE: we deliberately do NOT consult X-Forwarded-For here. Pan-agent
// binds 127.0.0.1 by default and the only legitimate callers are the
// same-host webview / CLI. Reverse-proxy deployments behind e.g. Nginx
// would need a forwarded-for policy layer the caller owns explicitly.
func remoteIP(r *http.Request) string {
	if r == nil || r.RemoteAddr == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// hostSafe matches the character class that is safe to interpolate into a
// JavaScript string literal after Go's %q quoting: DNS labels, IPv4 dotted
// addresses, IPv6 inside brackets, standard port separators. Anything else
// (quotes, backticks, angle brackets, backslashes, whitespace) is rejected so
// a malicious X-Forwarded-Host can't escape the quoting context in config.js.
var hostSafe = regexp.MustCompile(`^[a-zA-Z0-9.:\[\]\-_]+$`)

// newUpgrader builds a gorilla Upgrader bound to a specific adapter so
// its CheckOrigin can consult the adapter's strictOrigin flag. M5 makes
// CheckOrigin stateful (dependent on the per-instance config). We used
// to have a single package-level upgrader, but once originAllowed grew
// a strictOrigin parameter that stopped being a clean pattern.
func newUpgrader(a *Adapter) *websocket.Upgrader {
	return &websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(r.Header.Get("Origin"), a.strictOrigin)
		},
	}
}

// Adapter is the Claw3D gateway: serves the embedded bundle at /office/* and
// terminates the WebSocket protocol at /office/ws. It is registered onto the
// main gateway mux via Register; lifecycle is tied to the gateway itself.
//
// The Adapter owns no long-running goroutines of its own — it is a passive
// request handler. Per-connection goroutines are created in handleWS.
type Adapter struct {
	hub      *Hub
	sessions *sessionStore
	db       *storage.DB
	upgrader *websocket.Upgrader
	// publicHost overrides r.Host when serving /office/config.js. Empty
	// means "auto-detect via X-Forwarded-Host then r.Host" — the Gate-2
	// refinement for reverse-proxy and VPN-tunnel deployments.
	publicHost string
	// strictOrigin (M5) rejects WS upgrades with empty Origin headers
	// when true. Default false preserves permissive behaviour for
	// loopback CLI/test clients.
	strictOrigin bool
}

// NewAdapter constructs an adapter with defaults. The db parameter may be nil
// during tests/smoke runs; M2 handlers that require persistence return a
// clear error when db is nil rather than nil-panicking.
//
// The strictOrigin flag (M5) gates empty-Origin WS upgrades. Callers
// should read this from config.GetOfficeConfig(profile).StrictOrigin.
func NewAdapter(publicHost string, db *storage.DB, strictOrigin bool) *Adapter {
	a := &Adapter{
		hub:          NewHub(),
		sessions:     newSessionStore(),
		db:           db,
		publicHost:   publicHost,
		strictOrigin: strictOrigin,
	}
	a.upgrader = newUpgrader(a)
	return a
}

// DB returns the adapter's backing store. Handlers reach it via the client's
// hub back-reference plus a package-level accessor; this keeps dispatch() from
// needing to know anything about persistence.
func (a *Adapter) DB() *storage.DB { return a.db }

// Register wires all /office/* handlers onto mux. Idempotent — call once at
// gateway startup.
func (a *Adapter) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /office/config.js", a.handleConfigJS)
	mux.HandleFunc("GET /office/ws", a.handleWS)
	mux.Handle("GET /office/", http.StripPrefix("/office/",
		a.withSession(http.FileServerFS(Bundle()))))
}

// Hub exposes the underlying hub so other packages can broadcast events
// (chat deltas, presence updates) without reaching into adapter internals.
func (a *Adapter) Hub() *Hub { return a.hub }

// withSession issues a session cookie on the first GET of any /office/*
// static asset so the subsequent WS upgrade can present it. This is the
// Gate-2 CSWSH mitigation: a bare origin header is insufficient; the WS
// must also carry a cookie only obtainable via an actual HTML load.
func (a *Adapter) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("claw3d_sess"); err != nil {
			_ = a.sessions.issue(w)
		}
		next.ServeHTTP(w, r)
	})
}

// publicHostFor returns the host:port clients should reach us at. Priority:
// explicit config > sanitized X-Forwarded-Host > Request.Host.
//
// Hardened after M1 code review: X-Forwarded-Host is attacker-controllable
// on any request that doesn't pass through a trusted reverse proxy. The
// value is interpolated into a JS string in config.js, so we must reject
// anything outside the hostSafe character class; failing values fall back
// to r.Host as if the header were absent.
func (a *Adapter) publicHostFor(r *http.Request) string {
	if a.publicHost != "" {
		return a.publicHost
	}
	if h := r.Header.Get("X-Forwarded-Host"); h != "" && hostSafe.MatchString(h) {
		return h
	}
	return r.Host
}

// handleConfigJS emits a tiny bootstrap script that the embedded Claw3D
// bundle sources before its main chunks. It injects WS URL + API base as
// window globals so the build-time NEXT_PUBLIC_GATEWAY_URL is never
// captured into the bundle at `next build` time.
func (a *Adapter) handleConfigJS(w http.ResponseWriter, r *http.Request) {
	host := a.publicHostFor(r)
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w,
		"window.__CLAW3D_WS_URL__=%q;\nwindow.__CLAW3D_API_BASE__=%q;\nwindow.__CLAW3D_BUNDLE_SHA__=%q;\n",
		"ws://"+host+"/office/ws",
		"http://"+host,
		BundleSHA256,
	)
}

// handleWS authenticates the upgrade, attaches the connection to the hub,
// and hands control to the reader/writer goroutine pair. Failed
// authentication returns 401 before any WS handshake happens.
//
// M5 auth polish runs three gates in order BEFORE the handshake:
//  1. isLockedOut — reject 429 if this remote is in a lockout window;
//     prevents a bot from learning anything about token validity during
//     the lockout.
//  2. allowRate — token-bucket check; 429 with Retry-After if exceeded.
//  3. verify — cookie/query token check; on failure, recordFailure()
//     which may flip lockout on. On success, recordSuccess() clears
//     consecutive-failure state so a one-off typo isn't punished.
//
// All three gates share a single audit row per failure so the dogfood
// log can reconstruct the sequence. The audit digest is just the
// remote IP — no hashing — matching existing AuditOffice convention
// (see internal/storage/office.go AuditOffice call sites).
func (a *Adapter) handleWS(w http.ResponseWriter, r *http.Request) {
	ip := remoteIP(r)

	// Gate 1: active lockout window — short-circuit before revealing
	// anything about session validity.
	if a.sessions.isLockedOut(ip) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		if a.db != nil {
			_ = a.db.AuditOffice("local", "auth.failure", ip, "lockout")
		}
		return
	}

	// Gate 2: token bucket. Legitimate users never hit this — burst=20
	// with 5/sec refill absorbs a reconnect storm comfortably.
	if !a.sessions.allowRate(ip) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		if a.db != nil {
			_ = a.db.AuditOffice("local", "auth.failure", ip, "rate_limited")
		}
		return
	}

	// Gate 3: session token verification. On failure, record it and
	// return 401. On lockout-flip, the next request hits Gate 1 above.
	if !a.sessions.verify(r) {
		lockedOut := a.sessions.recordFailure(ip)
		result := "denied"
		if lockedOut {
			result = "lockout"
		}
		if a.db != nil {
			_ = a.db.AuditOffice("local", "auth.failure", ip, result)
		}
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	a.sessions.recordSuccess(ip)

	conn, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// upgrader already wrote an appropriate error response.
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	c := &adapterClient{
		conn: conn, out: make(chan []byte, outboxCap),
		ctx: ctx, cancel: cancel, hub: a.hub, adapter: a,
	}
	a.hub.register(c)

	// Goroutine lifecycle — reader runs on this goroutine, writer on a
	// separate one. LIFO defer order guarantees:
	//   1. reader returns (naturally or via error) -> cancel() fires
	//   2. writer observes ctx.Done() and returns
	//   3. wg.Wait() unblocks after both have exited
	//   4. conn.Close() runs AFTER both goroutines have finished writing,
	//      so we never concurrently Close + WriteMessage (gorilla is not
	//      safe for concurrent Close+Write).
	//   5. hub.unregister runs last.
	//
	// This structure was hardened after Phase 4 code review flagged a
	// writer-leak / write-after-close race.
	var wg sync.WaitGroup
	defer a.hub.unregister(c)
	defer conn.Close()
	defer wg.Wait()

	wg.Add(1)
	go func() { defer wg.Done(); c.writer() }()

	c.send(marshalHelloOK())
	c.reader()
}

// marshalHelloOK builds the handshake event clients receive right after
// connecting. Structure is frozen by protocol v3; new fields may be added
// but no existing key may change shape.
func marshalHelloOK() []byte {
	methodNames := make([]string, 0, len(methods))
	for k := range methods {
		methodNames = append(methodNames, k)
	}
	return marshalEventFrame("hello-ok", map[string]any{
		"protocol":       3,
		"adapterType":    "hermes",
		"adapterVersion": "0.4.0-alpha",
		"features": map[string]any{
			"methods": methodNames,
			"events":  []string{"hello-ok", "chat", "presence", "heartbeat", "cron"},
		},
	})
}
