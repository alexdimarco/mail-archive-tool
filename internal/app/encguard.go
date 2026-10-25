package app

import (
	"errors"
	"fmt"
	"path/filepath"

	"mail-archive-tool/internal/vault"
)

// ErrEncryptedNeedsKey refuses an operation that would treat an encrypted archive
// as plaintext — read it as files on disk, prune it, re-export into it — when the
// operation was not handed the archive's key (design-encrypted-archives-phase2a
// §5, A15).
var ErrEncryptedNeedsKey = errors.New("this archive is encrypted")

// manifestDir is the folder of a -manifest override, or "" when there is none.
func manifestDir(manifest string) string {
	if manifest == "" {
		return ""
	}
	return filepath.Dir(manifest)
}

// ErrLogInEncryptedArchive refuses a run log placed inside an encrypted archive: a
// run log names stores, folders and source paths in plaintext (phase2a §4).
var ErrLogInEncryptedArchive = errors.New("a run log cannot be written inside an encrypted archive")

// RefuseLogInside refuses a -log path whose folder is, or sits inside, an encrypted
// archive — any encrypted archive, not only the run's own — before the log is
// opened. An empty path (no -log) is nothing to refuse.
func RefuseLogInside(logPath string) error {
	if logPath == "" {
		return nil
	}
	abs, err := filepath.Abs(logPath)
	if err != nil {
		return nil
	}
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		if enc, why, _ := vault.EncryptedMarker(dir); enc {
			return fmt.Errorf("%w: %s would be inside the encrypted archive %s (%s) — leave out -log, or choose a folder outside the archive",
				ErrLogInEncryptedArchive, abs, dir, why)
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

// RefuseKeyless refuses op on out — before any lock, log, read or write — when
// out is an encrypted archive, or sits inside one, or any of extra (a -manifest
// folder, an extract destination) is or sits inside one, as vault.EncryptedMarker
// recognises it. An I/O error reading a folder is left to the operation's own
// handling. op names the operation in the refusal ("archive into it").
func RefuseKeyless(op, out string, extra ...string) error {
	for _, target := range append([]string{out}, extra...) {
		if target == "" {
			continue
		}
		abs, err := filepath.Abs(target)
		if err != nil {
			continue
		}
		for dir := abs; ; dir = filepath.Dir(dir) {
			if enc, why, _ := vault.EncryptedMarker(dir); enc {
				where := dir
				if dir != abs {
					where = dir + " (which holds " + abs + ")"
				}
				return fmt.Errorf("%w: %s is an encrypted archive — %s; refusing to %s without its key: open it in MailArchive with its password or recovery key",
					ErrEncryptedNeedsKey, where, why, op)
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
	}
	return nil
}
