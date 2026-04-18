//go:build darwin

package secret

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// largeValueThreshold is the size above which we pass the secret via stdin
// (security add-generic-password -w -) to keep the plaintext out of the
// process argument list visible to ps / /proc/<pid>/cmdline equivalents.
const largeValueThreshold = 16 * 1024

func setPlatform(key, value string) error {
	var cmd *exec.Cmd
	if len(value) > largeValueThreshold {
		cmd = exec.Command("/usr/bin/security", "add-generic-password",
			"-U", "-a", key, "-s", serviceName, "-w", "-")
		cmd.Stdin = strings.NewReader(value)
	} else {
		cmd = exec.Command("/usr/bin/security", "add-generic-password",
			"-U", "-a", key, "-s", serviceName, "-w", value)
	}
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return mapDarwinError(err, stderr.String())
	}
	return nil
}

func getPlatform(key string) (string, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password",
		"-a", key, "-s", serviceName, "-w")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", mapDarwinError(err, stderr.String())
	}
	// -w prints the password followed by a newline.
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func deletePlatform(key string) error {
	cmd := exec.Command("/usr/bin/security", "delete-generic-password",
		"-a", key, "-s", serviceName)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return mapDarwinError(err, stderr.String())
	}
	return nil
}

func mapDarwinError(err error, stderr string) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("secret: macOS keychain: %s: %w", stderr, err)
	}
	switch exitErr.ExitCode() {
	case 44: // errSecItemNotFound — "The specified item could not be found in the keychain"
		return ErrNotFound
	case 45: // user denied access
		return ErrKeyringUnavailable
	case 51: // errSecAuthFailed (-25308)
		return ErrKeyringUnavailable
	default:
		return fmt.Errorf("secret: macOS keychain: %s: %w", stderr, err)
	}
}
