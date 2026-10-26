package keyring

import (
	"errors"
	"fmt"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// RecoveryFile is the new archive's recovery file, to offer with its recovery key
// (§5.1 step 5, §9.4): the recovery entry only, which opens nothing without the
// recovery key.
func (p *PendingCreate) RecoveryFile() ([]byte, error) { return p.p.RecoveryFile() }

// RecoveryFile is the recovery file for the new recovery key (§9.2).
func (p *PendingRecovery) RecoveryFile() ([]byte, error) { return p.p.RecoveryFile() }

// ErrRestoredNotRecorded reports a key file that WAS put back — the new password
// and recovery key are the ones that count now — when this computer's record could
// not be updated (the P3-1b pass, CON-02, FID-02): keep the new recovery key and
// recovery file, and open the archive with the new password to record it.
var ErrRestoredNotRecorded = errors.New("the key file was put back — the new password and recovery key are the ones that count now — but this computer's record of encrypted archives could not be updated")

// RestoreOptions say how a key file is put back.
type RestoreOptions struct {
	// ConfirmReRecord is asked when the person once told MailArchive to forget this
	// archive (§4.2). Nil refuses.
	ConfirmReRecord func(id string) (bool, error)
	// ConfirmCost is asked before deriving a recovery file's entry above
	// MailArchive's own cost at a folder this computer has not recorded (K14; the
	// P3-1b pass, CRY-05). Nil refuses.
	ConfirmCost func(vault.Params) (bool, error)
}

// commitRestore is the vault's Commit, a seam so a test can change the record while
// the key file is written.
var commitRestore = (*vault.PendingRestore).Commit

// InEffect reports whether err from a restore's Commit still means the restore
// took effect: the key file was written, and only what followed failed.
func InEffect(err error) bool {
	return err == nil || errors.Is(err, vault.ErrPartiallySaved) || errors.Is(err, vault.ErrUnconfirmed) ||
		errors.Is(err, ErrRestoredNotRecorded)
}

// PendingRestore is a key file to put back, prepared but not written (§9.4, §9.5):
// a new password and a new recovery key, shown and entered again (K2) before
// Commit. It prints redacted.
type PendingRestore struct {
	out string
	p   *vault.PendingRestore
	r   recorded
	o   RestoreOptions
}

// Format prints a pending restore without its keys.
func (p *PendingRestore) Format(f fmt.State, _ rune) {
	fmt.Fprint(f, "keyring.PendingRestore{redacted}")
}

// RecoveryKey is the new recovery key to show once.
func (p *PendingRestore) RecoveryKey() string { return p.p.RecoveryKey().String() }

// RecoveryFile is the new recovery file, offered with the new recovery key.
func (p *PendingRestore) RecoveryFile() ([]byte, error) { return p.p.RecoveryFile() }

// ArchiveID is the archive being restored.
func (p *PendingRestore) ArchiveID() vault.ID { return p.p.ArchiveID() }

// Discard forgets the prepared key file.
func (p *PendingRestore) Discard() { p.p.Discard() }

// RestoreFromFile prepares putting back out's key files from a recovery file and
// its recovery key (§9.4), with newPassword. rk must be a recovery key
// (RecoveryKey, or RecoveryKeyFile).
func RestoreFromFile(out string, file []byte, rk Secret, newPassword string, o RestoreOptions) (*PendingRestore, error) {
	if !rk.IsRecoveryKey() {
		return nil, errors.New("a recovery file is opened with its recovery key, not a password")
	}
	e, err := vault.ParseRecoveryFile(file)
	if err != nil {
		return nil, err
	}
	return prepareRestore(out, newPassword, o, func(r recorded, limit *vault.Params) vault.RestoreSource {
		return vault.RecoveryEntrySource{Entry: e, RecoveryKey: rk.text, Limit: limit}
	})
}

// RestoreFromRemembered prepares rebuilding out's key files from the key this
// computer remembers (§9.5, K15): the recorded id's, or the id the folder's damaged
// key files or its record entry claim.
func RestoreFromRemembered(out string, s vault.Store, newPassword string, o RestoreOptions) (*PendingRestore, error) {
	if s == nil {
		return nil, errors.New("no secret store to find a remembered key in")
	}
	return prepareRestore(out, newPassword, o, func(r recorded, _ *vault.Params) vault.RestoreSource {
		claimed := r.want
		if claimed == (vault.ID{}) {
			if ids := vault.Claims(out).IDs; len(ids) > 0 {
				claimed = ids[0]
			} else if r.ok {
				claimed, _ = parseID(r.match.ID)
			}
		}
		return vault.RememberedSource{Store: storeOutsideDerivation{s}, Claimed: claimed}
	})
}

// storeOutsideDerivation lets go of the derivation lock while the secret store is
// read, which may wait on the OS asking the person: no other open in this process
// waits behind that question (the P3-1b pass, CON-04). It is read only inside
// prepareRestore, which holds the lock.
type storeOutsideDerivation struct{ vault.Store }

func (s storeOutsideDerivation) Get(account string) (string, error) {
	deriving.Unlock()
	defer deriving.Lock()
	return s.Store.Get(account)
}

// prepareRestore is a restore as a write: the record must be usable, a recorded
// folder that is now a link or holds other files is refused in its case's words,
// and only a recorded, proven path lends its id (the vault's no-record exception;
// an unproven path never does). At a folder this computer has not recorded, a
// recovery file asking more than MailArchive's own cost is asked about first (K14).
func prepareRestore(out, newPassword string, o RestoreOptions, src func(recorded, *vault.Params) vault.RestoreSource) (*PendingRestore, error) {
	r, err := lookup(out)
	if err != nil {
		return nil, err
	}
	if r.recErr != nil {
		return nil, refuseUnusableRecord(out, r.recErr)
	}
	if r.ok {
		if err := refuseCase(out); err != nil {
			return nil, err
		}
	}
	if err := CheckNewPassword(newPassword); err != nil {
		return nil, err
	}
	var p *vault.PendingRestore
	err = askingCost(out, r.want, o.ConfirmCost, func(limit *vault.Params) error {
		deriving.Lock()
		defer deriving.Unlock()
		var err error
		p, err = vault.PrepareRestore(out, r.want, src(r, limit), newPassword)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &PendingRestore{out: out, p: p, r: r, o: o}, nil
}

// Commit writes the key file and records the folder under the archive (a restore
// is a write, §3.3). The record and the folder's case are judged again first, as
// they are now: the archive may have been forgotten, or the folder replaced, since
// the restore was prepared (the P3-1b pass, CON-01, CRIT-05). Once the key file is
// written the restore IS in effect, and every error says so (InEffect):
// vault.ErrPartiallySaved, vault.ErrUnconfirmed, or ErrRestoredNotRecorded.
func (p *PendingRestore) Commit() error {
	id := p.p.ArchiveID().String()
	rec, err := encrecord.Load()
	if err != nil {
		p.Discard()
		return refuseUnusableRecord(p.out, err)
	}
	if p.r.ok {
		if err := refuseCase(p.out); err != nil {
			p.Discard()
			return err
		}
	}
	confirmed := false
	if e := rec.Archives[id]; e != nil && e.Forgotten {
		if p.o.ConfirmReRecord != nil {
			if confirmed, err = p.o.ConfirmReRecord(id); err != nil {
				p.Discard()
				return err
			}
		}
		if !confirmed {
			p.Discard()
			return fmt.Errorf("%w (archive %s); its key file was not put back", ErrReRecordRefused, id)
		}
	}
	err = commitRestore(p.p)
	if !InEffect(err) {
		return err
	}
	_, rerr := update("record "+id, func(rec *encrecord.Record) error {
		// Forgotten since the question was asked, or without it being asked: the
		// person never said to record it again (as adopt; the P3-1a pass, CON-04).
		if e := rec.Archives[id]; e != nil && e.Forgotten && !confirmed {
			return fmt.Errorf("%w (archive %s)", ErrReRecordRefused, id)
		}
		if m, ok := rec.Lookup(p.out); ok && m.Path.Unproven && m.ID != id {
			rec.ForgetPath(m.ID, m.Path.Spelling)
		}
		return rec.Add(id, p.out, false, now())
	})
	if rerr != nil {
		if err != nil {
			return fmt.Errorf("%w (%w) in %s; open the archive with its new password to record it — and: %w", ErrRestoredNotRecorded, rerr, p.out, err)
		}
		return fmt.Errorf("%w (%w) in %s; open the archive with its new password to record it", ErrRestoredNotRecorded, rerr, p.out)
	}
	encrecord.ClearLost(id)
	return err
}
