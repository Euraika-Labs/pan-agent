package claw3d

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestPortOpen pins the TCP-dial predicate. The Status() false-positive
// incident happened because we only checked isAlive; this test fences
// the new port check against regressions (empty port, unbound port,
// bound-but-silent port, real HTTP server).
func TestPortOpen(t *testing.T) {
	if portOpen(0) {
		t.Error("portOpen(0) = true, want false")
	}
	// Random unbound port — ephemeral, should never be listening.
	if portOpen(1) {
		t.Error("portOpen(1) = true, want false (port 1 should be unbound on a dev box)")
	}
	// Bound TCP listener with no app serving → SYN still accepted.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	p := ln.Addr().(*net.TCPAddr).Port
	if !portOpen(p) {
		t.Errorf("portOpen(%d) = false on bound listener, want true", p)
	}
}

// TestHttpAliveResponding confirms httpAlive returns true for a real
// HTTP server answering on the loopback. Any status code counts —
// what we care about is "did the app respond at all".
func TestHttpAliveResponding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // 418 still counts as "alive"
	}))
	defer srv.Close()

	// httptest server URL is http://127.0.0.1:PORT.
	u := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	port, err := strconv.Atoi(u)
	if err != nil {
		t.Fatalf("parse port from %s: %v", srv.URL, err)
	}
	if !httpAlive(port) {
		t.Errorf("httpAlive(%d) = false on live HTTP server, want true", port)
	}
}

// TestHttpAliveSilentListener is the regression fence for the Next.js
// hang we saw: a bare TCP listener that never speaks HTTP should be
// treated as NOT alive. portOpen would say true; httpAlive must say
// false.
func TestHttpAliveSilentListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	// Sanity check: the bare listener IS reachable at TCP level.
	if !portOpen(port) {
		t.Fatalf("portOpen(%d) = false — setup broken", port)
	}

	// But httpAlive should time out and return false.
	if httpAlive(port) {
		t.Errorf("httpAlive(%d) = true on silent listener, want false", port)
	}
}

// TestHttpAliveUnboundPort confirms an obviously-unbound port returns
// false quickly (the dial fails fast, no timeout needed).
func TestHttpAliveUnboundPort(t *testing.T) {
	if httpAlive(1) {
		t.Error("httpAlive(1) = true, want false")
	}
	if httpAlive(0) {
		t.Error("httpAlive(0) = true, want false")
	}
}
