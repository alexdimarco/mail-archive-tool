package vault

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

// The recovery file and putting a key file back (design-encrypted-archives-phase3
// §9.4, §9.5, A28): the one change Phase 3 makes to Phase 1's frozen API, with
// OpenWithin (§3.1 K14, the P3-1a pass's OUT-04).

// RecoveryFileFormat is the recovery file's own format version.
const RecoveryFileFormat = 1

// recoveryFileKind names the file, so another JSON file is never taken for one.
const recoveryFileKind = "mailarchive-recovery-file"

// maxRecoveryFile bounds a recovery file read.
const maxRecoveryFile = 4 << 10

// maxGeneration is the cap below which every generation stays: a restore's
// time-based generation, and every rewrite, refuse to pass it.
const maxGeneration = 1 << 62

// releaseFloor is this build's date: a restore refuses a clock that reads earlier,
// since its generation is the time (§9.4).
var releaseFloor = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)

// clock is the time a restore stamps; a var so a test can set it.
var clock = time.Now

var (
	// ErrRecoveryFile refuses text that is not a recovery file this build reads.
	ErrRecoveryFile = errors.New("vault: not a MailArchive recovery file")
	// ErrKeyFileOpens refuses a restore while a copy of the key file still opens:
	// nothing needs putting back.
	ErrKeyFileOpens = errors.New("vault: a copy of this archive's key file still opens with this key — nothing needs putting back")
	// ErrNotProven refuses a restore whose key this folder's sealed record does not
	// prove (another archive's key, or a folder with no record this computer
	// recorded as the archive's).
	ErrNotProven = errors.New("vault: this folder's sealed record does not prove the key belongs to it")
	// ErrClock refuses a restore on a clock that reads earlier than this build.
	ErrClock = errors.New("vault: this computer's clock is wrong")
	// ErrKeyFileUnreadable refuses a restore when a copy of the key file cannot be
	// read: that is not damage, and nothing is put over it.
	ErrKeyFileUnreadable = errors.New("vault: the key file could not be read — check the drive")
)

// RecoveryFile is the recovery entry of a vault, as text (O6, K12, A28): the
// archive id and the recovery-wrapped master key with its salt and cost. It opens
// nothing without the recovery key, and it holds no password entry.
func (v *Vault) RecoveryFile() ([]byte, error) {
	b, err := json.MarshalIndent(struct {
		Format    int       `json:"format"`
		Kind      string    `json:"kind"`
		ArchiveID string    `json:"archive_id"`
		Entry     entryFile `json:"entry"`
	}{RecoveryFileFormat, recoveryFileKind, v.ArchiveID.String(), v.Recovery.file(entryRecovery)}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// RecoveryEntry is a parsed recovery file. It trusts nothing: the recovery key
// unwraps it, and the archive's sealed record proves the key (PrepareRestore).
type RecoveryEntry struct {
	ArchiveID ID
	entry     Entry
}

// ParseRecoveryFile reads a recovery file's text strictly.
func ParseRecoveryFile(data []byte) (RecoveryEntry, error) {
	var re RecoveryEntry
	if len(data) > maxRecoveryFile {
		return re, fmt.Errorf("%w: it is larger than %d bytes", ErrRecoveryFile, maxRecoveryFile)
	}
	var probe struct {
		Format int `json:"format"`
	}
	if json.NewDecoder(bytes.NewReader(data)).Decode(&probe) == nil && probe.Format > RecoveryFileFormat {
		return re, fmt.Errorf("%w (recovery file format %d; this build reads %d): upgrade MailArchive", ErrNewerVault, probe.Format, RecoveryFileFormat)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f struct {
		Format    int       `json:"format"`
		Kind      string    `json:"kind"`
		ArchiveID string    `json:"archive_id"`
		Entry     entryFile `json:"entry"`
	}
	if err := dec.Decode(&f); err != nil {
		return re, fmt.Errorf("%w: %v", ErrRecoveryFile, err)
	}
	if dec.More() || f.Format != RecoveryFileFormat || f.Kind != recoveryFileKind {
		return re, ErrRecoveryFile
	}
	id, err := parseID(f.ArchiveID)
	if err != nil || id == (ID{}) {
		return re, fmt.Errorf("%w: its archive id", ErrRecoveryFile)
	}
	kind, e, err := f.Entry.parse()
	if err != nil || kind != entryRecovery {
		return re, fmt.Errorf("%w: its recovery entry", ErrRecoveryFile)
	}
	return RecoveryEntry{ArchiveID: id, entry: e}, nil
}

// RestoreSource is where a restore gets the master key: a recovery file with its
// recovery key (§9.4), or the key this computer remembers (§9.5).
type RestoreSource interface {
	restoreKey() (*Key, error)
}

// RecoveryEntrySource unwraps a recovery file's entry with its recovery key.
type RecoveryEntrySource struct {
	Entry       RecoveryEntry
	RecoveryKey string
}

func (s RecoveryEntrySource) restoreKey() (*Key, error) {
	rk, err := ParseRecoveryKey(s.RecoveryKey)
	if err != nil {
		return nil, err
	}
	kek, err := deriveKEK(rk[:], s.Entry.entry.Salt[:], s.Entry.entry.Params)
	if err != nil {
		return nil, err
	}
	defer clear(kek)
	master, err := open(kek, wrapAAD(s.Entry.ArchiveID, entryRecovery), s.Entry.entry.Wrapped)
	if err != nil {
		return nil, ErrWrongSecret
	}
	defer clear(master)
	return newKey(s.Entry.ArchiveID, master)
}

// RememberedSource loads the key remembered for Claimed from Store, inside the
// vault, so no key leaves before the proof (§9.5, K15).
type RememberedSource struct {
	Store   Store
	Claimed ID
}

func (s RememberedSource) restoreKey() (*Key, error) {
	stored, err := s.Store.Get(Account(s.Claimed))
	if err != nil {
		return nil, fmt.Errorf("%w: no key is remembered for archive %s: %w", ErrKeyNotRetrievable, s.Claimed, err)
	}
	master, err := base64.StdEncoding.DecodeString(stored)
	defer clear(master)
	if err != nil || len(master) != keySize {
		return nil, fmt.Errorf("%w: the remembered key is damaged", ErrKeyNotRetrievable)
	}
	return newKey(s.Claimed, master)
}

// restoreState is what a restore judged on disk: both key-file copies and both
// record pointers as read, byte for byte, and the highest generation any copy
// claims.
type restoreState struct {
	copies   [2][]byte
	pointers [2][]byte
	highest  uint64
}

func (a restoreState) same(b restoreState) bool {
	for i := range a.copies {
		if !bytes.Equal(a.copies[i], b.copies[i]) || !bytes.Equal(a.pointers[i], b.pointers[i]) {
			return false
		}
	}
	return true
}

// pointerNames are the sealed record's root pointer and its mirror, and
// pointerMagic its frozen header ("MAPF" ‖ format u16 ‖ generation u64), read here
// because container imports vault, not the reverse; a container test pins them.
var pointerNames = [2]string{".mailarchive-manifest.ptr", ".mailarchive-manifest.ptr.bak"}

const (
	pointerMagic    = "MAPF"
	pointerHeadSize = 4 + 2 + 8
	maxPointerRead  = 64 << 20
)

// judgeRestore reads the folder and refuses what a restore must not go over
// (§9.4): a copy from a newer MailArchive, a copy naming another archive, a copy
// that cannot be read, a copy that still opens under k, and a key the sealed
// record does not prove — unless no record exists and want, the id this computer
// recorded for a proven path, is k's.
func judgeRestore(out string, want ID, k *Key) (restoreState, error) {
	var st restoreState
	for i, name := range []string{FileName, BackupName} {
		c := readCopy(out, name)
		st.copies[i] = c.raw
		switch {
		case c.err == nil:
		case errors.Is(c.err, ErrNewerVault):
			return st, fmt.Errorf("%s in %s: %w", name, out, c.err)
		case errors.Is(c.err, fs.ErrNotExist), errors.Is(c.err, ErrCorrupt):
		default:
			return st, fmt.Errorf("%w: %s in %s: %v", ErrKeyFileUnreadable, name, out, c.err)
		}
		id, claims := claimedID(c.raw)
		if c.v != nil {
			id, claims = c.v.ArchiveID, true
		}
		if claims && id != k.id {
			return st, fmt.Errorf("%w: in %s, %s belongs to archive %s, not %s", ErrTampered, out, name, id, k.id)
		}
		if c.v != nil {
			if c.v.Verify(k) == nil {
				return st, fmt.Errorf("%w (%s in %s) — open the archive with its password or recovery key instead", ErrKeyFileOpens, name, out)
			}
		}
		st.highest = max(st.highest, claimedGeneration(c.raw))
	}
	present, proven := false, false
	for i, name := range pointerNames {
		raw, err := util.ReadRegularFile(filepath.Join(out, name), maxPointerRead)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return st, fmt.Errorf("%w: %s in %s: %v", ErrKeyFileUnreadable, name, out, err)
		}
		st.pointers[i] = raw
		present = true
		if len(raw) < pointerHeadSize || string(raw[:4]) != pointerMagic {
			continue
		}
		if f := binary.BigEndian.Uint16(raw[4:6]); f > FormatVersion {
			return st, fmt.Errorf("%w: the archive's record in %s was written by a newer MailArchive", ErrNewerVault, out)
		}
		gen := binary.BigEndian.Uint64(raw[6:14])
		if _, err := k.OpenPointer(ContainerManifest, gen, raw[pointerHeadSize:]); err == nil {
			proven = true
		}
	}
	switch {
	case proven:
	case present:
		return st, fmt.Errorf("%w: %s's record does not open with this key — it is another archive's, or the record was altered", ErrNotProven, out)
	case want == (ID{}) || want != k.id:
		return st, fmt.Errorf("%w: %s holds no sealed record to prove the key against, and this computer did not record it as this archive's folder", ErrNotProven, out)
	}
	return st, nil
}

// claimedGeneration is the generation a copy's bytes claim, parsed or not.
func claimedGeneration(raw []byte) uint64 {
	var probe struct {
		Generation uint64 `json:"generation"`
	}
	if json.NewDecoder(bytes.NewReader(raw)).Decode(&probe) != nil {
		return 0
	}
	return probe.Generation
}

// PendingRestore is a key file to put back, made in memory and not written: a new
// password and a new recovery key over the proven master key, at a generation
// that outranks every copy on disk (§9.4). The new recovery key is shown and
// entered again before Commit. It prints redacted.
type PendingRestore struct {
	out   string
	want  ID
	key   *Key
	next  *Vault
	rk    RecoveryKey
	state restoreState
	done  bool
}

// PrepareRestore proves the master key from src against out's sealed record and
// prepares a fresh key file with newPassword and a new recovery key (§9.4, §9.5).
// want is the id this computer recorded for out when the path is recorded and
// proven, zero otherwise; it is the only way a folder with no sealed record is
// accepted. Nothing is written.
func PrepareRestore(out string, want ID, src RestoreSource, newPassword string) (*PendingRestore, error) {
	if err := checkNewPassword(newPassword); err != nil {
		return nil, err
	}
	if err := requireDir(out); err != nil {
		return nil, err
	}
	now := clock()
	if now.Before(releaseFloor) {
		return nil, fmt.Errorf("%w: it reads %s, before this MailArchive was made (%s) — set the clock, then try again", ErrClock, now.UTC().Format(time.DateOnly), releaseFloor.Format(time.DateOnly))
	}
	k, err := src.restoreKey()
	if err != nil {
		return nil, err
	}
	if want != (ID{}) && k.id != want {
		k.Destroy()
		return nil, fmt.Errorf("%w: the key is archive %s's, and this computer recorded %s here", ErrTampered, k.id, want)
	}
	st, err := judgeRestore(out, want, k)
	if err != nil {
		k.Destroy()
		return nil, err
	}
	gen := max(uint64(now.Unix()), st.highest+1)
	if gen >= maxGeneration {
		k.Destroy()
		return nil, fmt.Errorf("%w: a copy of the key file claims generation %d, past the limit", ErrCorrupt, st.highest)
	}
	rk, err := newRecoveryKey()
	if err != nil {
		k.Destroy()
		return nil, err
	}
	n := &Vault{ArchiveID: k.id, Generation: gen}
	if n.Password, err = wrap(k.id, entryPassword, passwordBytes(newPassword), k.master[:]); err == nil {
		n.Recovery, err = wrap(k.id, entryRecovery, rk[:], k.master[:])
	}
	if err != nil {
		k.Destroy()
		return nil, err
	}
	n.mac = n.computeMAC(k)
	return &PendingRestore{out: out, want: want, key: k, next: n, rk: rk, state: st}, nil
}

// RecoveryKey is the new recovery key to show once, before Commit.
func (p *PendingRestore) RecoveryKey() RecoveryKey { return p.rk }

// RecoveryFile is the new recovery file, offered with the new recovery key.
func (p *PendingRestore) RecoveryFile() ([]byte, error) { return p.next.RecoveryFile() }

// ArchiveID is the archive being restored.
func (p *PendingRestore) ArchiveID() ID { return p.key.id }

// Format prints a pending restore without its keys.
func (p *PendingRestore) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "vault.PendingRestore{archive %s, redacted}", p.key.id)
}

// Discard forgets the prepared key file.
func (p *PendingRestore) Discard() {
	if !p.done {
		p.key.Destroy()
	}
	p.done = true
}

// Commit writes the key file under the archive lock, only if the folder — both
// key-file copies and both record pointers — is byte for byte as it was prepared;
// otherwise ErrConflict and the restore is spent. It then reads back what it
// wrote and proves it opens. ErrPartiallySaved means the restore IS in effect.
func (p *PendingRestore) Commit() error {
	if p.done {
		return ErrPendingUsed
	}
	if err := requireDir(p.out); err != nil {
		return err
	}
	l, err := lockArchive(p.out, "restore-key-file")
	if err != nil {
		return err
	}
	defer l.Release()
	now, err := judgeRestore(p.out, p.want, p.key)
	if err != nil || !now.same(p.state) {
		p.Discard()
		if err != nil {
			return fmt.Errorf("%w: the folder changed since the key file was prepared: %w", ErrConflict, err)
		}
		return fmt.Errorf("%w: the folder %s changed since the key file was prepared; prepare it again", ErrConflict, p.out)
	}
	if err := stillHeld(l); err != nil {
		return err
	}
	if err := save(p.out, p.next); err != nil {
		if errors.Is(err, ErrPartiallySaved) {
			p.Discard()
		}
		return err
	}
	defer p.Discard()
	o, err := openVault(p.out, p.key.id, KeySecret(p.key), nil)
	if err != nil {
		return fmt.Errorf("vault: the key file was written but does not read back: %w", err)
	}
	defer o.Key.Destroy()
	if o.Vault.Generation != p.next.Generation {
		return fmt.Errorf("vault: the key file was written but reads back at generation %d, not %d", o.Vault.Generation, p.next.Generation)
	}
	return nil
}

// CostError refuses, before deriving, a key file whose entry for the secret asks
// for more than an open's limit (OpenWithin). Params is the cost it asks.
type CostError struct{ Params Params }

func (e *CostError) Error() string {
	return fmt.Sprintf("vault: this key file asks for %s to open, above the limit allowed without asking", e.Params)
}

// OpenWithin is Open, refusing with *CostError before any derivation a copy whose
// entry for s asks for more than limit — judged on the very bytes it would derive
// from (the P3-1a pass, OUT-04).
func OpenWithin(out string, want ID, s Secret, limit Params) (*Opened, error) {
	return openVault(out, want, s, &limit)
}

// above reports whether p exceeds limit in any dimension.
func (p Params) above(limit Params) bool {
	return p.MemoryKiB > limit.MemoryKiB || p.Time > limit.Time || p.Threads > limit.Threads
}
