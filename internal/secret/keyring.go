// Package secret provides a platform keyring wrapper and a deterministic
// secret-redaction pipeline for pan-agent. Every caller imports this package;
// no other package owns these primitives.
package secret

import (
	"errors"
	"regexp"
)

var (
	// ErrNotFound is returned when the requested key does not exist in the keyring.
	ErrNotFound = errors.New("secret: key not found")
	// ErrUnsupportedPlatform is returned on GOOS values without a keyring backend.
	ErrUnsupportedPlatform = errors.New("secret: keyring not supported on this platform")
	// ErrKeyringUnavailable is returned when the daemon/service is present but
	// temporarily inaccessible (locked, not running, access denied). Distinct
	// from ErrUnsupportedPlatform — this is a recoverable runtime condition.
	ErrKeyringUnavailable = errors.New("secret: keyring daemon unavailable")
	// ErrInvalidKey is returned when a key name fails keyNameRe validation.
	ErrInvalidKey = errors.New("secret: invalid key name")
)

const (
	// serviceName is the keyring namespace all pan-agent secrets live under.
	// Windows: target name prefix. macOS: -s argument to `security`.
	// Linux: attribute {"service": serviceName} on Secret Service items.
	serviceName = "pan-agent"
)

// keyNameRe restricts key names to alphanumeric + dash + underscore + dot,
// 1–128 chars. Prevents shell-argument injection on macOS and DBus attribute
// confusion on Linux.
var keyNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// backend is the internal interface that platform files satisfy.
// Tests may inject a fake via setBackend.
type backend interface {
	set(key, value string) error
	get(key string) (string, error)
	delete(key string) error
}

// platformBackend wraps the platform-specific free functions into the backend
// interface, keeping platform files as simple free functions (no structs needed).
type platformBackend struct{}

func (platformBackend) set(key, value string) error        { return setPlatform(key, value) }
func (platformBackend) get(key string) (string, error)     { return getPlatform(key) }
func (platformBackend) delete(key string) error            { return deletePlatform(key) }

// activeBackend is the live dispatch target. Tests swap it via setBackend.
var activeBackend backend = platformBackend{}

// setBackend replaces the active backend. Pass nil to restore the platform default.
// Package-internal only (unexported) — not part of the public API.
func setBackend(b backend) {
	if b == nil {
		activeBackend = platformBackend{}
		return
	}
	activeBackend = b
}

func validateKey(key string) error {
	if !keyNameRe.MatchString(key) {
		return ErrInvalidKey
	}
	return nil
}

// Set writes value under the pan-agent service namespace with the given key.
// Overwrites any existing value. key must match keyNameRe.
func Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	return activeBackend.set(key, value)
}

// Get reads the value previously stored under key.
// Returns ErrNotFound if the key does not exist.
func Get(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	return activeBackend.get(key)
}

// Delete removes the key from the platform keyring.
// Returns ErrNotFound if the key did not exist.
func Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	return activeBackend.delete(key)
}

// IsNotFound reports whether err (or any wrapped cause) is ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
