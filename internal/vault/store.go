package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

const (
	// FileName is the vault in the archive root; BackupName is its mirror (C3).
	FileName   = ".mailarchive-vault.json"
	BackupName = ".mailarchive-vault.bak.json"

	// tempPattern names in-flight vault writes so export.SweepOrphans reaps a
	// crash's leftover (A8).
	tempPattern = ".mailarchive-*.tmp"

	maxVaultBytes = 64 << 10
)

// ErrNoVault reports a folder with no vault: not an encrypted archive.
var ErrNoVault = errors.New("vault: no vault in this folder (not an encrypted archive)")

// copyOnDisk is one of the two vault files as read.
type copyOnDisk struct {
	name string
	raw  []byte
	v    *Vault
	err  error // os.ErrNotExist when absent
}

func readCopy(out, name string) copyOnDisk {
	c := copyOnDisk{name: name}
	f, err := util.OpenRegularFile(filepath.Join(out, name))
	if err != nil {
		c.err = err
		return c
	}
	defer f.Close()
	c.raw, err = io.ReadAll(io.LimitReader(f, maxVaultBytes+1))
	switch {
	case err != nil:
		c.err = err
	case len(c.raw) > maxVaultBytes:
		c.err = fmt.Errorf("%w: %s is larger than %d bytes", ErrCorrupt, name, maxVaultBytes)
	default:
		c.v, c.err = Parse(c.raw)
	}
	return c
}

// readCopies reads both copies and returns the ones that parse, highest
// generation first (the primary first on a tie), plus all of them as read.
func readCopies(out string) (parsed, all []copyOnDisk) {
	all = []copyOnDisk{readCopy(out, FileName), readCopy(out, BackupName)}
	for _, c := range all {
		if c.v != nil {
			parsed = append(parsed, c)
		}
	}
	sort.SliceStable(parsed, func(i, j int) bool { return parsed[i].v.Generation > parsed[j].v.Generation })
	return parsed, all
}

// noUsableCopy is the refusal when neither copy parses.
func noUsableCopy(out string, all []copyOnDisk) error {
	if errors.Is(all[0].err, os.ErrNotExist) && errors.Is(all[1].err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNoVault, out)
	}
	for _, c := range all {
		if c.err != nil && !errors.Is(c.err, os.ErrNotExist) {
			return fmt.Errorf("%s in %s: %w", c.name, out, c.err)
		}
	}
	return fmt.Errorf("%w: %s", ErrCorrupt, out)
}

// Info is what a reader WITHOUT the key may learn from the vault (A5): that the
// folder is an encrypted archive, which one, and its newest generation. None of
// it is authenticated without the key, so it may be used to recognise and label
// an archive, never to trust one.
type Info struct {
	ArchiveID  ID
	Generation uint64
}

// Detect recognises an encrypted archive without the key, from either copy
// (archindex, C10). ok is false when neither vault file exists; an error means
// a vault is present but neither copy parses.
func Detect(out string) (info Info, ok bool, err error) {
	parsed, all := readCopies(out)
	if len(parsed) == 0 {
		if err := noUsableCopy(out, all); !errors.Is(err, ErrNoVault) {
			return info, false, err
		}
		return info, false, nil
	}
	v := parsed[0].v
	return Info{ArchiveID: v.ArchiveID, Generation: v.Generation}, true, nil
}

// unlockFunc turns one parsed copy into a proven key, or refuses.
type unlockFunc func(*Vault) (*Key, error)

// Secret is what a person unlocks with: the password or the recovery key.
type Secret struct {
	text       string
	isRecovery bool
}

// PasswordSecret and RecoverySecret name the secret a person supplied.
func PasswordSecret(pw string) Secret   { return Secret{text: pw} }
func RecoverySecret(code string) Secret { return Secret{text: code, isRecovery: true} }
func (s Secret) unlock() unlockFunc     { return func(v *Vault) (*Key, error) { return s.unlockVault(v) } }
func (s Secret) unlockVault(v *Vault) (*Key, error) {
	if s.isRecovery {
		return v.UnlockRecovery(s.text)
	}
	return v.Unlock(s.text)
}

// Opened is an archive's vault proven with a key.
type Opened struct {
	Vault *Vault
	Key   *Key
	// NeedsHeal reports that the two copies are not both the winning vault
	// byte-for-byte (one missing, damaged, stale or altered). Only a writer
	// holding the archive lock heals, by saving the winner (Heal).
	NeedsHeal bool
}

// openVault unlocks the archive at out and chooses the vault to trust (A7):
// the secret may open any copy, but the winner is the HIGHEST generation whose
// header MAC verifies under the resulting key — the master key is the same in
// every generation, so it proves the newer copy too. A secret that opens only
// an older generation (the password before a change, after a crash left a stale
// copy) is refused as superseded, never used to roll the vault back.
func openVault(out string, unlock unlockFunc) (*Opened, error) {
	parsed, all := readCopies(out)
	if len(parsed) == 0 {
		return nil, noUsableCopy(out, all)
	}
	var key *Key
	var unlockedGen uint64
	var firstErr error
	for _, c := range parsed {
		k, err := unlock(c.v)
		if err == nil {
			key, unlockedGen = k, c.v.Generation
			break
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if key == nil {
		return nil, firstErr
	}
	var winner copyOnDisk
	for _, c := range parsed {
		if c.v.Verify(key) == nil {
			winner = c
			break
		}
	}
	if winner.v.Generation > unlockedGen {
		key.Destroy()
		return nil, fmt.Errorf("%w: it was replaced by a newer one — use the current password or recovery key", ErrWrongSecret)
	}
	want, err := winner.v.Marshal()
	if err != nil {
		key.Destroy()
		return nil, err
	}
	heal := false
	for _, c := range all {
		if !bytes.Equal(c.raw, want) {
			heal = true
		}
	}
	return &Opened{Vault: winner.v, Key: key, NeedsHeal: heal}, nil
}

// Open unlocks the encrypted archive at out with s, read-only: it takes no lock
// and writes nothing (the reader's path).
func Open(out string, s Secret) (*Opened, error) { return openVault(out, s.unlock()) }

// save writes v to the primary, then the mirror — each atomically, 0600, with a
// sweepable temp name and a durable rename (A7, A8). A crash between the two
// leaves the primary at the new generation and the mirror at the old; openVault
// then trusts the newer one that authenticates.
func save(out string, v *Vault) error {
	b, err := v.Marshal()
	if err != nil {
		return err
	}
	for _, name := range []string{FileName, BackupName} {
		if err := util.WriteFileAtomic0600Temp(filepath.Join(out, name), b, tempPattern); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// lockArchive takes the archive-folder lock (A9): the same lock every export,
// capture and reindex of this archive takes, so a vault rewrite can never
// interleave with a backup run.
func lockArchive(out, verb string) (*lockfile.Lock, error) {
	return lockfile.AcquireAs(filepath.Join(out, lockfile.Name), verb)
}

// requireDir refuses an archive folder that is not present, before any write.
func requireDir(out string) error {
	fi, err := os.Stat(out)
	if err != nil {
		return fmt.Errorf("archive folder %s is not present: %w", out, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("archive folder %s is not a folder", out)
	}
	return nil
}

// ErrExists refuses to create a vault where one (or a fragment of one) exists.
var ErrExists = errors.New("vault: this archive already has a vault")

// CreateAt writes a new vault for the archive folder out under the archive lock
// and returns the unlocked key and the recovery key to show once. It refuses if
// either vault file already exists — re-keying an encrypted archive would
// orphan everything sealed under the old key.
func CreateAt(out, password string) (*Key, RecoveryKey, error) {
	var none RecoveryKey
	if err := requireDir(out); err != nil {
		return nil, none, err
	}
	l, err := lockArchive(out, "encrypt")
	if err != nil {
		return nil, none, err
	}
	defer l.Release()
	for _, name := range []string{FileName, BackupName} {
		if _, err := os.Lstat(filepath.Join(out, name)); !errors.Is(err, os.ErrNotExist) {
			return nil, none, fmt.Errorf("%w: %s is present in %s", ErrExists, name, out)
		}
	}
	v, k, rk, err := Create(password)
	if err != nil {
		return nil, none, err
	}
	if err := save(out, v); err != nil {
		k.Destroy()
		return nil, none, err
	}
	return k, rk, nil
}

// mutate opens the vault at out under the archive lock with current, applies
// change, and saves the result (both copies, which also heals them).
func mutate(out, verb string, current Secret, change func(*Opened) (*Vault, error)) error {
	if err := requireDir(out); err != nil {
		return err
	}
	l, err := lockArchive(out, verb)
	if err != nil {
		return err
	}
	defer l.Release()
	o, err := openVault(out, current.unlock())
	if err != nil {
		return err
	}
	defer o.Key.Destroy()
	n, err := change(o)
	if err != nil {
		return err
	}
	if n.Generation <= o.Vault.Generation {
		return fmt.Errorf("vault: internal: a rewrite must raise the generation (%d → %d)", o.Vault.Generation, n.Generation)
	}
	return save(out, n)
}

// ChangePasswordAt sets a new password for the archive at out (G3), unlocking
// with current — the old password, or the recovery key to reset a forgotten
// one (C9). Only the vault is rewritten; the remembered key and every sealed
// byte stay as they are (A10).
func ChangePasswordAt(out string, current Secret, newPassword string) error {
	return mutate(out, "change-password", current, func(o *Opened) (*Vault, error) {
		return o.Vault.WithPassword(o.Key, newPassword)
	})
}

// RegenerateRecoveryAt replaces the archive's recovery key (C9) and returns the
// new one, to show once. The old recovery key stops working.
func RegenerateRecoveryAt(out string, current Secret) (RecoveryKey, error) {
	var rk RecoveryKey
	err := mutate(out, "new-recovery-key", current, func(o *Opened) (*Vault, error) {
		n, k, err := o.Vault.WithNewRecovery(o.Key)
		rk = k
		return n, err
	})
	if err != nil {
		return RecoveryKey{}, err
	}
	return rk, nil
}

// Heal rewrites both vault copies from the trusted winner, under the archive
// lock, when Open reported NeedsHeal. The generation is unchanged: healing
// restores a copy, it does not make a new vault.
func Heal(out string, current Secret) error {
	if err := requireDir(out); err != nil {
		return err
	}
	l, err := lockArchive(out, "heal-vault")
	if err != nil {
		return err
	}
	defer l.Release()
	o, err := openVault(out, current.unlock())
	if err != nil {
		return err
	}
	defer o.Key.Destroy()
	if !o.NeedsHeal {
		return nil
	}
	return save(out, o.Vault)
}
