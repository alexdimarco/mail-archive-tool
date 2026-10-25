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

// Located is a Store that keeps its secrets in a folder (the 0600 file store on
// macOS/Linux); the Credential Manager has no folder and does not implement it.
type Located interface{ Location() string }

// ErrStoreInsideArchive refuses to remember a key in a store whose folder is inside
// an encrypted archive: the key would travel with the archive it opens (P2a-C14).
var ErrStoreInsideArchive = errors.New("vault: the secret store is inside an encrypted archive")

// Remember files k's master key in s so unattended backups can open the archive
// (D1's default, A10). It is the master key itself, protected only by the store
// — the Credential Manager on Windows, a 0600 file on macOS/Linux (R-E2: this
// protects a copy of the archive that leaves the machine, not the machine). A
// store whose folder is inside ANY encrypted archive is refused before the write.
func Remember(s Store, k *Key) error {
	if err := k.usable(); err != nil {
		return err
	}
	if l, ok := s.(Located); ok {
		if where, why, _ := EncryptedAtOrAbove(l.Location()); where != "" {
			return fmt.Errorf("%w: %s is inside the encrypted archive %s (%s) — keep MailArchive's settings outside the archive", ErrStoreInsideArchive, l.Location(), where, why)
		}
	}
	return s.Set(Account(k.id), base64.StdEncoding.EncodeToString(k.master[:]))
}

// Forget removes an archive's remembered key ("ask me each time").
func Forget(s Store, id ID) error { return s.Delete(Account(id)) }

var (
	// ErrKeyNotRetrievable refuses an unattended run that cannot get a key which
	// opens the archive (C7, A15).
	ErrKeyNotRetrievable = errors.New("vault: this archive's remembered key cannot be retrieved")
	// ErrVaultMissing is the gate's reason when an archive this computer recorded
	// as encrypted has no key file. It is deliberately NOT ErrNoVault: nothing
	// that reads "no vault, maybe plaintext" may match it (A15).
	ErrVaultMissing = errors.New("vault: this encrypted archive's key file is missing")
)

// Remedies the gate names, by cause.
const (
	fixRemember = "open the archive once in MailArchive with its password or recovery key to remember its key again"
	fixRestore  = "restore the archive's key files (" + FileName + ", " + BackupName + ") from a backup copy of the archive; until then it is not written to, so no mail is stored unencrypted"
	fixUpgrade  = "upgrade MailArchive: a newer version wrote this archive's key file"
	fixConnect  = "connect the drive or share that holds the archive, then run the backup again"
)

// RequireRetrievableKey is the unattended run's gate (C7; the twin of
// app.RequireExistingOut, MA-689). want is the archive id THIS MACHINE recorded
// when the archive was encrypted (A15) — never one read from the folder, which
// an attacker who can write the folder controls. It returns the opened vault
// (key, NeedsHeal, Forked) only when the key remembered under want
// authenticates a vault in out that names want, and otherwise refuses — naming
// the archive, the cause and the fix for that cause — before the caller writes
// anything: a vault deleted, damaged, swapped for another archive's or from a
// newer MailArchive is a refusal, never "not encrypted". It writes nothing
// itself; the caller heals under its own lock with Heal(out, want, KeySecret).
func RequireRetrievableKey(out string, want ID, s Store) (*Opened, error) {
	refuse := func(why, fix string, cause error) error {
		return fmt.Errorf("%w: archive %s (id %s): %s — %s: %w", ErrKeyNotRetrievable, out, want, why, fix, cause)
	}
	fixFor := func(err error) string {
		switch {
		case errors.Is(err, ErrNewerVault):
			return fixUpgrade
		case errors.Is(err, ErrWrongSecret), errors.Is(err, ErrKeyDestroyed):
			return fixRemember
		}
		return fixRestore
	}
	if want == (ID{}) {
		return nil, fmt.Errorf("%w: archive %s: no archive id was recorded for it on this computer", ErrKeyNotRetrievable, out)
	}
	if err := requireDir(out); err != nil {
		return nil, refuse("its folder is not present", fixConnect, err)
	}
	info, ok, err := Detect(out)
	switch {
	case err != nil:
		return nil, refuse("its key file is unusable", fixFor(err), err)
	case !ok:
		return nil, refuse("its key file is missing", fixRestore, ErrVaultMissing)
	case info.ArchiveID != want:
		return nil, refuse("its key file belongs to archive "+info.ArchiveID.String()+", not this one", fixRestore, ErrTampered)
	}
	stored, err := s.Get(Account(want))
	if err != nil {
		return nil, refuse("no key is stored for it", fixRemember, err)
	}
	master, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || len(master) != keySize {
		clear(master)
		return nil, refuse("the stored key is damaged", fixRemember, ErrCorrupt)
	}
	defer clear(master)
	k, err := newKey(want, master)
	if err != nil {
		return nil, refuse("the stored key is damaged", fixRemember, err)
	}
	defer k.Destroy()
	o, err := openVault(out, want, KeySecret(k))
	if err != nil {
		return nil, refuse("the stored key does not open it", fixFor(err), err)
	}
	return o, nil
}
