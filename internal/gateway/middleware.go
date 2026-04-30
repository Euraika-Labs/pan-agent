package gateway

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// allowedOrigins is the set of origins permitted to call the gateway.
// Vite dev server and the Tauri app shell are the only expected callers.
var allowedOrigins = map[string]bool{
	"http://localhost:5173":   true,
	"http://127.0.0.1:5173":   true,
	"tauri://localhost":       true,
	"http://tauri.localhost":  true,
	"https://tauri.localhost": true,
	"http://asset.localhost":  true,
	"https://asset.localhost": true,
	"http://ipc.localhost":    true,
	"https://ipc.localhost":   true,
	"null":                    true,
}

// allowedHosts is the set of Host-header values accepted at the top of
// every request. A DNS-rebinding attacker can lure a browser into sending
// requests to 127.0.0.1:8642 with Host: malicious.com — SSE GET requests
// bypass CORS preflight, so we must validate Host explicitly.
var allowedHosts = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"[::1]":     true,
	"::1":       true,
}

// authToken is the optional shared secret required when PAN_AGENT_AUTH_TOKEN
// is set (or when bound to a non-loopback host). Requests must present
// Authorization: Bearer <token> or X-Pan-Agent-Token: <token>.
func authTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv("PAN_AGENT_AUTH_TOKEN"))
}

// withMiddleware wraps handler with CORS headers, Host-header validation,
// bearer-auth enforcement, and request logging. Applied once at server
// construction so every route benefits.
func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ---------------------------------------------------------------
		// Host-header validation (DNS-rebinding defense). The Host may
		// carry a port — strip it before comparing.
		// ---------------------------------------------------------------
		hostOnly := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			hostOnly = h
		}
		hostOnly = strings.TrimPrefix(strings.TrimSuffix(hostOnly, "]"), "[")
		if !allowedHosts[strings.ToLower(hostOnly)] {
			// Deliberate 421 (Misdirected Request) so monitoring tools can
			// distinguish rebinding attempts from regular 4xx noise.
			http.Error(w, "host not allowed", http.StatusMisdirectedRequest)
			return
		}

		// ---------------------------------------------------------------
		// Optional bearer-token authentication.
		// ---------------------------------------------------------------
		if tok := authTokenFromEnv(); tok != "" {
			got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if got == "" {
				got = strings.TrimSpace(r.Header.Get("X-Pan-Agent-Token"))
			}
			// Use constant-time comparison to avoid timing attacks.
			if !constantTimeEqual(got, tok) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// ---------------------------------------------------------------
		// CORS — only allow the Vite dev server and the Tauri app shell.
		// Hardened per M2: when Origin is present but NOT in the
		// allowlist, we reject preflight with 403. Previously an unknown
		// Origin silently fell through without Access-Control-Allow-Origin
		// headers — the browser then blocked the response, which is the
		// right outcome but obscured the cause. Explicit 403 makes the
		// mismatch visible in devtools and preserves the fact that
		// a non-allowlisted origin cannot trigger a state-changing request.
		// Access-Control-Allow-Credentials is deliberately omitted —
		// the gateway uses bearer-token / cookie-less auth.
		// ---------------------------------------------------------------
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !allowedOrigins[origin] {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Session-ID, X-Pan-Agent-Token")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")

		// Handle pre-flight immediately.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// ---------------------------------------------------------------
		// Logging — record method, path, and wall-clock duration.
		// ---------------------------------------------------------------
		start := time.Now()
		lw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(lw, r)

		duration := time.Since(start)
		fmt.Printf("[gateway] %s %s %d %s\n",
			r.Method, r.URL.Path, lw.statusCode, duration.Round(time.Millisecond))
	})
}

// loggingResponseWriter wraps http.ResponseWriter so we can capture the HTTP
// status code written by downstream handlers.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader captures the status code before forwarding.
func (lw *loggingResponseWriter) WriteHeader(code int) {
	if !lw.wroteHeader {
		lw.statusCode = code
		lw.wroteHeader = true
	}
	lw.ResponseWriter.WriteHeader(code)
}

// Write ensures that an implicit WriteHeader(200) is recorded when the
// handler writes a body without calling WriteHeader explicitly.
func (lw *loggingResponseWriter) Write(b []byte) (int, error) {
	if !lw.wroteHeader {
		lw.WriteHeader(http.StatusOK)
	}
	return lw.ResponseWriter.Write(b)
}

// Flush forwards the Flush call to the underlying writer, allowing SSE
// streaming handlers to push events immediately. If the underlying writer does
// not implement http.Flusher this is a no-op.
func (lw *loggingResponseWriter) Flush() {
	if f, ok := lw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack delegates to the underlying ResponseWriter when it implements
// http.Hijacker. This is required so that WebSocket upgrade handlers (which
// use gorilla/websocket or the stdlib upgrader) can take over the raw TCP
// connection. Without this delegation, upgrader.Upgrade returns 500 because
// the loggingResponseWriter wrapper hides the Hijacker interface.
func (lw *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := lw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("loggingResponseWriter: underlying ResponseWriter does not implement http.Hijacker")
	}
	return h.Hijack()
}

// constantTimeEqual compares two byte strings in constant time. Uses
// crypto/subtle.ConstantTimeCompare rather than a hand-rolled loop with
// an early length-mismatch return — the stdlib version internally handles
// the length branch uniformly, which a naive `if len(a) != len(b) { return }`
// does not (it leaks the expected secret length via timing).
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
