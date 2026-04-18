//go:build windows

package secret

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/danieljoos/wincred"
)

// wincredMaxBlob is CRED_MAX_CREDENTIAL_BLOB_SIZE per the Windows SDK.
const wincredMaxBlob = 2560

func setPlatform(key, value string) error {
	if len(value) > wincredMaxBlob {
		return fmt.Errorf("secret: value exceeds 2560-byte wincred blob limit")
	}
	target := serviceName + ":" + key
	cred := wincred.NewGenericCredential(target)
	// PersistLocalMachine: user-scoped via DPAPI (tied to user SID), survives
	// reboot. PersistSession would be lost at logoff; PersistEnterprise permits
	// domain roaming which is undesirable for a per-user desktop agent.
	cred.Persist = wincred.PersistLocalMachine
	cred.CredentialBlob = []byte(value)
	if err := cred.Write(); err != nil {
		return mapWindowsError(err)
	}
	return nil
}

func getPlatform(key string) (string, error) {
	target := serviceName + ":" + key
	cred, err := wincred.GetGenericCredential(target)
	if err != nil {
		return "", mapWindowsError(err)
	}
	return string(cred.CredentialBlob), nil
}

func deletePlatform(key string) error {
	target := serviceName + ":" + key
	cred, err := wincred.GetGenericCredential(target)
	if err != nil {
		return mapWindowsError(err)
	}
	if err := cred.Delete(); err != nil {
		return mapWindowsError(err)
	}
	return nil
}

func mapWindowsError(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case 1168: // ERROR_NOT_FOUND
			return ErrNotFound
		case 1722: // RPC_S_SERVER_UNAVAILABLE
			return ErrKeyringUnavailable
		}
	}
	return fmt.Errorf("secret: windows keyring: %w", err)
}
