//go:build linux

// Library choice: github.com/zalando/go-keyring v0.2.8 (MIT, published 2026-03-23).
//
// Evaluation against four criteria:
//   (a) MIT licensed — pass.
//   (b) Pure Go, no CGo — pass. Uses godbus/dbus/v5 (BSD-2-Clause, pure Go) on
//       Linux and danieljoos/wincred on Windows; no C bindings anywhere.
//   (c) Commit activity 2026-02 or later — pass. v0.2.8 published 2026-03-23.
//   (d) Error surface (ServiceUnknown / IsLocked / NoReply) — partial pass.
//       The library does not expose these dbus errors by name, but it maps
//       them to its own sentinel (keyring.ErrNotFound or a wrapped dbus error)
//       which we then map to our own sentinels below.
//
// Direct godbus wire-protocol implementation was rejected: ~200 lines of
// DH-IETF session negotiation code that we would own and test ourselves.
// ppacher/go-dbus-keyring was rejected: last commit 2020-02-28 (criterion c).

package secret

import (
	"errors"
	"fmt"
	"strings"

	zk "github.com/zalando/go-keyring"
)

func setPlatform(key, value string) error {
	err := zk.Set(serviceName, key, value)
	return mapLinuxError(err)
}

func getPlatform(key string) (string, error) {
	val, err := zk.Get(serviceName, key)
	if err != nil {
		return "", mapLinuxError(err)
	}
	return val, nil
}

func deletePlatform(key string) error {
	err := zk.Delete(serviceName, key)
	return mapLinuxError(err)
}

func mapLinuxError(err error) error {
	if err == nil {
		return nil
	}
	// zalando/go-keyring maps "item not found" to this sentinel.
	if errors.Is(err, zk.ErrNotFound) {
		return ErrNotFound
	}
	msg := err.Error()
	// D-Bus ServiceUnknown means the Secret Service daemon is not running.
	if strings.Contains(msg, "org.freedesktop.DBus.Error.ServiceUnknown") ||
		strings.Contains(msg, "org.freedesktop.DBus.Error.NoReply") ||
		strings.Contains(msg, "org.freedesktop.Secret.Error.IsLocked") {
		return ErrKeyringUnavailable
	}
	// Any other dbus error wraps with context.
	return fmt.Errorf("secret: secret-service: %w", err)
}
