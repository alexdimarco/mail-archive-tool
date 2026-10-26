package export

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// UnitsDir is the one content directory of an encrypted archive: every sealed
// unit lives at units/<first two hex of its name>/<name>, and no folder tree of
// the mailbox exists on disk (design-encrypted-archives-phase2a §1, §3).
const UnitsDir = "units"

// ErrUnitStore refuses a unit path an attacker could have planted (a link or a
// special file where a directory or unit belongs), and a unit whose bytes are
// not the ones its record names (a swapped-in older revision, truncation).
var ErrUnitStore = errors.New("the encrypted archive's unit store is damaged")

// ErrUnitExists refuses a unit write whose new revision's name is already taken
// (phase2b P2b-C7): the record is older than the units beside it.
var ErrUnitExists = errors.New("the encrypted archive's record is older than its units")

// Units writes and reads an encrypted archive's sealed units. A nil *Units on
// the Exporter means a plaintext archive, whose every path is left as it was.
type Units struct {
	Root     string     // the archive root (-out)
	Key      *vault.Key // the archive's unlocked key
	RunStart time.Time  // every unit written this run carries this mtime (P2a-C19)
	// OrphanAfter is the start of the run that made the sealed record's last commit:
	// a taken unit name whose file a run since then wrote is that run's crash orphan,
	// replaced; an older one is evidence the record is older than its units, refused
	// (phase2b, the adversarial pass's CON-4).
	OrphanAfter time.Time
}

// newUnitID draws a record's unit id: 128 random bits, lowercase hex.
func newUnitID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", fmt.Errorf("draw a unit id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ref is a unit revision's reference: what its AAD binds and its name derives
// from, so an older revision never opens as the current one (P2a-C1).
func ref(unit string, rev uint32) string { return unit + "." + strconv.FormatUint(uint64(rev), 10) }

// rel is the archive-relative path of the unit of kind at revision rev.
func (u *Units) rel(unit string, kind vault.UnitKind, rev uint32) (string, error) {
	if !state.ValidUnitID(unit) || rev == 0 {
		return "", fmt.Errorf("%w: a unit id or revision is malformed", ErrUnitStore)
	}
	name, err := u.Key.UnitName(kind, ref(unit, rev))
	if err != nil {
		return "", err
	}
	return filepath.Join(UnitsDir, name[:2], name), nil
}

// realDir refuses a path that exists but is not a real directory (a planted
// link or file); absent is fine.
func realDir(path string) (exists bool, err error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return false, fmt.Errorf("%w: %s is %s, not a folder", ErrUnitStore, path, util.ModeKind(fi.Mode()))
	}
	return true, nil
}

// ensureDir makes dir (one level) if absent, refusing a planted non-directory,
// and fsyncs its parent so the new entry survives power loss before any manifest
// save names a unit inside it (P2a-C5b).
func ensureDir(dir string) error {
	exists, err := realDir(dir)
	if err != nil || exists {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if _, err := realDir(dir); err != nil {
		return err
	}
	syncDir(filepath.Dir(dir))
	return nil
}

// Write seals plaintext as the unit of kind at revision rev and places it under
// its opaque name, atomically and durably. It returns what the record keeps.
func (u *Units) Write(unit string, kind vault.UnitKind, rev uint32, plaintext []byte) (state.SealedUnit, error) {
	rel, err := u.rel(unit, kind, rev)
	if err != nil {
		return state.SealedUnit{}, err
	}
	sealed, err := u.Key.SealUnit(kind, ref(unit, rev), plaintext)
	if err != nil {
		return state.SealedUnit{}, err
	}
	if err := ensureDir(filepath.Join(u.Root, UnitsDir)); err != nil {
		return state.SealedUnit{}, err
	}
	shard := filepath.Join(u.Root, filepath.Dir(rel))
	if err := ensureDir(shard); err != nil {
		return state.SealedUnit{}, err
	}
	dst := filepath.Join(u.Root, rel)
	// Every write is a new revision, so its name is never already taken — unless a run
	// since the record's last commit crashed after writing it (its orphan, which the
	// start sweep may not have removed: no sweep ran, or a clock moved back), or the
	// record was restored from an older copy and a later run's revision is still here:
	// keyed evidence of that, and overwriting it would lose that later message's bytes
	// (phase2b §3, P2b-C7).
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode().IsRegular() && !u.OrphanAfter.IsZero() && !fi.ModTime().Before(u.OrphanAfter) {
			if err := os.Remove(dst); err != nil {
				return state.SealedUnit{}, err
			}
		} else {
			return state.SealedUnit{}, fmt.Errorf("%w: %s already exists — the archive's record looks older than its units (a restored older copy?); nothing was overwritten", ErrUnitExists, rel)
		}
	}
	tmp, err := createTemp(shard)
	if err != nil {
		return state.SealedUnit{}, err
	}
	if _, err := tmp.Write(sealed); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return state.SealedUnit{}, err
	}
	if err := commitTemp(tmp, dst); err != nil {
		return state.SealedUnit{}, err
	}
	if !u.RunStart.IsZero() {
		_ = os.Chtimes(dst, u.RunStart, u.RunStart) // write order, not wall time, is what leaks (P2a-C19)
	}
	sum := sha256.Sum256(sealed)
	return state.SealedUnit{Rev: rev, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(sealed))}, nil
}

// open opens the unit file at rel after judging the path and its directories
// without following a link (on Windows the open itself would follow one).
func (u *Units) open(rel string) (*os.File, fs.FileInfo, error) {
	for _, d := range []string{UnitsDir, filepath.Dir(rel)} {
		if _, err := realDir(filepath.Join(u.Root, d)); err != nil {
			return nil, nil, err
		}
	}
	path := filepath.Join(u.Root, rel)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !util.DescriptorReadable(runtime.GOOS, fi.Mode()) {
		return nil, nil, fmt.Errorf("%w: %s is %s, not a unit file", ErrUnitStore, rel, util.ModeKind(fi.Mode()))
	}
	f, err := util.OpenRegularFile(path)
	if err != nil {
		return nil, nil, err
	}
	return f, fi, nil
}

// Read opens the unit of kind that s records and returns its plaintext. It reads
// at most s.Size bytes, and checks the ciphertext against s's digest before it
// decrypts, so a planted, truncated or older revision is refused (A2) — then the
// AEAD tag proves the rest.
func (u *Units) Read(unit string, kind vault.UnitKind, s state.SealedUnit) ([]byte, error) {
	sealed, err := u.readSealed(unit, kind, s)
	if err != nil {
		return nil, err
	}
	return u.Key.OpenUnit(kind, ref(unit, s.Rev), sealed)
}

// The reader's split read (design-encrypted-archives-phase3 §3.2): the key is held
// only to name a unit (Locate) and to open it (Open), never while its ciphertext is
// read from disk (ReadSealedAt), so a slow drive never keeps a locked key alive.

// Locate is the archive-relative path of the unit of kind that s records.
func (u *Units) Locate(unit string, kind vault.UnitKind, s state.SealedUnit) (string, error) {
	return u.rel(unit, kind, s.Rev)
}

// ReadSealedAt is the ciphertext of the unit at rel — as Locate returned it — under
// root, read at most s.Size bytes and checked against s's size and digest: no key
// needed. A unit that is gone keeps fs.ErrNotExist; a rel outside the unit store is
// refused.
func ReadSealedAt(root, rel string, s state.SealedUnit) ([]byte, error) {
	clean := filepath.Clean(rel)
	if !filepath.IsLocal(clean) || !strings.HasPrefix(filepath.ToSlash(clean), UnitsDir+"/") {
		return nil, fmt.Errorf("%w: %s is not a unit path", ErrUnitStore, rel)
	}
	return (&Units{Root: root}).readSealedAt(clean, s)
}

// Open decrypts the ciphertext of the unit of kind that s records, read by
// ReadSealedAt.
func (u *Units) Open(unit string, kind vault.UnitKind, s state.SealedUnit, sealed []byte) ([]byte, error) {
	return u.Key.OpenUnit(kind, ref(unit, s.Rev), sealed)
}

// readSealed is the unit's ciphertext, read at most s.Size bytes and checked
// against s's size and digest.
func (u *Units) readSealed(unit string, kind vault.UnitKind, s state.SealedUnit) ([]byte, error) {
	rel, err := u.rel(unit, kind, s.Rev)
	if err != nil {
		return nil, err
	}
	return u.readSealedAt(rel, s)
}

func (u *Units) readSealedAt(rel string, s state.SealedUnit) ([]byte, error) {
	f, _, err := u.open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sealed, err := io.ReadAll(io.LimitReader(f, s.Size+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(sealed)
	if int64(len(sealed)) != s.Size || hex.EncodeToString(sum[:]) != s.SHA256 {
		return nil, fmt.Errorf("%w: %s is not the unit its record names (size or digest differs)", ErrUnitStore, rel)
	}
	return sealed, nil
}

// Present reports whether the unit s records is on disk as a regular file of its
// recorded size (a cheap check; Read proves the bytes). A unit that cannot be
// looked at reads as not present: callers that would DELETE on "not present" use
// Check, which tells "gone" from "cannot tell".
func (u *Units) Present(unit string, kind vault.UnitKind, s state.SealedUnit) bool {
	present, err := u.Check(unit, kind, s)
	return present && err == nil
}

// Check tells a unit that is on disk at its recorded size (true, nil) from one that
// is confirmed gone (false, nil) and one that cannot be looked at — a sharing
// violation, a permission, an I/O error (false, err): only the second is gone.
func (u *Units) Check(unit string, kind vault.UnitKind, s state.SealedUnit) (bool, error) {
	rel, err := u.rel(unit, kind, s.Rev)
	if err != nil {
		return false, err
	}
	f, fi, err := u.open(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	f.Close()
	return fi.Size() == s.Size, nil
}

// Intact reports whether the unit s records is on disk with exactly its recorded
// size and ciphertext digest: what a re-export may keep instead of rewriting.
func (u *Units) Intact(unit string, kind vault.UnitKind, s state.SealedUnit) bool {
	_, err := u.readSealed(unit, kind, s)
	return err == nil
}

// Remove deletes the unit of kind at revision rev. A unit already gone is not
// an error.
func (u *Units) Remove(unit string, kind vault.UnitKind, rev uint32) error {
	rel, err := u.rel(unit, kind, rev)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(u.Root, rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// SweepUnits is the keyed inventory sweep (A5, P2a-C4): run under the archive
// lock after a successful manifest load and before the first write, it removes
// the files under units/ that no record of m names and that may allows (the sealed
// record's rule, phase2b §3: never one its inventory lists live; one it lists
// retained; otherwise only one a run since its last commit wrote) — a crashed
// run's new revisions, or ones superseded during the last run — when they are
// older than before (this run's start). A nil may allows every unnamed file. It
// returns how many it removed. units/ or a shard that is not a real directory is
// refused, never walked.
func (u *Units) SweepUnits(m *state.Manifest, before time.Time, may func(rel string, mtime time.Time) bool) (int, error) {
	root := filepath.Join(u.Root, UnitsDir)
	if exists, err := realDir(root); err != nil || !exists {
		return 0, err
	}
	// Every name the manifest's records name, or no sweep at all: a name that cannot
	// be derived (an unusable key) must never make every unit look unnamed.
	named := map[string]bool{}
	for _, r := range m.All() {
		if r.Sealed == nil {
			continue
		}
		for kind, s := range map[vault.UnitKind]*state.SealedUnit{vault.UnitMessage: r.Sealed.HTML, vault.UnitAttachment: r.Sealed.Zip, vault.UnitOriginal: r.Sealed.EML} {
			if s == nil {
				continue
			}
			rel, err := u.rel(r.Unit, kind, s.Rev)
			if err != nil {
				return 0, fmt.Errorf("%w: a record's unit name cannot be derived, so nothing is swept: %w", ErrUnitStore, err)
			}
			named[filepath.ToSlash(rel)] = true
		}
	}
	shards, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, sh := range shards {
		// A plain file beside the shards (a .DS_Store, a desktop.ini) cannot redirect
		// anything: it is left alone. A link or a special file is refused.
		if sh.Type().IsRegular() {
			continue
		}
		dir := filepath.Join(root, sh.Name())
		if _, err := realDir(dir); err != nil {
			return removed, err
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return removed, err
		}
		for _, f := range files {
			rel := UnitsDir + "/" + sh.Name() + "/" + f.Name()
			info, err := f.Info()
			if named[rel] || err != nil || !info.ModTime().Before(before) || !info.Mode().IsRegular() || (may != nil && !may(rel, info.ModTime())) {
				continue
			}
			if os.Remove(filepath.Join(dir, f.Name())) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// HasUnitFiles reports whether units/ holds any unit file (a 32-hex name in a shard
// named by its first two): an encrypted archive's content is there even when its
// manifest is missing. A stray file (a .DS_Store) is not a unit.
func (u *Units) HasUnitFiles() bool {
	found := false
	root := filepath.Join(u.Root, UnitsDir)
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if name := d.Name(); len(name) == 32 && filepath.Base(filepath.Dir(p)) == name[:2] && filepath.Dir(filepath.Dir(p)) == root {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// Path is the archive-relative, slash-separated path of the unit of kind at
// revision rev: what the sealed record's inventory lists (phase2b §3).
func (u *Units) Path(unit string, kind vault.UnitKind, rev uint32) (string, error) {
	rel, err := u.rel(unit, kind, rev)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// Name is the archive-relative path of the unit of kind at revision rev — what
// logs and errors name instead of a subject (P2a-C16).
func (u *Units) Name(unit string, kind vault.UnitKind, rev uint32) string {
	rel, err := u.rel(unit, kind, rev)
	if err != nil {
		return UnitsDir + "/(invalid)"
	}
	return filepath.ToSlash(rel)
}
