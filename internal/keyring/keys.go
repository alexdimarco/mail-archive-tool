package keyring

import (
	"errors"
	"fmt"

	"mail-archive-tool/internal/vault"
)

// errReadOnly refuses a key change on an archive opened to read.
var errReadOnly = errors.New("this archive was opened to read; open it for a change to its keys")

// ChangePassword sets a new password for u's archive (§9.1), authorised by u — the
// password, the recovery key or the remembered key it was opened with (K13). Only
// the key file changes; the remembered key and every sealed byte stay. A
// vault.ErrPartiallySaved means the new password IS in effect; the second copy is
// rewritten at the next opening.
func ChangePassword(u *Unlocked, newPassword string) error {
	if !u.write {
		return errReadOnly
	}
	k, _, err := u.acquire()
	if err != nil {
		return err
	}
	defer u.release()
	deriving.Lock()
	defer deriving.Unlock()
	return vault.ChangePasswordAt(u.out, u.id, vault.KeySecret(k), newPassword)
}

// PendingRecovery is a new recovery key made but not written (§9.2): shown, hidden
// and entered again (K2) before Commit; the old one works until then. It prints
// redacted.
type PendingRecovery struct {
	p *vault.PendingRecovery
}

// Format prints a pending recovery key without its keys.
func (p *PendingRecovery) Format(f fmt.State, _ rune) {
	fmt.Fprint(f, "keyring.PendingRecovery{redacted}")
}

// RecoveryKey is the new recovery key to show once.
func (p *PendingRecovery) RecoveryKey() string { return p.p.RecoveryKey().String() }

// Commit replaces the recovery key. vault.ErrPartiallySaved means the new key IS
// in effect; vault.ErrConflict means the key file changed meanwhile and a new
// recovery key must be made.
func (p *PendingRecovery) Commit() error { return p.p.Commit() }

// Discard forgets the new recovery key; the old one stays in force.
func (p *PendingRecovery) Discard() { p.p.Discard() }

// NewRecoveryKey prepares a new recovery key for u's archive (§9.2), authorised by
// u (K13).
func NewRecoveryKey(u *Unlocked) (*PendingRecovery, error) {
	if !u.write {
		return nil, errReadOnly
	}
	k, _, err := u.acquire()
	if err != nil {
		return nil, err
	}
	defer u.release()
	deriving.Lock()
	defer deriving.Unlock()
	p, err := vault.PrepareNewRecovery(u.out, u.id, vault.KeySecret(k))
	if err != nil {
		return nil, err
	}
	return &PendingRecovery{p: p}, nil
}

// Remember files u's key in s (O3, §9.3) and records the folder, as an open with
// Remember does.
func Remember(u *Unlocked, s vault.Store) error {
	if !u.write {
		return errReadOnly
	}
	return remember(u, s)
}

// CheckRecoveryKey reports whether typed is the recovery key shown, by its parsed
// value: spacing, letter case and the dashes between groups do not matter, and a
// typing mistake is told apart from another key (K2).
func CheckRecoveryKey(shown, typed string) error {
	want, err := vault.ParseRecoveryKey(shown)
	if err != nil {
		return err
	}
	got, err := vault.ParseRecoveryKey(typed)
	if err != nil {
		return err
	}
	if got != want {
		return errors.New("that is not the recovery key shown")
	}
	return nil
}
