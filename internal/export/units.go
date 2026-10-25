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

// Units writes and reads an encrypted archive's sealed units. A nil *Units on
// the Exporter means a plaintext archive, whose every path is left as it was.
type Units struct {
	Root     string     // the archive root (-out)
	Key      *vault.Key // the archive's unlocked key
	RunStart time.Time  // every unit written this run carries this mtime (P2a-C19)
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
	rel, err := u.rel(unit, kind, s.Rev)
	if err != nil {
		return nil, err
	}
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
	return u.Key.OpenUnit(kind, ref(unit, s.Rev), sealed)
}

// Present reports whether the unit s records is on disk as a regular file of its
// recorded size (a cheap check; Read proves the bytes).
func (u *Units) Present(unit string, kind vault.UnitKind, s state.SealedUnit) bool {
	rel, err := u.rel(unit, kind, s.Rev)
	if err != nil {
		return false
	}
	f, fi, err := u.open(rel)
	if err != nil {
		return false
	}
	f.Close()
	return fi.Size() == s.Size
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
// the files under units/ that no record of m names — a crashed run's new
// revisions, or superseded ones whose deletion never ran — when they are older
// than before (this run's start). It returns how many it removed. units/ or a
// shard that is not a real directory is refused, never walked.
func (u *Units) SweepUnits(m *state.Manifest, before time.Time) (int, error) {
	root := filepath.Join(u.Root, UnitsDir)
	if exists, err := realDir(root); err != nil || !exists {
		return 0, err
	}
	named := map[string]bool{}
	for _, r := range m.All() {
		if r.Sealed == nil {
			continue
		}
		for kind, s := range map[vault.UnitKind]*state.SealedUnit{vault.UnitMessage: r.Sealed.HTML, vault.UnitAttachment: r.Sealed.Zip, vault.UnitOriginal: r.Sealed.EML} {
			if s != nil {
				named[u.Name(r.Unit, kind, s.Rev)] = true
			}
		}
	}
	shards, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, sh := range shards {
		dir := filepath.Join(root, sh.Name())
		if _, err := realDir(dir); err != nil {
			return removed, err
		}
		if !sh.IsDir() {
			continue
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return removed, err
		}
		for _, f := range files {
			rel := UnitsDir + "/" + sh.Name() + "/" + f.Name()
			info, err := f.Info()
			if named[rel] || err != nil || !info.ModTime().Before(before) || !info.Mode().IsRegular() {
				continue
			}
			if os.Remove(filepath.Join(dir, f.Name())) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// HasUnitFiles reports whether units/ holds any file: an encrypted archive's
// content is there even when its manifest is missing.
func (u *Units) HasUnitFiles() bool {
	found := false
	filepath.WalkDir(filepath.Join(u.Root, UnitsDir), func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
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
