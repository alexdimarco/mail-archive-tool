package container

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// The reader's load (design-encrypted-archives-phase3 §7.3; P3-2a-1, MA-795).

// readable is an archive with records, two history chunks and a sealed index.
func readable(t *testing.T) string {
	t.Helper()
	out := fresh(t)
	m, s := open(t, out, opts(1))
	tl := s.Timeline()
	runEvents(t, tl, 1, runAt(1), 1)
	for i := 0; i < 5; i++ {
		m.Add(key(i), rec(i))
	}
	s.SetIndex((&imageSource{img: pattern(2*indexChunkSize+10, 3), version: 3, rows: 5}).get)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3000; i++ {
		tl.WriteFolder(fmt.Sprintf("s\x00big-%d", i), "Inbox/"+strings.Repeat("x", 80))
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if len(s.ptr.History) < 2 || len(s.ptr.Index) < 2 {
		t.Fatalf("fixture: %d history chunks, %d index chunks", len(s.ptr.History), len(s.ptr.Index))
	}
	return out
}

// readOnly is a reader's load options: no unit lister, no run.
func readOnly(k *vault.Key) LoadOptions { return LoadOptions{Key: k, ReadOnly: true} }

// keyCopy is a copy of keys[0] a test may destroy.
func keyCopy(t *testing.T) *vault.Key {
	t.Helper()
	o, err := vault.Open(keyDirs[0], keys[0].ArchiveID(), vault.KeySecret(keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	return o.Key
}

// covers: MA-795, R37
// A reader's load writes nothing: it reads the same records a backup's does, and its
// Store refuses to commit, to make a first commit and to sweep, each with
// ErrReadOnly, leaving every file as it was; reading the index records nothing in
// the Store. The positive twin: a backup's load of the same folder commits.
func TestAReadOnlyLoadWritesNothing(t *testing.T) {
	out := readable(t)
	before := treeBytes(t, out)
	m, s, err := LoadManifest(out, readOnly(keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := open(t, out, opts(2))
	if !reflect.DeepEqual(m.All(), want.All()) || m.Len() != 5 {
		t.Fatalf("a reader's load read %d records, unlike a backup's", m.Len())
	}
	m.Add(key(9), rec(9))
	for name, err := range map[string]error{
		"commit": m.Save(),
		"init":   s.Init(m),
		"sweep":  func() error { _, err := s.SweepContainers(runAt(99)); return err }(),
	} {
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "opened to read"),
			assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeBytes(t, out), before) }))
		if !errors.Is(err, ErrReadOnly) {
			t.Fatalf("%s: want ErrReadOnly, got %v", name, err)
		}
	}
	var img bytes.Buffer
	if err := s.WriteIndexImage(&img); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, s.indexDigests == nil && img.Len() > 0, "the index read, nothing recorded in the Store")

	// A fresh folder read is not initialised.
	empty := t.TempDir()
	fm, fs2, err := LoadManifest(empty, readOnly(keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	if err := fs2.Init(fm); !errors.Is(err, ErrReadOnly) || len(treeBytes(t, empty)) != 0 {
		t.Fatalf("a fresh folder read was initialised: %v, %d files", err, len(treeBytes(t, empty)))
	}

	// Positive twin: a backup's load commits.
	m2, _ := open(t, out, opts(3))
	m2.Add(key(9), rec(9))
	if err := m2.Save(); err != nil {
		t.Fatalf("a backup's load could not commit: %v", err)
	}
}

// covers: MA-795
// PeekFile sees every commit without the key: its result changes when a backup
// commits, not otherwise; a folder with no pointer yet is the zero result (§7.3).
func TestPeekFileSeesEveryCommit(t *testing.T) {
	empty := t.TempDir()
	if p, err := PeekFile(empty); err != nil || p != (Peeked{}) {
		t.Fatalf("no pointer: %+v, %v", p, err)
	}
	out := readable(t)
	p1, err := PeekFile(out)
	if err != nil || p1.Gen == 0 {
		t.Fatalf("peek: %+v, %v", p1, err)
	}
	again, _ := PeekFile(out)
	if again != p1 {
		t.Fatal("two peeks with no commit between differ")
	}
	m, _ := open(t, out, opts(5))
	m.Add(key(9), rec(9))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	p2, _ := PeekFile(out)
	assure.Reached(t, p2.Gen == p1.Gen+1 && p2.SHA256 != p1.SHA256, "the commit seen")
}

// covers: MA-795, R37
// A reader's history read tells a chunk that is gone, and a key locked meanwhile,
// from damage: the first two are errors (the reader reloads, or shows the locked
// page), damage still stands as a barrier; the backup's History is unchanged. The
// record's history gap is readable (§7.2, §7.3, §3.2).
func TestAReadersHistoryTellsGoneAndLockedFromDamage(t *testing.T) {
	out := readable(t)
	_, s, err := LoadManifest(out, readOnly(keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	whole, err := s.HistoryChecked()
	if err != nil || len(whole) == 0 {
		t.Fatalf("positive twin: %d events, %v", len(whole), err)
	}
	if _, _, ok := s.HistoryGap(); ok {
		t.Fatal("a whole history reported a gap")
	}
	first := filepath.Join(out, filepath.FromSlash(s.ptr.History[0].Name.Path()))
	raw, _ := os.ReadFile(first)

	// Damage: a barrier, no error.
	bad := bytes.Clone(raw)
	bad[len(bad)-1] ^= 1
	os.WriteFile(first, bad, 0o600)
	ev, err := s.HistoryChecked()
	if err != nil || !ev[0].Barrier {
		t.Fatalf("a damaged chunk: want a barrier, got %v", err)
	}

	// Gone: an error keeping fs.ErrNotExist; History still folds it into a barrier.
	os.Remove(first)
	if _, err := s.HistoryChecked(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a chunk gone: want fs.ErrNotExist, got %v", err)
	}
	assure.Reached(t, s.History()[0].Barrier, "the backup's History unchanged")
	os.WriteFile(first, raw, 0o600)

	// Locked: an error keeping vault.ErrKeyDestroyed.
	k := keyCopy(t)
	_, ls, err := LoadManifest(out, readOnly(k))
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	if _, err := ls.HistoryChecked(); !errors.Is(err, vault.ErrKeyDestroyed) {
		t.Fatalf("a locked key: want vault.ErrKeyDestroyed, got %v", err)
	}

	// The gap a record carries is told.
	m, ws := open(t, out, opts(6))
	ws.markHistoryLoss(2, 0)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	_, gs, _ := LoadManifest(out, readOnly(keys[0]))
	if from, _, ok := gs.HistoryGap(); !ok || from == 0 {
		t.Fatalf("the record's history gap was not told: %v %v", from, ok)
	}
}

// covers: MA-795, R37
// A sealed index that cannot be read keeps its cause: a chunk gone is
// fs.ErrNotExist and a locked key vault.ErrKeyDestroyed, beside ErrIndexImage, so a
// reader reloads or shows the locked page rather than a damaged index (§7.3, §3.2).
func TestAnIndexReadKeepsWhyItFailed(t *testing.T) {
	out := readable(t)
	_, s, err := LoadManifest(out, readOnly(keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.IndexImage(); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	chunk := filepath.Join(out, filepath.FromSlash(s.ptr.Index[1].Name.Path()))
	raw, _ := os.ReadFile(chunk)
	os.Remove(chunk)
	if _, err := s.IndexImage(); !errors.Is(err, ErrIndexImage) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a chunk gone: %v", err)
	}
	os.WriteFile(chunk, raw, 0o600)
	k := keyCopy(t)
	_, ls, err := LoadManifest(out, readOnly(k))
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	_, err = ls.IndexImage()
	assure.Reached(t, errors.Is(err, ErrIndexImage) && errors.Is(err, vault.ErrKeyDestroyed), "a locked key told")
}
