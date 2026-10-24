package vault

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// Store is the OS secret store as the vault needs it; graphconfig.SecretStore
// (Windows Credential Manager, or a 0600 file on macOS/Linux) satisfies it.
type Store interface {
	Set(account, secret string) error
	Get(account string) (string, error)
	Delete(account string) error
}

// Account is where an archive's remembered key is filed (C7): its stable
// archive id, which travels with the archive, never its folder path.
func Account(id ID) string { return "archive:" + id.String() }

// Remember files k's master key in s so unattended backups can open the archive
// (D1's default, A10). It is the master key itself, protected only by the store
// — the Credential Manager on Windows, a 0600 file on macOS/Linux (R-E2: this
// protects a copy of the archive that leaves the machine, not the machine).
func Remember(s Store, k *Key) error {
	return s.Set(Account(k.id), base64.StdEncoding.EncodeToString(k.master[:]))
}

// Forget removes an archive's remembered key ("ask me each time").
func Forget(s Store, id ID) error { return s.Delete(Account(id)) }

// ErrKeyNotRetrievable refuses an unattended run that cannot get a key which
// opens the archive (C7): none stored, unreadable, or not this archive's.
var ErrKeyNotRetrievable = errors.New("vault: this archive's remembered key cannot be retrieved")

// rememberedUnlock proves a raw master key against one vault copy.
func rememberedUnlock(master []byte) unlockFunc {
	return func(v *Vault) (*Key, error) {
		k, err := newKey(v.ArchiveID, master)
		if err != nil {
			return nil, err
		}
		if err := v.Verify(k); err != nil {
			k.Destroy()
			return nil, err
		}
		return k, nil
	}
}

// RequireRetrievableKey is the unattended run's gate (C7; the twin of
// app.RequireExistingOut, MA-689). want is the archive id THIS MACHINE recorded
// when the archive was encrypted (A15) — never one read from the folder, which
// an attacker who can write the folder controls. It returns the key only when
// the key remembered under want authenticates a vault in out that names want,
// and otherwise refuses — naming the archive and the fix — before the caller
// writes anything: a vault deleted, damaged, swapped for another archive's, or
// from a newer MailArchive is a refusal, never "not encrypted". It writes
// nothing itself (no heal: healing needs the archive lock).
func RequireRetrievableKey(out string, want ID, s Store) (*Key, error) {
	refuse := func(why string, cause error) error {
		return fmt.Errorf("%w: archive %s (id %s): %s — open the archive once in MailArchive with its password or recovery key to remember its key again (%w)",
			ErrKeyNotRetrievable, out, want, why, cause)
	}
	if want == (ID{}) {
		return nil, fmt.Errorf("%w: archive %s: no archive id was recorded for it on this machine", ErrKeyNotRetrievable, out)
	}
	info, ok, err := Detect(out)
	switch {
	case err != nil:
		return nil, refuse("its key file is unusable", err)
	case !ok:
		return nil, refuse("its key file is missing, so it will not be written to — it was encrypted, and writing without the key would store mail unencrypted", ErrNoVault)
	case info.ArchiveID != want:
		return nil, refuse("its key file belongs to archive "+info.ArchiveID.String()+", not this one", ErrTampered)
	}
	stored, err := s.Get(Account(want))
	if err != nil {
		return nil, refuse("no key is stored for it", err)
	}
	master, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || len(master) != keySize {
		clear(master)
		return nil, refuse("the stored key is damaged", ErrCorrupt)
	}
	defer clear(master)
	o, err := openVault(out, rememberedUnlock(master))
	if err != nil {
		return nil, refuse("the stored key does not open it", err)
	}
	return o.Key, nil
}
