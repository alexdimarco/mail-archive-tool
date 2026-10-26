package keyring

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// maxSecretFile bounds a password or recovery-key file (§3.4).
const maxSecretFile = 4 << 10

// ErrSecretFile refuses a password or recovery-key file that is not this account's
// alone, not a plain file, empty, or too large.
var ErrSecretFile = errors.New("the secret file cannot be used")

// Typed is what a person typed at an unlock prompt: a recovery key when it reads as
// one, otherwise a password.
func Typed(text string) Secret {
	if _, err := vault.ParseRecoveryKey(text); err == nil {
		return RecoveryKey(text)
	}
	return Password(text)
}

// PasswordFile reads the password from path (-password-file).
func PasswordFile(path string) (Secret, error) {
	text, err := readSecretFile(path, "password file")
	return Secret{how: ByPassword, text: text}, err
}

// RecoveryKeyFile reads the recovery key from path (-recovery-key-file).
func RecoveryKeyFile(path string) (Secret, error) {
	text, err := readSecretFile(path, "recovery-key file")
	return Secret{how: ByRecoveryKey, text: text}, err
}

// readSecretFile reads a secret file with one descriptor-judged open (§3.4): a
// regular file, not a link; owned by this account and readable by no other (the
// mode on Unix, the owner and access list on Windows, judged on the open file);
// at most 4 KiB; one trailing line ending stripped.
func readSecretFile(path, kind string) (string, error) {
	refuse := func(why string) error { return fmt.Errorf("%w: %s %s %s", ErrSecretFile, kind, path, why) }
	fi, err := os.Lstat(path)
	if err != nil {
		return "", refuse(fmt.Sprintf("cannot be read: %v", err))
	}
	if !fi.Mode().IsRegular() {
		return "", refuse("must be a plain file (not a link, folder, pipe or device)")
	}
	f, err := util.OpenRegularFile(path)
	if err != nil {
		return "", refuse(fmt.Sprintf("cannot be read: %v", err))
	}
	defer f.Close()
	if why := ownersOnly(f, path); why != "" {
		return "", refuse(why)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSecretFile+1))
	if err != nil {
		return "", refuse(fmt.Sprintf("cannot be read: %v", err))
	}
	defer clear(data)
	if len(data) > maxSecretFile {
		return "", refuse(fmt.Sprintf("is larger than %d bytes — is this the right file?", maxSecretFile))
	}
	text := string(data)
	if strings.HasSuffix(text, "\r\n") {
		text = text[:len(text)-2]
	} else if strings.HasSuffix(text, "\n") {
		text = text[:len(text)-1]
	}
	if text == "" {
		return "", refuse("is empty")
	}
	return text, nil
}
