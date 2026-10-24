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
// app.RequireExistingOut, MA-689): it returns the archive's key only when the
// remembered key authenticates the archive's vault, and otherwise refuses —
// naming the archive and the fix — before the caller writes anything. It writes
// nothing itself (no heal: healing needs the archive lock).
func RequireRetrievableKey(out string, s Store) (*Key, error) {
	info, ok, err := Detect(out)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoVault, out)
	}
	refuse := func(why string) error {
		return fmt.Errorf("%w: archive %s (id %s): %s — open the archive once in MailArchive with its password or recovery key to remember its key again",
			ErrKeyNotRetrievable, out, info.ArchiveID, why)
	}
	stored, err := s.Get(Account(info.ArchiveID))
	if err != nil {
		return nil, refuse("no key is stored for it (" + err.Error() + ")")
	}
	master, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || len(master) != keySize {
		clear(master)
		return nil, refuse("the stored key is damaged")
	}
	defer clear(master)
	o, err := openVault(out, rememberedUnlock(master))
	if err != nil {
		return nil, refuse("the stored key does not open it (" + err.Error() + ")")
	}
	return o.Key, nil
}
