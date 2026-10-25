package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// encryptedOut is a fresh archive folder holding a committed vault, and its key.
func encryptedOut(t *testing.T) (string, *vault.Key) {
	t.Helper()
	out := tmpDir(t)
	p, err := vault.PrepareCreate(out, "encrypted run test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	return out, k
}

// loadSealed opens out's sealed record with key (Phase 2b-1): the manifest an
// encrypted archive keeps, read the way a keyed run reads it. The load writes
// nothing; a Save through it commits like a run's.
func loadSealed(t *testing.T, out string, key *vault.Key) *state.Manifest {
	t.Helper()
	units := &export.Units{Root: out, Key: key}
	m, _, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(units)})
	if err != nil {
		t.Fatalf("load the sealed record: %v", err)
	}
	return m
}

// encryptedSourcePST writes the PST fixture into a fresh source folder.
func encryptedSourcePST(t *testing.T) string {
	t.Helper()
	return writeData(t, tmpDir(t), "support.pst", pstFixtureData(t))
}

// treeOf lists the archive-relative paths of every file under out.
func treeOf(t *testing.T, out string) []string {
	t.Helper()
	var paths []string
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(out, p)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return paths
}

// snapshotTree is every file under out with its bytes, and every folder below out as
// "<path>/" with nil bytes, so a created or removed folder — even an empty one — is a
// change sameTree sees.
func snapshotTree(t *testing.T, out string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == out {
			return nil
		}
		rel, _ := filepath.Rel(out, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			snap[rel+"/"] = nil
			return nil
		}
		b, _ := os.ReadFile(p)
		snap[rel] = b
		return nil
	})
	return snap
}

func sameTree(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || !bytes.Equal(v, w) {
			return false
		}
	}
	return true
}

// holderOnly is the lock file's holder line with no verb= field: an encrypted
// archive's lock names no source kind (A18, MA-735).
var holderOnly = regexp.MustCompile(`^pid=[0-9]+ started=\S+ host=\S* nonce=[0-9a-f]{32}\n$`)

// requireHolderLineOnly fails unless out's lock file, if present, is a bare holder
// line: an under-lock refusal may rewrite only that line, never add a verb.
func requireHolderLineOnly(t *testing.T, out string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, lockfile.Name))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil || !holderOnly.Match(b) {
		t.Fatalf("the lock file after the refusal is not a bare holder line (%v): %q", err, b)
	}
}

// withoutLock is snap less the lock file, whose holder line an under-lock refusal
// rewrites (checked by requireHolderLineOnly instead).
func withoutLock(snap map[string][]byte) map[string][]byte {
	delete(snap, lockfile.Name)
	return snap
}

// covers: MA-725, R37
func TestAnEncryptedLocalRunWritesOnlyUnitsAndIsIncremental(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil)
	if err != nil {
		t.Fatalf("an encrypted run over a healthy PST: %v", err)
	}
	m := loadSealed(t, out, key)
	recs := assure.Reached(t, m.All(), "archived records")
	for k, r := range recs {
		if !state.ValidUnitID(r.Unit) || r.Sealed == nil || r.Sealed.HTML == nil {
			t.Fatalf("record %q is not sealed: %+v", k, r)
		}
	}
	for _, p := range treeOf(t, out) {
		top := strings.SplitN(p, "/", 2)[0]
		if strings.Contains(p, "/") && top != export.UnitsDir && top != container.ChunksDir && top != container.InventoryDir {
			t.Fatalf("%s: an encrypted run created the folder %q — no mailbox tree may reach disk", p, top)
		}
	}
	units := &export.Units{Root: out, Key: key}
	for _, r := range recs {
		if _, err := units.Read(r.Unit, vault.UnitMessage, *r.Sealed.HTML); err != nil {
			t.Fatalf("a sealed page must open: %v", err)
		}
	}

	before := snapshotTree(t, filepath.Join(out, export.UnitsDir))
	res2, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil)
	if err != nil {
		t.Fatalf("an incremental re-run: %v", err)
	}
	if res2.Stats.Exported != 0 || res.Stats.Exported == 0 {
		t.Fatalf("first run exported %d, the re-run %d: an unchanged source re-exports nothing (R2)", res.Stats.Exported, res2.Stats.Exported)
	}
	if !sameTree(before, snapshotTree(t, filepath.Join(out, export.UnitsDir))) {
		t.Fatal("an incremental re-run over an unchanged source must leave every unit as it was")
	}
}

// covers: MA-725, R37
func TestAnEncryptedRunRefusesWhatItCannotWriteSafely(t *testing.T) {
	src := encryptedSourcePST(t)
	_, otherKey := encryptedOut(t)

	// Each case returns the folder whose every byte the refusal must leave as it was
	// (P2a-C10: a manual refusal writes nothing). Cases checked before the lock leave
	// even the lock file alone; those only the manifest can show are checked under the
	// lock, and rewrite only the lock file's holder line (A18).
	underLock := map[string]bool{"an existing plaintext archive": true, "a missing record beside sealed units": true}
	cases := map[string]func(t *testing.T) (string, Options){
		"another archive's key": func(t *testing.T) (string, Options) {
			out, _ := encryptedOut(t)
			return out, Options{Inputs: []string{src}, Out: out, Key: otherKey}
		},
		"a folder with no vault": func(t *testing.T) (string, Options) {
			out := tmpDir(t)
			return out, Options{Inputs: []string{src}, Out: out, Key: otherKey}
		},
		"an out inside another encrypted archive": func(t *testing.T) (string, Options) {
			// The inner folder is an encrypted archive of its own, whose key opens it:
			// only the walk above out refuses it.
			outer, k := encryptedOut(t)
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: outer, Key: k}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			inner := filepath.Join(outer, "inner")
			if err := os.Mkdir(inner, 0o700); err != nil {
				t.Fatal(err)
			}
			p, err := vault.PrepareCreate(inner, "encrypted run test passphrase")
			if err != nil {
				t.Fatal(err)
			}
			innerKey, err := p.Commit()
			if err != nil {
				t.Fatal(err)
			}
			return outer, Options{Inputs: []string{src}, Out: inner, Key: innerKey}
		},
		"an existing plaintext archive": func(t *testing.T) (string, Options) {
			out := tmpDir(t)
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			p, err := vault.PrepareCreate(out, "encrypted run test passphrase")
			if err != nil {
				t.Fatal(err)
			}
			k, err := p.Commit()
			if err != nil {
				t.Fatal(err)
			}
			return out, Options{Inputs: []string{src}, Out: out, Key: k}
		},
		// The record is its pointer pair (Phase 2b-1): an envelope alone missing is
		// healed, but sealed units and chunks with no pointer are refused.
		"a missing record beside sealed units": func(t *testing.T) (string, Options) {
			out, k := encryptedOut(t)
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{container.PointerName, container.MirrorName} {
				if err := os.Remove(filepath.Join(out, name)); err != nil {
					t.Fatal(err)
				}
			}
			return out, Options{Inputs: []string{src}, Out: out, Key: k}
		},
		"a relocated manifest": func(t *testing.T) (string, Options) {
			out, k := encryptedOut(t)
			return out, Options{Inputs: []string{src}, Out: out, Key: k, Manifest: filepath.Join(tmpDir(t), "m.json")}
		},
	}
	for label, setup := range cases {
		t.Run(label, func(t *testing.T) {
			root, opts := setup(t)
			before := snapshotTree(t, root)
			if underLock[label] {
				before = withoutLock(before)
			}
			_, err := Run(context.Background(), opts, discard(), nil)
			if !errors.Is(err, ErrEncryptedArchive) {
				t.Fatalf("want ErrEncryptedArchive, got %v", err)
			}
			rc, msg := 1, err.Error()
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(opts.Out), assure.NoSideEffect(func() bool {
				after := snapshotTree(t, root)
				if underLock[label] {
					requireHolderLineOnly(t, root)
					after = withoutLock(after)
				}
				return sameTree(before, after)
			}))
		})
	}
}

// covers: MA-725, R37
func TestAnEncryptedRunSweepsUnitsNoRecordNames(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(out, export.UnitsDir, "ab", "ab0123456789abcdef0123456789abcd")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	// Taken after the orphan's shard folder exists: the sweep removes unit files, and
	// leaves shard folders as they are.
	named := snapshotTree(t, filepath.Join(out, export.UnitsDir))
	// A crashed run started after the record's last commit: its orphan carries that
	// later start (P2a-C19), so it is the sweep's to remove. A stray older than the last
	// commit came from somewhere else and is kept (phase2b §11, CRIT-3b).
	if err := os.WriteFile(orphan, []byte("a crashed run's revision"), 0o644); err != nil {
		t.Fatal(err)
	}
	crashed := time.Now()
	os.Chtimes(orphan, crashed, crashed)
	stray := filepath.Join(out, export.UnitsDir, "ab", "ab9999999999999999999999999999aa")
	if err := os.WriteFile(stray, []byte("a file from somewhere else"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(stray, old, old)

	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("a unit no record names, left by an interrupted run, must be swept")
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatal("a unit older than the record's last commit, which no run since wrote, must be kept")
	}
	os.Remove(stray)
	if !sameTree(named, snapshotTree(t, filepath.Join(out, export.UnitsDir))) {
		t.Fatal("the sweep must leave every unit a record names exactly as it was")
	}
}

// covers: MA-725, R37
func TestAnEncryptedCaptureSealsUnitsAndNeverCollapsesRecords(t *testing.T) {
	s := sweepServer(false, []string{"A1", "A2"}, []string{"B1"}, nil)
	defer s.Close()
	out, key := encryptedOut(t)
	g := isoOptions(s)
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Key: key}, discard()); err != nil {
		t.Fatalf("an encrypted capture: %v", err)
	}
	m := loadSealed(t, out, key)
	for k, r := range assure.Reached(t, m.All(), "captured records") {
		if !state.ValidUnitID(r.Unit) || r.Sealed == nil || r.Sealed.HTML == nil {
			t.Fatalf("captured record %q is not sealed", k)
		}
	}
	if len(m.CollapsedTokens) == 0 {
		t.Fatal("a fresh encrypted capture marks its mailbox collapsed (nothing to collapse)")
	}

	// The archive now claims to owe the identity collapse, which reads .eml files by
	// logical path: on an encrypted archive it would merge distinct messages.
	m.CollapsedTokens = nil
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	// Refused before the run's record (P2a-C10): every file but the lock's holder
	// line is as it was — no unit, no "running" or failed last-run record.
	before := withoutLock(snapshotTree(t, out))
	_, err := RunGraph(context.Background(), g, Options{Out: out, Key: key}, discard())
	if !errors.Is(err, ErrEncryptedArchive) || !strings.Contains(err.Error(), "identity collapse") {
		t.Fatalf("an owed collapse on an encrypted archive: want ErrEncryptedArchive naming the collapse, got %v", err)
	}
	requireHolderLineOnly(t, out)
	if !sameTree(before, withoutLock(snapshotTree(t, out))) {
		t.Fatal("the refusal must change nothing in the archive")
	}
}
