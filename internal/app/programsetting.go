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

// SealProgramCopies seals the choice to keep copies of MailArchive in the encrypted
// archive (program design §6, C16): a keyed write under the archive lock, committed
// by the record's save, as the pause note is. Nothing in program/ changes now: the
// next backup's program step writes the copies, or removes them. Refused, with its
// reason, when the archive is paused, its drive is missing, another run holds it, or
// the sealed set was written by a newer MailArchive (R6).
func SealProgramCopies(ctx context.Context, opts Options, choice ProgramChoice, logger *log.Logger) error {
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
	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), lockVerb(opts, "program copies"))
	if err != nil {
		return withKind(state.FailArchiveUnwritable, err)
	}
	defer lock.Release()
	opts.ProgramCopies = ProgramCopiesUnset // set below, so a refusal is told, not logged
	opts.settingOnly = true
	manifest, _, store, err := openEncrypted(opts, lock.StillHeld, time.Now().UTC(), logger, nil)
	if err != nil {
		return err
	}
	if err := setProgramChoice(store, choice); err != nil {
		return err
	}
	if err := manifest.Save(); err != nil {
		return manifestSaveFailure(out, err)
	}
	return nil
}
