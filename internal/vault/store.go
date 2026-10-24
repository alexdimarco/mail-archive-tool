package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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

var (
	// ErrNoVault reports a folder with no vault file. Absence is unauthenticated:
	// it does not prove the folder was never encrypted (a deleted vault looks the
	// same), so a path that knows an archive id for the folder refuses instead
	// (RequireRetrievableKey, A15).
	ErrNoVault = errors.New("vault: no vault found in this folder")
	// ErrExists refuses to create a vault where one (or a fragment of one) exists.
	ErrExists = errors.New("vault: this archive already has a vault")
	// ErrConflict refuses to settle two different vaults at the same generation
	// (two machines changed the vault at once), or to commit a prepared change
	// over a vault that changed since it was prepared.
	ErrConflict = errors.New("vault: the archive's key file changed or forked")
	// ErrPartiallySaved reports a change that IS in effect — the primary holds it
	// — whose mirror could not be written; the next change or heal repairs it.
	ErrPartiallySaved = errors.New("vault: the change is saved, but its backup copy could not be written")
)

// writeCopy writes one vault copy atomically and durably (A8); a var so a test
// can observe the writes or fail one.
var writeCopy = util.WriteFileAtomic0600Temp

// copyOnDisk is one of the two vault files as read.
type copyOnDisk struct {
	name string
	raw  []byte
	v    *Vault
	err  error // os.ErrNotExist when absent
}

func readCopy(out, name string) copyOnDisk {
	c := copyOnDisk{name: name}
	path := filepath.Join(out, name)
	// Judge the path before opening it: on Windows the open itself follows a
	// link (util.OpenRegularFile), so this is what refuses one there.
	fi, err := os.Lstat(path)
	if err != nil {
		c.err = err
		return c
	}
	if !fi.Mode().IsRegular() {
		c.err = fmt.Errorf("%w: %s is not a regular file (a link or special file was put there)", ErrCorrupt, name)
		return c
	}
	f, err := util.OpenRegularFile(path)
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

// copies is both vault files as read (primary first), and those that parse,
// highest generation first (the primary first on a tie).
type copies struct {
	all    []copyOnDisk
	parsed []copyOnDisk
}

// readCopies reads both copies. Before anything trusts either, it refuses when
// a copy was written by a newer MailArchive (never fall back past it, never
// heal over it) or when the two copies name different archives (never adopt or
// heal across archives), and when neither copy parses.
func readCopies(out string) (copies, error) {
	cs := copies{all: []copyOnDisk{readCopy(out, FileName), readCopy(out, BackupName)}}
	for _, c := range cs.all {
		if errors.Is(c.err, ErrNewerVault) {
			return cs, fmt.Errorf("%s in %s: %w", c.name, out, c.err)
		}
		if c.v != nil {
			cs.parsed = append(cs.parsed, c)
		}
	}
	if len(cs.parsed) == 0 {
		return cs, noUsableCopy(out, cs.all)
	}
	if len(cs.parsed) == 2 && cs.all[0].v.ArchiveID != cs.all[1].v.ArchiveID {
		return cs, fmt.Errorf("%w: in %s, %s and %s belong to different archives (%s and %s) — restore this archive's own key file",
			ErrTampered, out, FileName, BackupName, cs.all[0].v.ArchiveID, cs.all[1].v.ArchiveID)
	}
	sort.SliceStable(cs.parsed, func(i, j int) bool { return cs.parsed[i].v.Generation > cs.parsed[j].v.Generation })
	return cs, nil
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
// folder holds a vault, which archive, and its newest generation. None of it is
// authenticated without the key, so it may be used to recognise and label an
// archive, never to trust one.
type Info struct {
	ArchiveID  ID
	Generation uint64
}

// Detect recognises an encrypted archive without the key, from either copy
// (archindex, C10). ok is false when neither vault file exists — which does NOT
// prove the folder is plaintext (ErrNoVault); an error means a vault is present
// but unusable: damaged, from a newer MailArchive, or two archives' copies.
func Detect(out string) (info Info, ok bool, err error) {
	cs, err := readCopies(out)
	if errors.Is(err, ErrNoVault) {
		return info, false, nil
	}
	if err != nil {
		return info, false, err
	}
	v := cs.parsed[0].v
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

func (s Secret) unlock() unlockFunc {
	return func(v *Vault) (*Key, error) {
		if s.isRecovery {
			return v.UnlockRecovery(s.text)
		}
		return v.Unlock(s.text)
	}
}

// Opened is an archive's vault proven with a key.
type Opened struct {
	Vault *Vault
	Key   *Key
	// NeedsHeal reports that the two copies are not both the winning vault
	// byte-for-byte (one missing, damaged, stale or altered). Only a writer
	// holding the archive lock heals.
	NeedsHeal bool
	// Forked reports two DIFFERENT authentic vaults at the newest generation:
	// two machines changed the vault at once. Heal will not choose between them;
	// the next deliberate change (from the copy this secret opened) settles it.
	Forked bool
}

// cost orders copies for unlocking: the Argon2id work its two entries ask for.
func (v *Vault) cost() uint64 {
	c := uint64(0)
	for _, e := range []Entry{v.Password, v.Recovery} {
		c += uint64(e.Params.MemoryKiB) * uint64(e.Params.Time)
	}
	return c
}

// openVault unlocks the archive at out and chooses the vault to trust (A7).
// The cheapest copy is tried first and a byte-identical copy is not tried twice,
// so a planted expensive copy costs a derivation only when the real one fails.
// The winner is the HIGHEST generation whose header MAC verifies under the
// resulting key — the master key is the same in every generation, so it proves
// newer copies too — and on a tie, the copy the secret itself opened. A secret
// that opens only an older generation (the password before a change, after a
// crash left a stale copy) is refused as superseded, never used to roll back.
func openVault(out string, unlock unlockFunc) (*Opened, error) {
	cs, err := readCopies(out)
	if err != nil {
		return nil, err
	}
	tries := slices.Clone(cs.parsed)
	sort.SliceStable(tries, func(i, j int) bool { return tries[i].v.cost() < tries[j].v.cost() })
	var key *Key
	var opened copyOnDisk
	var errs []error
	for i, c := range tries {
		if slices.ContainsFunc(tries[:i], func(t copyOnDisk) bool { return bytes.Equal(t.raw, c.raw) }) {
			continue
		}
		k, err := unlock(c.v)
		if err == nil {
			key, opened = k, c
			break
		}
		errs = append(errs, err)
	}
	if key == nil {
		return nil, mostTelling(errs)
	}
	var authentic []copyOnDisk
	for _, c := range cs.parsed {
		if c.v.Verify(key) == nil {
			authentic = append(authentic, c)
		}
	}
	if top := authentic[0].v.Generation; top > opened.v.Generation {
		key.Destroy()
		return nil, fmt.Errorf("%w: it was replaced by a newer one — use the current password or recovery key", ErrWrongSecret)
	}
	o := &Opened{Vault: opened.v, Key: key}
	for _, c := range authentic {
		if c.v.Generation == opened.v.Generation && !bytes.Equal(c.raw, opened.raw) {
			o.Forked = true
		}
	}
	want, err := opened.v.Marshal()
	if err != nil {
		key.Destroy()
		return nil, err
	}
	for _, c := range cs.all {
		if !bytes.Equal(c.raw, want) {
			o.NeedsHeal = true
		}
	}
	return o, nil
}

// mostTelling picks the refusal to show when no copy unlocked: a wrong secret
// if any copy said so (the likeliest truth, and not one a planted copy can
// mask), else the first copy's reason.
func mostTelling(errs []error) error {
	for _, e := range errs {
		if errors.Is(e, ErrWrongSecret) {
			return e
		}
	}
	return errs[0]
}

// Open unlocks the encrypted archive at out with s, read-only: it takes no lock
// and writes nothing (the reader's path).
func Open(out string, s Secret) (*Opened, error) { return openVault(out, s.unlock()) }

// save writes v to the primary, then the mirror (A7) — each atomically, 0600,
// with a sweepable temp name and a durable rename (A8). The primary is the
// commit point: if it was written and the mirror was not, the change IS in
// effect and the error says so (ErrPartiallySaved).
func save(out string, v *Vault) error {
	b, err := v.Marshal()
	if err != nil {
		return err
	}
	if err := writeCopy(filepath.Join(out, FileName), b, tempPattern); err != nil {
		return fmt.Errorf("write %s: %w", FileName, err)
	}
	if err := writeCopy(filepath.Join(out, BackupName), b, tempPattern); err != nil {
		return fmt.Errorf("%w: %s in %s holds the change, but %s could not be written (%v); it is repaired on the next change or heal",
			ErrPartiallySaved, FileName, out, BackupName, err)
	}
	return nil
}

// lockArchive takes the archive-folder lock (A9): the same lock every export,
// capture and reindex of this archive takes, so a vault rewrite can never
// interleave with a backup run.
func lockArchive(out, verb string) (*lockfile.Lock, error) {
	return lockfile.AcquireAs(filepath.Join(out, lockfile.Name), verb)
}

// stillHeld re-proves the lock immediately before a write (R5): a lock file
// removed or replaced mid-operation means another run may hold a fresh one.
func stillHeld(l *lockfile.Lock) error {
	if err := l.StillHeld(); err != nil {
		return fmt.Errorf("vault: nothing was written: %w", err)
	}
	return nil
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

// PendingCreate is a new vault made in memory and not yet written: a recovery
// key nobody has seen is never committed (the seavault rule). The caller shows
// RecoveryKey, the person confirms they saved it, and only then Commit writes
// the vault; a crash before Commit leaves the folder untouched.
type PendingCreate struct {
	v  *Vault
	k  *Key
	rk RecoveryKey
}

// PrepareCreate makes the vault for a newly encrypted archive, in memory.
func PrepareCreate(password string) (*PendingCreate, error) {
	v, k, rk, err := Create(password)
	if err != nil {
		return nil, err
	}
	return &PendingCreate{v: v, k: k, rk: rk}, nil
}

// RecoveryKey is the key to show once, before Commit.
func (p *PendingCreate) RecoveryKey() RecoveryKey { return p.rk }

// ArchiveID is the new archive's id, to record outside the archive (A15).
func (p *PendingCreate) ArchiveID() ID { return p.v.ArchiveID }

// Discard forgets an uncommitted vault.
func (p *PendingCreate) Discard() { p.k.Destroy() }

// Commit writes the prepared vault into the archive folder out under the
// archive lock and returns the unlocked key. It refuses if either vault file
// already exists — re-keying an encrypted archive would orphan everything
// sealed under the old key. If the write fails part-way it removes what it
// wrote, so the folder is as it was and a retry can proceed.
func (p *PendingCreate) Commit(out string) (*Key, error) {
	if err := requireDir(out); err != nil {
		return nil, err
	}
	l, err := lockArchive(out, "encrypt")
	if err != nil {
		return nil, err
	}
	defer l.Release()
	names := []string{FileName, BackupName}
	for _, name := range names {
		if _, err := os.Lstat(filepath.Join(out, name)); !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s is present in %s", ErrExists, name, out)
		}
	}
	if err := stillHeld(l); err != nil {
		return nil, err
	}
	if err := save(out, p.v); err != nil {
		var left []string
		for _, name := range names {
			if rm := os.Remove(filepath.Join(out, name)); rm != nil && !errors.Is(rm, os.ErrNotExist) {
				left = append(left, name)
			}
		}
		if len(left) > 0 {
			return nil, fmt.Errorf("vault: the new vault could not be written (%v), and %s could not be removed — delete it before trying again", err, strings.Join(left, ", "))
		}
		return nil, fmt.Errorf("vault: the new vault could not be written, so none was kept — try again: %v", err)
	}
	return p.k, nil
}

// CreateAt prepares and commits a vault in one step, returning the key and the
// recovery key. An interactive caller uses PrepareCreate/Commit instead, so the
// recovery key is shown and confirmed before anything is written.
func CreateAt(out, password string) (*Key, RecoveryKey, error) {
	p, err := PrepareCreate(password)
	if err != nil {
		return nil, RecoveryKey{}, err
	}
	k, err := p.Commit(out)
	if err != nil {
		p.Discard()
		return nil, RecoveryKey{}, err
	}
	return k, p.rk, nil
}

// mutate opens the vault at out under the archive lock with current, applies
// change, and saves the result (both copies, which also heals them). Building
// on the copy the secret opened, at a generation above every copy, also
// settles a fork.
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
	if err := stillHeld(l); err != nil {
		return err
	}
	return save(out, n)
}

// ChangePasswordAt sets a new password for the archive at out (G3), unlocking
// with current — the old password, or the recovery key to reset a forgotten
// one (C9). Only the vault is rewritten; the remembered key and every sealed
// byte stay as they are (A10). ErrPartiallySaved means the new password IS in
// effect.
func ChangePasswordAt(out string, current Secret, newPassword string) error {
	return mutate(out, "change-password", current, func(o *Opened) (*Vault, error) {
		return o.Vault.WithPassword(o.Key, newPassword)
	})
}

// PendingRecovery is a new recovery key made in memory and not yet written: the
// caller shows RecoveryKey, the person confirms they saved it, and only then
// Commit replaces the old one, which keeps working until then.
type PendingRecovery struct {
	out  string
	key  *Key
	base *Vault
	next *Vault
	rk   RecoveryKey
}

// PrepareNewRecovery unlocks the archive at out with current and builds the
// vault with a new recovery key, writing nothing.
func PrepareNewRecovery(out string, current Secret) (*PendingRecovery, error) {
	o, err := Open(out, current)
	if err != nil {
		return nil, err
	}
	next, rk, err := o.Vault.WithNewRecovery(o.Key)
	if err != nil {
		o.Key.Destroy()
		return nil, err
	}
	return &PendingRecovery{out: out, key: o.Key, base: o.Vault, next: next, rk: rk}, nil
}

// RecoveryKey is the new key to show once, before Commit.
func (p *PendingRecovery) RecoveryKey() RecoveryKey { return p.rk }

// Discard forgets an uncommitted recovery key; the old one stays in force.
func (p *PendingRecovery) Discard() { p.key.Destroy() }

// Commit replaces the recovery key under the archive lock — only if the vault
// is still the one the change was prepared from (ErrConflict otherwise:
// something else changed it; prepare again). ErrPartiallySaved means the new
// key IS in effect.
func (p *PendingRecovery) Commit() error {
	defer p.key.Destroy()
	if err := requireDir(p.out); err != nil {
		return err
	}
	l, err := lockArchive(p.out, "new-recovery-key")
	if err != nil {
		return err
	}
	defer l.Release()
	o, err := openVault(p.out, rememberedUnlock(p.key.master[:]))
	if err != nil {
		return err
	}
	defer o.Key.Destroy()
	if o.Vault.Generation != p.base.Generation || o.Vault.mac != p.base.mac {
		return fmt.Errorf("%w: the key file in %s changed since the new recovery key was made; make it again", ErrConflict, p.out)
	}
	if err := stillHeld(l); err != nil {
		return err
	}
	return save(p.out, p.next)
}

// RegenerateRecoveryAt prepares and commits a new recovery key in one step and
// returns it — also alongside ErrPartiallySaved, when it is already in effect.
// An interactive caller uses PrepareNewRecovery/Commit instead.
func RegenerateRecoveryAt(out string, current Secret) (RecoveryKey, error) {
	p, err := PrepareNewRecovery(out, current)
	if err != nil {
		return RecoveryKey{}, err
	}
	if err := p.Commit(); err != nil {
		if errors.Is(err, ErrPartiallySaved) {
			return p.rk, err
		}
		return RecoveryKey{}, err
	}
	return p.rk, nil
}

// Heal rewrites both vault copies from the trusted winner, under the archive
// lock, when Open reported NeedsHeal. The generation is unchanged: healing
// restores a copy, it does not make a new vault. It refuses a fork (ErrConflict)
// rather than choose which of two people's changes to keep.
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
	if o.Forked {
		return fmt.Errorf("%w: %s and %s in %s are two different key files at generation %d (changed on two machines at once); change the password or make a new recovery key to settle which is kept",
			ErrConflict, FileName, BackupName, out, o.Vault.Generation)
	}
	if !o.NeedsHeal {
		return nil
	}
	if err := stillHeld(l); err != nil {
		return err
	}
	return save(out, o.Vault)
}
