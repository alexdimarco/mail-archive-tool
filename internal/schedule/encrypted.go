package schedule

import (
	"errors"
	"fmt"
	"path/filepath"

	"mail-archive-tool/internal/vault"
)

// ErrEncryptedArchive refuses a schedule for an encrypted archive, or one whose log
// would be written inside one (design-encrypted-archives-phase2a §5).
var ErrEncryptedArchive = errors.New("an encrypted archive cannot be scheduled yet")

// refuseEncrypted refuses, before the scheduler gets an entry or the archive a
// descriptor, an install whose archive is — or sits inside — an encrypted archive,
// or whose log (and so the scheduler's error-output file beside it) would be inside
// one. Scheduled backups of an encrypted archive arrive with their unattended key
// (Phase 4); until then an install would write a full descriptor — source paths,
// mailbox addresses, credential paths — and every run's log and error output into
// the archive in plaintext. Every install surface (CLI, wizard, Desktop, repair)
// goes through Install, so this one check covers them all.
func refuseEncrypted(s Spec) error {
	if s.Out != "" {
		if dir, why := encryptedAtOrAbove(s.Out); dir != "" {
			return fmt.Errorf("%w: %s is an encrypted archive (%s) — scheduled backups of an encrypted archive are not available yet; nothing was installed", ErrEncryptedArchive, dir, why)
		}
	}
	if s.Log != "" {
		if dir, why := encryptedAtOrAbove(filepath.Dir(s.Log)); dir != "" {
			return fmt.Errorf("%w: the schedule's log %s would be inside the encrypted archive %s (%s) — choose a log folder outside it; nothing was installed", ErrEncryptedArchive, s.Log, dir, why)
		}
	}
	return nil
}

// encryptedAtOrAbove is the first of dir and its parents that is an encrypted
// archive (vault.EncryptedMarker), and why; "" when none is. A folder it cannot read
// is not called encrypted here.
func encryptedAtOrAbove(dir string) (string, string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	for d := abs; ; d = filepath.Dir(d) {
		if enc, why, _ := vault.EncryptedMarker(d); enc {
			return d, why
		}
		if filepath.Dir(d) == d {
			return "", ""
		}
	}
}
