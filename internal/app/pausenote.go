package app

import (
	"context"
	"log"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
)

// SealPauseNote seals note into the encrypted archive's record as its pause note
// (design-encrypted-archives-phase3 §6, E6), or clears it when note is "". It is a
// keyed write under the archive lock, committed by the record's save; the pause
// file itself is written by the pause, after this, as the empty marker. A paused
// archive is not written: a pause seals the note first, a resume clears it after
// the archive is writable again.
func SealPauseNote(ctx context.Context, opts Options, note string, logger *log.Logger) error {
	if opts.Key == nil {
		return ErrEncryptedNeedsKey
	}
	out := opts.Out
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := seal.RefuseWrite(out); err != nil {
		return err
	}
	if err := preflightEncrypted(opts); err != nil {
		return err
	}
	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), lockVerb(opts, "pause"))
	if err != nil {
		return withKind(state.FailArchiveUnwritable, err)
	}
	defer lock.Release()
	opts.settingOnly = true
	manifest, _, store, err := openEncrypted(opts, lock.StillHeld, time.Now().UTC(), logger, nil)
	if err != nil {
		return err
	}
	store.SetPauseNote(note)
	if err := manifest.Save(); err != nil {
		return manifestSaveFailure(out, err)
	}
	return nil
}
