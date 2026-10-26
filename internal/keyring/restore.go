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

// RestoreOptions say how a key file is put back.
type RestoreOptions struct {
	// ConfirmReRecord is asked when the person once told MailArchive to forget this
	// archive (§4.2). Nil refuses.
	ConfirmReRecord func(id string) (bool, error)
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
	return prepareRestore(out, newPassword, o, func(r recorded) vault.RestoreSource {
		return vault.RecoveryEntrySource{Entry: e, RecoveryKey: rk.text}
	})
}

// RestoreFromRemembered prepares rebuilding out's key files from the key this
// computer remembers (§9.5, K15): the recorded id's, or the id the folder's damaged
// key files or its record entry claim.
func RestoreFromRemembered(out string, s vault.Store, newPassword string, o RestoreOptions) (*PendingRestore, error) {
	if s == nil {
		return nil, errors.New("no secret store to find a remembered key in")
	}
	return prepareRestore(out, newPassword, o, func(r recorded) vault.RestoreSource {
		claimed := r.want
		if claimed == (vault.ID{}) {
			if ids := vault.Claims(out).IDs; len(ids) > 0 {
				claimed = ids[0]
			} else if r.ok {
				claimed, _ = parseID(r.match.ID)
			}
		}
		return vault.RememberedSource{Store: s, Claimed: claimed}
	})
}

// prepareRestore is a restore as a write: the record must be usable, a recorded
// folder that is now a link or holds other files is refused in its case's words,
// and only a recorded, proven path lends its id (the vault's no-record exception;
// an unproven path never does).
func prepareRestore(out, newPassword string, o RestoreOptions, src func(recorded) vault.RestoreSource) (*PendingRestore, error) {
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
	deriving.Lock()
	p, err := vault.PrepareRestore(out, r.want, src(r), newPassword)
	deriving.Unlock()
	if err != nil {
		return nil, err
	}
	return &PendingRestore{out: out, p: p, r: r, o: o}, nil
}

// Commit writes the key file and records the folder under the archive (a restore
// is a write, §3.3). vault.ErrPartiallySaved means the restore IS in effect; the
// folder is recorded then too.
func (p *PendingRestore) Commit() error {
	id := p.p.ArchiveID().String()
	if e := p.r.rec.Archives[id]; e != nil && e.Forgotten {
		ok := false
		if p.o.ConfirmReRecord != nil {
			var err error
			if ok, err = p.o.ConfirmReRecord(id); err != nil {
				p.Discard()
				return err
			}
		}
		if !ok {
			p.Discard()
			return fmt.Errorf("%w (archive %s); its key file was not put back", ErrReRecordRefused, id)
		}
	}
	err := p.p.Commit()
	if err != nil && !errors.Is(err, vault.ErrPartiallySaved) {
		return err
	}
	_, rerr := update("record "+id, func(rec *encrecord.Record) error {
		if m, ok := rec.Lookup(p.out); ok && m.Path.Unproven && m.ID != id {
			rec.ForgetPath(m.ID, m.Path.Spelling)
		}
		return rec.Add(id, p.out, false, now())
	})
	if rerr != nil {
		return fmt.Errorf("the key file was put back in %s, but this computer's record of encrypted archives could not be updated (%w); open the archive with its new password to record it", p.out, rerr)
	}
	encrecord.ClearLost(id)
	return err
}
