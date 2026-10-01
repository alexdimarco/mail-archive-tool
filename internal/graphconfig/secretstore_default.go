//go:build !windows

package graphconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultSecretStore on non-Windows is a 0600 file store under the OS config dir
// (the same posture as -client-secret-file). On Windows it is the Credential
// Manager (secretstore_windows.go).
func DefaultSecretStore() (SecretStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine the OS config directory for the secret store: %w", err)
	}
	return NewFileSecretStore(filepath.Join(dir, "mailarchive", "secrets")), nil
}
