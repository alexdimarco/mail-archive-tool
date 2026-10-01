package graphconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/util"
)

const secretMaxBytes = 8 << 10 // a client secret is small; bound the read

// fileStore keeps one secret per account as a 0600 regular file under dir. It is
// the non-Windows SecretStore and is also used in tests on any OS. The secret is
// PLAINTEXT at rest (0600), the same posture as the existing -client-secret-file —
// not encrypted; on Windows the Credential Manager store is used instead.
type fileStore struct{ dir string }

// NewFileSecretStore returns a file-backed SecretStore writing under dir.
func NewFileSecretStore(dir string) SecretStore { return fileStore{dir: dir} }

func (s fileStore) Name() string { return "file (" + s.dir + ")" }

// path maps an account key to a safe filename (the key is already non-sensitive:
// graph:<hash8>-<hash8>); any stray separator/control is replaced defensively.
func (s fileStore) path(account string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, account)
	return filepath.Join(s.dir, safe+".secret")
}

func (s fileStore) Set(account, secret string) error {
	if strings.TrimSpace(secret) == "" {
		return errors.New("refusing to store an empty secret")
	}
	return util.WriteFileAtomic0600(s.path(account), []byte(secret))
}

func (s fileStore) Get(account string) (string, error) {
	data, err := util.ReadSecureFile(s.path(account), "secret store file", secretMaxBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("no stored secret for %s: %w", account, os.ErrNotExist)
		}
		return "", err
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", fmt.Errorf("stored secret for %s is empty: %w", account, os.ErrNotExist)
	}
	return secret, nil
}

func (s fileStore) Delete(account string) error {
	if err := os.Remove(s.path(account)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
