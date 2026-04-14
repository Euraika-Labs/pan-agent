package claw3d

import (
	"strings"
	"sync"
	"time"
)

// logWriter is an io.Writer that appends each written chunk into a shared
// in-process ring buffer. Both the dev server and the adapter pipe their
// stdout/stderr through instances with different prefixes so
// GET /v1/office/logs can surface the tail of recent output.
//
// Why: before this existed, the adapter would silently exit when its port
// (18789) was reserved by Windows svchost. The /office/logs endpoint
// returned an empty string, the UI had no signal, and the diagnostic path
// was "wait for the user to paste dev-tools errors into chat." Capturing
// stderr fixes that loop.
type logWriter struct {
	prefix string
}

const logRingCap = 300 // lines; ~60kB with typical Node output

var (
	logMu   sync.Mutex
	logRing []string
)

// Write implements io.Writer. Splits on newlines and appends per-line into
// the ring buffer with a prefix + timestamp.
func (w *logWriter) Write(p []byte) (int, error) {
	text := string(p)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		appendLog(w.prefix, line)
	}
	return len(p), nil
}

func appendLog(prefix, line string) {
	stamp := time.Now().Format("15:04:05")
	entry := stamp + " [" + prefix + "] " + line
	logMu.Lock()
	defer logMu.Unlock()
	logRing = append(logRing, entry)
	if len(logRing) > logRingCap {
		logRing = logRing[len(logRing)-logRingCap:]
	}
}

// GetLogs returns the recent log tail as a single string, newest at bottom.
// Consumed by gateway.handleOfficeLogs.
func GetLogs() string {
	logMu.Lock()
	defer logMu.Unlock()
	return strings.Join(logRing, "\n")
}

// ClearLogs wipes the ring; exposed for tests and for a manual "clear logs"
// UI button.
func ClearLogs() {
	logMu.Lock()
	defer logMu.Unlock()
	logRing = nil
}
