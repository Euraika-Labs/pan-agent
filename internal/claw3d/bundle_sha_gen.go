package claw3d

// This file exists solely to host the //go:generate directive that runs the
// sha-stamp tool. Keeping it separate from embed.go prevents surprise when
// the directive is edited — embed.go stays a pure declaration.

//go:generate go run ./sha-stamp
