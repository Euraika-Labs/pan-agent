// Package version holds the build-time version information for pan-agent.
// All three variables are intended to be overridden at link time via ldflags:
//
//	go build -ldflags "-X github.com/euraika-labs/pan-agent/internal/version.Version=1.0.0 \
//	                   -X github.com/euraika-labs/pan-agent/internal/version.Commit=$(git rev-parse --short HEAD) \
//	                   -X github.com/euraika-labs/pan-agent/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
//	         ./cmd/pan-agent
package version

// Version is the semantic version string (e.g. "0.4.0").
// Overridden via -ldflags at build time.
var Version = "0.4.3"

// Commit is the short Git commit hash at build time.
// Overridden via -ldflags at build time.
var Commit = "dev"

// Date is the UTC timestamp of the build in RFC 3339 format.
// Overridden via -ldflags at build time.
var Date = "unknown"
