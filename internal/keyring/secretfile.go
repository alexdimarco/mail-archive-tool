package keyring

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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

// RecoveryKeyFile reads the recovery key from path (-recovery-key-file): the one
// line that is not blank and not a "#" comment, so the file -recovery-key-out
// writes reads back (the P3-1a pass, SEC-02). Text that is not a recovery key is
// refused without quoting any of it (SEC-07).
func RecoveryKeyFile(path string) (Secret, error) {
	text, err := readSecretFile(path, "recovery-key file")
	if err != nil {
		return Secret{}, err
	}
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		return Secret{}, fmt.Errorf("%w: recovery-key file %s must hold one recovery key on a line of its own (it holds %d lines of text)", ErrSecretFile, path, len(lines))
	}
	if _, err := vault.ParseRecoveryKey(lines[0]); err != nil {
		return Secret{}, fmt.Errorf("%w: recovery-key file %s: %w", ErrSecretFile, path, recoveryKeyRefusal(err))
	}
	return Secret{how: ByRecoveryKey, text: lines[0]}, nil
}

// RecoveryKeyFileText is the text -recovery-key-out saves: comments naming the
// archive the way its owner knows it — its folder and the day the key was made —
// beside its id, then the key, which RecoveryKeyFile reads back (P3-8 friction
// review, B24). A control character in the folder's name is shown as a space, so
// the comment stays one line.
func RecoveryKeyFileText(id vault.ID, key, folder string, made time.Time) string {
	folder = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, folder)
	return fmt.Sprintf("# MailArchive recovery key for the encrypted archive in %s\n# made %s; archive id %s\n%s\n", folder, made.Format("2006-01-02"), id, key)
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
	// A UTF-16 file (Windows PowerShell 5's default) or any NUL byte would read as
	// another secret and be told as a wrong password; a UTF-8 byte-order mark
	// (Notepad's "UTF-8 with BOM") is dropped (the P3-1a pass, CRIT-09).
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF}) || bytes.IndexByte(data, 0) >= 0 {
		return "", refuse("is not plain text (it looks like UTF-16) — save it as plain UTF-8 text")
	}
	text := string(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
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
