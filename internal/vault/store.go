package vault

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

	// TempPattern names in-flight vault writes so export.SweepTemps reaps a
	// crash's leftover (A8).
	TempPattern = ".mailarchive-*.tmp"

	maxVaultBytes = 64 << 10
)

var (
	// ErrNoVault reports a folder with no vault file. Absence is unauthenticated:
	// it does not prove the folder was never encrypted (a deleted vault looks the
	// same), so a path that knows an archive id for the folder refuses instead
	// (RequireRetrievableKey, A15).
	ErrNoVault = errors.New("this folder holds no encrypted archive's key file")
	// ErrExists refuses to create a vault where one (or a fragment of one) exists.
	ErrExists = errors.New("this archive already has a key file")
	// ErrConflict refuses to settle two different vaults at the same generation
	// (two machines changed the vault at once), or to commit a prepared change
	// over a vault that changed since it was prepared.
	ErrConflict = errors.New("the archive's key file was changed meanwhile, here or on another computer")
	// ErrPartiallySaved reports a change that IS in effect — the primary holds it
	// — whose mirror could not be written; the next change or heal repairs it.
	ErrPartiallySaved = errors.New("the change is saved, but the second copy of the key file could not be written")
	// ErrPendingUsed refuses a prepared change that was already committed or
	// discarded.
	ErrPendingUsed = errors.New("this change was already saved or abandoned")
)

// Seams: writeCopy writes one vault copy atomically and durably (A8),
// lockedHook runs right after a writer takes the archive lock, and lstat judges
// a vault path before it is opened — so a test can observe or fail a write,
// take the lock away mid-operation, or present a path as Windows would (a link
// the open itself would follow there).
var (
	writeCopy  = util.WriteFileAtomic0600Temp
	lockedHook = func() {}
	lstat      = os.Lstat
)

// copyOnDisk is one of the two vault files as read.
type copyOnDisk struct {
	name string
	raw  []byte
	v    *Vault
	err  error // os.ErrNotExist when absent
}

// judgeVaultFile is the file-type rule for a vault copy, judged on the path
// BEFORE it is opened (on Windows the open itself follows a link): a regular
// file anywhere, and on Windows also a cloud placeholder (ModeIrregular) — the
// rule util.DescriptorReadable applies to every untrusted file. A link, a
// directory, a pipe or a device is refused.
func judgeVaultFile(goos string, mode fs.FileMode) error {
	if mode&fs.ModeSymlink != 0 || !util.DescriptorReadable(goos, mode) {
		return fmt.Errorf("%w: it is %s, not a regular file (a link or special file was put there)", ErrCorrupt, util.ModeKind(mode))
	}
	return nil
}

func readCopy(out, name string) copyOnDisk {
	c := copyOnDisk{name: name}
	path := filepath.Join(out, name)
	fi, err := lstat(path)
	if err != nil {
		c.err = err
		return c
	}
	if err := judgeVaultFile(runtime.GOOS, fi.Mode()); err != nil {
		c.err = fmt.Errorf("%s: %w", name, err)
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

// claimedID is the archive id a copy that does not parse still claims, if its
// "archive_id" reads as one — so a damaged copy of ANOTHER archive is told
// apart from a damaged copy of this one.
func claimedID(raw []byte) (ID, bool) {
	var probe struct {
		ArchiveID string `json:"archive_id"`
	}
	if json.NewDecoder(bytes.NewReader(raw)).Decode(&probe) != nil {
		return ID{}, false
	}
	b, err := hex.DecodeString(probe.ArchiveID)
	if err != nil || len(b) != len(ID{}) {
		return ID{}, false
	}
	var id ID
	copy(id[:], b)
	return id, id != ID{}
}

// copies is both vault files as read (primary first), and those that parse,
// highest generation first (the primary first on a tie).
type copies struct {
	all    []copyOnDisk
	parsed []copyOnDisk
}

// readCopies reads both copies. Before anything trusts either, it refuses when
// a copy was written by a newer MailArchive (never fall back past it, never
// heal over it), when the copies — parsed or merely claiming an id — name two
// archives, when they name an archive other than want (the id this machine
// recorded; zero when none is recorded, A15), and when neither copy parses.
func readCopies(out string, want ID) (copies, error) {
	cs := copies{all: []copyOnDisk{readCopy(out, FileName), readCopy(out, BackupName)}}
	ids := map[ID][]string{}
	for _, c := range cs.all {
		if errors.Is(c.err, ErrNewerVault) {
			return cs, fmt.Errorf("%s in %s: %w", c.name, out, c.err)
		}
		if c.v != nil {
			cs.parsed = append(cs.parsed, c)
			ids[c.v.ArchiveID] = append(ids[c.v.ArchiveID], c.name)
		} else if id, ok := claimedID(c.raw); ok {
			ids[id] = append(ids[id], c.name)
		}
	}
	if len(ids) > 1 {
		var names []string
		for id, files := range ids {
			names = append(names, strings.Join(files, " and ")+" belongs to archive "+id.String())
		}
		sort.Strings(names)
		return cs, fmt.Errorf("%w: in %s the key files belong to different archives (%s) — restore this archive's own key file",
			ErrTampered, out, strings.Join(names, "; "))
	}
	if want != (ID{}) {
		for id, files := range ids {
			if id != want {
				return cs, fmt.Errorf("%w: in %s, %s belongs to archive %s, not to archive %s that this computer recorded there — restore this archive's own key file",
					ErrTampered, out, strings.Join(files, " and "), id, want)
			}
		}
	}
	if len(cs.parsed) == 0 {
		return cs, noUsableCopy(out, cs.all)
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
// prove the folder is plaintext (ErrNoVault, A15); an error means a vault is
// present but unusable: damaged, from a newer MailArchive, or two archives'.
func Detect(out string) (info Info, ok bool, err error) {
	cs, err := readCopies(out, ID{})
	if errors.Is(err, ErrNoVault) {
		return info, false, nil
	}
	if err != nil {
		return info, false, err
	}
	v := cs.parsed[0].v
	return Info{ArchiveID: v.ArchiveID, Generation: v.Generation}, true, nil
}

// Secret is what unlocks an archive: the password, the recovery key, or an
// already-unlocked key (a remembered one, for an unattended heal). It prints
// redacted.
type Secret struct {
	text string
	kind entryKind // entryPassword, entryRecovery, or 0 for a key
	key  *Key
}

// PasswordSecret, RecoverySecret and KeySecret name the secret supplied.
func PasswordSecret(pw string) Secret   { return Secret{text: pw, kind: entryPassword} }
func RecoverySecret(code string) Secret { return Secret{text: code, kind: entryRecovery} }
func KeySecret(k *Key) Secret           { return Secret{key: k} }

// Format prints a secret without its text, whatever the verb.
func (s Secret) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "vault.Secret{%s, redacted}", map[entryKind]string{0: "key", entryPassword: "password", entryRecovery: "recovery key"}[s.kind])
}

// open turns one parsed copy into a proven key, or refuses.
func (s Secret) open(v *Vault) (*Key, error) {
	switch s.kind {
	case entryPassword:
		return v.Unlock(s.text)
	case entryRecovery:
		return v.UnlockRecovery(s.text)
	}
	if err := s.key.usable(); err != nil {
		return nil, err
	}
	k, err := newKey(v.ArchiveID, s.key.master[:])
	if err != nil {
		return nil, err
	}
	if err := v.Verify(k); err != nil {
		// A key either is this archive's or is not: a mismatch says the key is
		// wrong (another archive's), the same way a wrong password does.
		k.Destroy()
		return nil, fmt.Errorf("%w: the key does not belong to this archive (%v)", ErrWrongSecret, err)
	}
	return k, nil
}

// entry is the wrap entry this secret unlocks in v (the zero Entry for a key).
func (s Secret) entry(v *Vault) Entry {
	switch s.kind {
	case entryPassword:
		return v.Password
	case entryRecovery:
		return v.Recovery
	}
	return Entry{}
}

// cost is the Argon2id work unlocking v with this secret takes (none for a key).
func (s Secret) cost(v *Vault) uint64 {
	p := s.entry(v).Params
	return uint64(p.MemoryKiB) * uint64(p.Time)
}

func sameEntry(a, b Entry) bool {
	return a.Salt == b.Salt && a.Params == b.Params && bytes.Equal(a.Wrapped, b.Wrapped)
}

// Opened is an archive's vault proven with a key.
type Opened struct {
	Vault *Vault
	Key   *Key
	// NeedsHeal reports that the two copies are not both the winning vault
	// byte-for-byte (one missing, damaged, stale, altered or reformatted). Only
	// a writer holding the archive lock heals.
	NeedsHeal bool
	// Forked reports a DIFFERENT authentic vault at the winning generation: two
	// machines changed the vault at once. Heal will not choose between them; the
	// next deliberate change from this side settles it — and the other side's
	// new password or recovery key then stops working, which a caller must say.
	Forked bool
}

// openVault unlocks the archive at out with s and chooses the vault to trust
// (A7). The copy that is cheapest for s is tried first and a byte-identical
// copy is not tried twice, so a planted expensive copy costs a derivation only
// when the real one fails. The winner is the HIGHEST generation that
// authenticates under the resulting key — the master key is the same in every
// generation — provided s still opens it: a remembered key always does; a
// password or recovery key whose entry is unchanged there does without another
// derivation; otherwise it is tried once more. A secret that opens only an
// older generation (the password before a change, after a crash left a stale
// copy) is refused as superseded, never used to roll back.
func openVault(out string, want ID, s Secret, limit *Params) (*Opened, error) {
	cs, err := readCopies(out, want)
	if err != nil {
		return nil, err
	}
	// Every copy's cost is judged against the limit before ANY derivation, on the
	// bytes it would derive from — the first try and the re-derivation on a newer
	// copy alike — and the refusal names the most each setting asks, so one
	// question covers both copies (OpenWithin; K14; the P3-1b pass, CRY-01, REG-02,
	// REG-03).
	if limit != nil && s.kind != 0 {
		var over *Params
		for _, c := range cs.parsed {
			if p := s.entry(c.v).Params; p.above(*limit) {
				if over != nil {
					p = p.join(*over)
				}
				over = &p
			}
		}
		if over != nil {
			return nil, &CostError{Params: *over}
		}
	}
	tries := slices.Clone(cs.parsed)
	sort.SliceStable(tries, func(i, j int) bool { return s.cost(tries[i].v) < s.cost(tries[j].v) })
	var key *Key
	var opened copyOnDisk
	var errs []error
	for i, c := range tries {
		if slices.ContainsFunc(tries[:i], func(t copyOnDisk) bool { return bytes.Equal(t.raw, c.raw) }) {
			continue
		}
		k, err := s.open(c.v)
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
	winner := opened
	if top := authentic[0]; top.v.Generation > opened.v.Generation {
		current := s.kind == 0 || sameEntry(s.entry(top.v), s.entry(opened.v))
		if !current {
			if k, err := s.open(top.v); err == nil {
				k.Destroy()
				current = true
			}
		}
		if !current {
			key.Destroy()
			return nil, fmt.Errorf("%w: it was replaced by a newer one — use the current password or recovery key", ErrWrongSecret)
		}
		winner = top
	}
	o := &Opened{Vault: winner.v, Key: key}
	for _, c := range authentic {
		if c.v.Generation == winner.v.Generation && c.v.mac != winner.v.mac {
			o.Forked = true
		}
	}
	want2, err := winner.v.Marshal()
	if err != nil {
		key.Destroy()
		return nil, err
	}
	for _, c := range cs.all {
		if !bytes.Equal(c.raw, want2) {
			o.NeedsHeal = true
		}
	}
	return o, nil
}

// mostTelling picks the refusal to show when no copy unlocked. Tampering found
// AFTER the secret unwrapped a key is the strongest evidence (the secret is
// right and a header was altered), then a wrong secret (not one a planted copy
// can mask), then the first copy's reason.
func mostTelling(errs []error) error {
	for _, want := range []error{ErrTampered, ErrWrongSecret} {
		for _, e := range errs {
			if errors.Is(e, want) {
				return e
			}
		}
	}
	return errs[0]
}

// Open unlocks the encrypted archive at out with s, read-only: it takes no lock
// and writes nothing (the reader's path). want is the archive id this machine
// recorded for out, or the zero ID when none is recorded (a handed-over copy).
func Open(out string, want ID, s Secret) (*Opened, error) { return openVault(out, want, s, nil) }

// VerifyKey proves that k opens the encrypted archive at out (whose id must be
// want, when one is recorded) and hands out nothing: a writer that already holds
// a key checks it belongs to the folder it is about to write (phase2a P2a-C9c).
// Read-only, like Open.
func VerifyKey(out string, want ID, k *Key) error {
	o, err := openVault(out, want, KeySecret(k), nil)
	if err != nil {
		return err
	}
	o.Key.Destroy()
	return nil
}

// save writes v to the primary, then the mirror (A7) — each atomically, 0600,
// with a sweepable temp name and a durable rename (A8). The primary is the
// commit point: if it was written and the mirror was not, the change IS in
// effect and the error says so (ErrPartiallySaved).
func save(out string, v *Vault) error {
	b, err := v.Marshal()
	if err != nil {
		return err
	}
	if err := writeCopy(filepath.Join(out, FileName), b, TempPattern); err != nil {
		return fmt.Errorf("write %s: %w", FileName, err)
	}
	if err := writeCopy(filepath.Join(out, BackupName), b, TempPattern); err != nil {
		return fmt.Errorf("%w: %s in %s holds the change, but %s could not be written (it is repaired on the next change or heal): %w",
			ErrPartiallySaved, FileName, out, BackupName, err)
	}
	return nil
}

// lockArchive takes the archive-folder lock (A9): the same lock every export,
// capture and reindex of this archive takes, so a vault rewrite can never
// interleave with a backup run. It names no verb: the lock file stays beside the
// archive in plaintext, and an encrypted archive's discloses only its host=, as a
// keyed run's does (A18; phase3 P3-3 plan).
func lockArchive(out string) (*lockfile.Lock, error) {
	l, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "")
	if err == nil {
		lockedHook()
	}
	return l, err
}

// stillHeld re-proves the lock immediately before a write (R5): a lock file
// removed or replaced mid-operation means another run may hold a fresh one.
func stillHeld(l *lockfile.Lock) error {
	if err := l.StillHeld(); err != nil {
		return fmt.Errorf("nothing was written: %w", err)
	}
	return nil
}

// requireDir refuses an archive folder that is not present, before any write.
func requireDir(out string) error {
	fi, err := os.Stat(out)
	if err != nil {
		return fmt.Errorf("archive folder %s is not present (is its drive connected?): %w", out, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("archive folder %s is not a folder", out)
	}
	return nil
}

// refuseExisting refuses a folder that already holds either vault file.
func refuseExisting(out string) error {
	for _, name := range []string{FileName, BackupName} {
		if _, err := os.Lstat(filepath.Join(out, name)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s is present in %s — if an earlier attempt to encrypt this archive finished, the password and recovery key you saved then open it", ErrExists, name, out)
		}
	}
	return nil
}

// PendingCreate is a new vault made in memory and not yet written: a recovery
// key nobody has seen is never committed (the seavault rule). The caller shows
// RecoveryKey, the person confirms they saved it, and only then Commit writes
// the vault; a crash before Commit leaves the folder untouched. It prints
// redacted.
type PendingCreate struct {
	out  string
	v    *Vault
	k    *Key
	rk   RecoveryKey
	done bool
}

// PrepareCreate makes the vault for the archive folder out, in memory. It
// refuses — before any recovery key is made or shown — a folder that is absent
// or already holds a vault.
func PrepareCreate(out, password string) (*PendingCreate, error) {
	if err := requireDir(out); err != nil {
		return nil, err
	}
	if err := refuseExisting(out); err != nil {
		return nil, err
	}
	v, k, rk, err := Create(password)
	if err != nil {
		return nil, err
	}
	return &PendingCreate{out: out, v: v, k: k, rk: rk}, nil
}

// RecoveryKey is the key to show once, before Commit.
func (p *PendingCreate) RecoveryKey() RecoveryKey { return p.rk }

// ArchiveID is the new archive's id, to record outside the archive (A15).
func (p *PendingCreate) ArchiveID() ID { return p.v.ArchiveID }

// RecoveryFile is the new archive's recovery file, offered with its recovery key
// (phase3 §5.1 step 5, §9.4).
func (p *PendingCreate) RecoveryFile() ([]byte, error) { return p.v.RecoveryFile() }

// Format prints a pending create without its keys.
func (p *PendingCreate) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "vault.PendingCreate{archive %s, redacted}", p.v.ArchiveID)
}

// Discard forgets an uncommitted vault. After a successful Commit it does
// nothing: the key belongs to Commit's caller.
func (p *PendingCreate) Discard() {
	if !p.done {
		p.k.Destroy()
	}
	p.done = true
}

// Commit writes the prepared vault under the archive lock and hands over the
// unlocked key (the pending create keeps no claim on it). It refuses if either
// vault file exists — re-keying an encrypted archive would orphan everything
// sealed under the old key. If the write fails part-way it removes what it
// wrote (while still holding the lock), so the folder is as it was and the
// same pending create can be committed again.
func (p *PendingCreate) Commit() (*Key, error) {
	if p.done {
		return nil, ErrPendingUsed
	}
	if err := requireDir(p.out); err != nil {
		return nil, err
	}
	l, err := lockArchive(p.out)
	if err != nil {
		return nil, err
	}
	defer l.Release()
	if err := refuseExisting(p.out); err != nil {
		return nil, err
	}
	if err := stillHeld(l); err != nil {
		return nil, err
	}
	if err := save(p.out, p.v); err != nil {
		if held := stillHeld(l); held != nil {
			return nil, fmt.Errorf("the new key file could not be written (%w), and the archive lock was lost, so nothing was cleaned up — check %s before trying again: %w", err, p.out, held)
		}
		var left []string
		for _, name := range []string{FileName, BackupName} {
			if rm := os.Remove(filepath.Join(p.out, name)); rm != nil && !errors.Is(rm, os.ErrNotExist) {
				left = append(left, name)
			}
		}
		util.SyncDir(p.out)
		if len(left) > 0 {
			return nil, fmt.Errorf("the new key file could not be written, and %s could not be removed — delete it before trying again: %w", strings.Join(left, ", "), err)
		}
		return nil, fmt.Errorf("the new key file could not be written, so none was kept — try again: %w", err)
	}
	k := p.k
	p.k, p.done = nil, true
	return k, nil
}

// mutate opens the vault at out under the archive lock with current, applies
// change, and saves the result (both copies, which also heals them). Building
// on the copy the secret opened, at a generation above every copy, also
// settles a fork.
func mutate(out string, want ID, current Secret, change func(*Opened) (*Vault, error)) error {
	if err := requireDir(out); err != nil {
		return err
	}
	l, err := lockArchive(out)
	if err != nil {
		return err
	}
	defer l.Release()
	o, err := openVault(out, want, current, nil)
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
func ChangePasswordAt(out string, want ID, current Secret, newPassword string) error {
	return mutate(out, want, current, func(o *Opened) (*Vault, error) {
		return o.Vault.WithPassword(o.Key, newPassword)
	})
}

// PendingRecovery is a new recovery key made in memory and not yet written: the
// caller shows RecoveryKey, the person confirms they saved it, and only then
// Commit replaces the old one, which keeps working until then. It prints
// redacted.
type PendingRecovery struct {
	out  string
	want ID
	key  *Key
	base *Vault
	next *Vault
	rk   RecoveryKey
	done bool
}

// PrepareNewRecovery unlocks the archive at out with current and builds the
// vault with a new recovery key, writing nothing.
func PrepareNewRecovery(out string, want ID, current Secret) (*PendingRecovery, error) {
	o, err := Open(out, want, current)
	if err != nil {
		return nil, err
	}
	next, rk, err := o.Vault.WithNewRecovery(o.Key)
	if err != nil {
		o.Key.Destroy()
		return nil, err
	}
	return &PendingRecovery{out: out, want: want, key: o.Key, base: o.Vault, next: next, rk: rk}, nil
}

// RecoveryKey is the new key to show once, before Commit.
func (p *PendingRecovery) RecoveryKey() RecoveryKey { return p.rk }

// RecoveryFile is the recovery file for the new recovery key (phase3 §9.2).
func (p *PendingRecovery) RecoveryFile() ([]byte, error) { return p.next.RecoveryFile() }

// Format prints a pending recovery key without its keys.
func (p *PendingRecovery) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "vault.PendingRecovery{archive %s, redacted}", p.base.ArchiveID)
}

// Discard forgets an uncommitted recovery key; the old one stays in force.
func (p *PendingRecovery) Discard() {
	if !p.done {
		p.key.Destroy()
	}
	p.done = true
}

// finish ends a pending recovery for good.
func (p *PendingRecovery) finish() {
	p.key.Destroy()
	p.done = true
}

// Commit replaces the recovery key under the archive lock — only while the
// vault it was prepared from is still the newest authentic one (on either side
// of a fork); otherwise ErrConflict, and the change is spent: prepare again.
// A failure that changed nothing (the lock busy, the folder absent, the lock
// lost, the primary write failed) leaves it committable: try again, the key
// shown stays the one that will be committed. ErrPartiallySaved means the new
// key IS in effect.
func (p *PendingRecovery) Commit() error {
	if p.done {
		return ErrPendingUsed
	}
	if err := requireDir(p.out); err != nil {
		return err
	}
	l, err := lockArchive(p.out)
	if err != nil {
		return err
	}
	defer l.Release()
	cs, err := readCopies(p.out, p.want)
	if err != nil {
		return err
	}
	found := false
	for _, c := range cs.parsed {
		if c.v.Verify(p.key) != nil {
			continue
		}
		if c.v.Generation > p.base.Generation {
			found = false
			break
		}
		if c.v.Generation == p.base.Generation && c.v.mac == p.base.mac {
			found = true
		}
	}
	if !found {
		p.finish()
		return fmt.Errorf("%w: the key file in %s changed since the new recovery key was made; make it again", ErrConflict, p.out)
	}
	if err := stillHeld(l); err != nil {
		return err
	}
	if err := save(p.out, p.next); err != nil {
		if errors.Is(err, ErrPartiallySaved) {
			p.finish()
		}
		return err
	}
	p.finish()
	return nil
}

// Heal rewrites both vault copies from the trusted winner, under the archive
// lock, when Open reported NeedsHeal. The generation is unchanged: healing
// restores a copy, it does not make a new vault. It refuses a fork (ErrConflict)
// rather than choose which of two people's changes to keep. An unattended run
// heals with KeySecret of the key RequireRetrievableKey returned.
func Heal(out string, want ID, current Secret) error {
	if err := requireDir(out); err != nil {
		return err
	}
	l, err := lockArchive(out)
	if err != nil {
		return err
	}
	defer l.Release()
	o, err := openVault(out, want, current, nil)
	if err != nil {
		return err
	}
	defer o.Key.Destroy()
	if o.Forked {
		return fmt.Errorf("%w: %s and %s in %s are two different key files at generation %d (changed on two machines at once); change the password, or make a new recovery key, from the side to keep",
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
