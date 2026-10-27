package keyring

import (
	"context"
	"errors"
	"log"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// LeaseForWrite is a lease for one write job — Run now, a key change, the keyed
// pause — from an archive opened to read, as the Desktop's unlock is
// (design-encrypted-archives-phase3 §3.2, §3.3): the record must be usable, a
// recorded folder must be intact, and an unrecorded folder is recorded under the
// archive's id (adoption), asking first when the person once said to forget it.
// The heal is the caller's, after the lease and before its run takes the archive
// lock (§3.6). The job destroys the lease when it ends.
func (u *Unlocked) LeaseForWrite(o OpenOptions) (*Unlocked, error) {
	r, err := lookup(u.out)
	if err != nil {
		return nil, err
	}
	if r.recErr != nil {
		return nil, refuseUnusableRecord(u.out, r.recErr)
	}
	if r.ok {
		if err := refuseCase(u.out); err != nil {
			return nil, err
		}
	}
	l, err := u.Lease()
	if err != nil {
		return nil, err
	}
	l.write = true
	if err := adopt(l, r, o.ConfirmReRecord); err != nil {
		l.Destroy()
		return nil, err
	}
	return l, nil
}

// OpenTyped opens out with what a person typed — a password or a recovery key. What
// reads as a recovery key may be a password of that shape set before such
// passwords were refused, so it is tried as one too, through a whole open, its own
// cost question asked (the P3-1a pass, SEC-03, CRIT-10). The command line and the
// Desktop share it.
func OpenTyped(out, typed string, o OpenOptions) (*Unlocked, error) {
	s := Typed(typed)
	u, err := Open(out, s, o)
	if errors.Is(err, vault.ErrWrongSecret) && s.IsRecoveryKey() {
		u, err = Open(out, Password(typed), o)
	}
	return u, err
}

// Forgotten reports whether the person once told this computer to forget the
// archive at out (§4.2, a tombstone): a write or a remember there asks before it
// records the folder again. The Desktop asks that question before it derives a key
// or shows a recovery key. The archive is the one the folder's key files claim; a
// folder with none is not forgotten.
func Forgotten(out string) (bool, error) {
	rec, err := encrecord.Load()
	if err != nil {
		return false, err
	}
	info, ok, err := vault.Detect(out)
	if err != nil || !ok {
		return false, err
	}
	e := rec.Archives[info.ArchiveID.String()]
	return e != nil && e.Forgotten, nil
}

// SetPauseNote seals note into the archive's record as its pause note, or clears
// it when note is "" (§6, E6): a keyed write, so u must be a write lease. The
// pause file beside the archive stays the empty marker.
func SetPauseNote(ctx context.Context, u *Unlocked, note string, logger *log.Logger) error {
	opts := app.Options{Out: u.Out()}
	done, err := Apply(&opts, u)
	if err != nil {
		return err
	}
	defer done()
	return app.SealPauseNote(ctx, opts, note, logger)
}

// SetProgramCopies seals the choice to keep copies of MailArchive in the archive
// (program design §6, C16): a keyed write, so u must be a write lease. The next backup
// writes the copies, or removes them.
func SetProgramCopies(ctx context.Context, u *Unlocked, on bool, logger *log.Logger) error {
	opts := app.Options{Out: u.Out()}
	done, err := Apply(&opts, u)
	if err != nil {
		return err
	}
	defer done()
	choice := app.ProgramCopiesOff
	if on {
		choice = app.ProgramCopiesOn
	}
	return app.SealProgramCopies(ctx, opts, choice, logger)
}

// ProgramCopies reads the archive's sealed program set, read-only and without the
// archive lock (§7.3), for the card: its status, and the set when it is one this
// program reads.
func ProgramCopies(u *Unlocked) (*container.ProgramSet, container.ProgramStatus, error) {
	k, _, err := u.acquire()
	if err != nil {
		return nil, container.ProgramAbsent, err
	}
	defer u.release()
	_, s, err := container.LoadManifest(u.out, container.LoadOptions{Key: k, ReadOnly: true})
	if err != nil {
		return nil, container.ProgramAbsent, err
	}
	ps, st, _ := s.Program()
	return &ps, st, nil
}

// PauseNote reads the pause note sealed in the archive's record, read-only and
// without the archive lock (§7.3).
func PauseNote(u *Unlocked) (string, error) {
	k, _, err := u.acquire()
	if err != nil {
		return "", err
	}
	defer u.release()
	_, s, err := container.LoadManifest(u.out, container.LoadOptions{Key: k, ReadOnly: true})
	if err != nil {
		return "", err
	}
	return s.PauseNote(), nil
}
