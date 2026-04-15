package claw3d

import (
	"embed"
	"io/fs"
)

// bundleFS holds the pre-built Claw3D static export embedded into the Go
// binary. The "all:" prefix preserves filenames starting with "_" or "."
// (Next.js emits _next/*). At M1 this is a single placeholder index.html;
// at M3 the full next export tree lands here.
//
//go:embed all:bundle
var bundleFS embed.FS

// Bundle returns the served static tree rooted inside the bundle/ subdir.
// Callers use http.FileServerFS(Bundle()) — never poke embed.FS directly,
// so the on-disk layout stays a package implementation detail.
func Bundle() fs.FS {
	sub, _ := fs.Sub(bundleFS, "bundle")
	return sub
}

// BundleSHA256 is the integrity fingerprint of the embedded bundle, reported
// to the browser via /office/config.js and surfaced in pan-agent doctor.
//
// It is a var (not a const) so release CI can override it via ldflags:
//
//	go build -ldflags "-X .../claw3d.BundleSHA256=<sha>" ...
//
// to guarantee the reported value is byte-for-byte the CI-built artifact,
// independent of the local go generate state. On dev machines, it is
// initialised from the BundleSHA256Generated constant written by the
// sha-stamp tool in internal/claw3d/sha-stamp (run via `go generate`).
var BundleSHA256 = BundleSHA256Generated
