package schedule

import (
	"errors"
	"fmt"
	"path/filepath"

	"mail-archive-tool/internal/encmark"
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
		res, err := encmark.At(s.Out)
		if res.ByRecordErr {
			return fmt.Errorf("%w: %v — nothing was installed; `mailarchive status` names the fix", encmark.ErrRecordUnusable, res.RecordErr)
		}
		dir, why := "", ""
		if res.Encrypted {
			dir, why = res.Folder, res.Why
		}
		if dir != "" {
			return fmt.Errorf("%w: %s is an encrypted archive (%s) — scheduled backups of an encrypted archive are not available yet; nothing was installed", ErrEncryptedArchive, dir, why)
		}
		if err != nil {
			return fmt.Errorf("%w: %v; nothing was installed", ErrEncryptedArchive, err)
		}
	}
	if s.Log != "" {
		if dir, why, _ := encmark.AtOrAbove(filepath.Dir(s.Log)); dir != "" {
			return fmt.Errorf("%w: the schedule's log %s would be inside the encrypted archive %s (%s) — choose a log folder outside it; nothing was installed", ErrEncryptedArchive, s.Log, dir, why)
		}
	}
	return nil
}
