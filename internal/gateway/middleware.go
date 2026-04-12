package gateway

import (
	"fmt"
	"net/http"
	"time"
)

// withMiddleware wraps handler with CORS headers and request logging.
// It is applied once at server construction so every route benefits.
func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ---------------------------------------------------------------
		// CORS — allow all origins for local desktop development.
		// The gateway only binds to 127.0.0.1 so this is safe.
		// ---------------------------------------------------------------
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Session-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Type")

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
