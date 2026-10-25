package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
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

// snapshotTree is every file under out with its bytes.
func snapshotTree(t *testing.T, out string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	for _, p := range treeOf(t, out) {
		b, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		snap[p] = b
	}
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

// covers: MA-725, R37
func TestAnEncryptedLocalRunWritesOnlyUnitsAndIsIncremental(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil)
	if err != nil {
		t.Fatalf("an encrypted run over a healthy PST: %v", err)
	}
	m := loadArchive(t, out)
	recs := assure.Reached(t, m.All(), "archived records")
	for k, r := range recs {
		if !state.ValidUnitID(r.Unit) || r.Sealed == nil || r.Sealed.HTML == nil {
			t.Fatalf("record %q is not sealed: %+v", k, r)
		}
	}
	for _, p := range treeOf(t, out) {
		top := strings.SplitN(p, "/", 2)[0]
		if strings.Contains(p, "/") && top != export.UnitsDir {
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
	underLock := map[string]bool{"an existing plaintext archive": true, "a missing manifest beside sealed units": true}
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
		"a missing manifest beside sealed units": func(t *testing.T) (string, Options) {
			out, k := encryptedOut(t)
			if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(out, ".mailarchive-manifest.json")); err != nil {
				t.Fatal(err)
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
				delete(before, ".mailarchive.lock")
			}
			_, err := Run(context.Background(), opts, discard(), nil)
			if !errors.Is(err, ErrEncryptedArchive) {
				t.Fatalf("want ErrEncryptedArchive, got %v", err)
			}
			rc, msg := 1, err.Error()
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(opts.Out), assure.NoSideEffect(func() bool {
				after := snapshotTree(t, root)
				if underLock[label] {
					delete(after, ".mailarchive.lock")
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
	named := snapshotTree(t, filepath.Join(out, export.UnitsDir))
	orphan := filepath.Join(out, export.UnitsDir, "ab", "ab0123456789abcdef0123456789abcd")
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("a crashed run's revision"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(orphan, old, old)

	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("a unit no record names, left by an interrupted run, must be swept")
	}
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
	m := loadArchive(t, out)
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
	before := snapshotTree(t, out)
	delete(before, ".mailarchive.lock")
	_, err := RunGraph(context.Background(), g, Options{Out: out, Key: key}, discard())
	if !errors.Is(err, ErrEncryptedArchive) || !strings.Contains(err.Error(), "identity collapse") {
		t.Fatalf("an owed collapse on an encrypted archive: want ErrEncryptedArchive naming the collapse, got %v", err)
	}
	after := snapshotTree(t, out)
	delete(after, ".mailarchive.lock")
	if !sameTree(before, after) {
		t.Fatal("the refusal must change nothing in the archive")
	}
}
