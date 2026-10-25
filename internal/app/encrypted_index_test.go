package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// sealedIndexOf opens out's sealed record and its search index.
func sealedIndexOf(t *testing.T, out string, key *vault.Key) (*state.Manifest, *container.Store, *index.Index) {
	t.Helper()
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
	if err != nil {
		t.Fatal(err)
	}
	img, err := s.IndexImage()
	if err != nil {
		t.Fatal(err)
	}
	idx, err := index.OpenMemory(img)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	return m, s, idx
}

// requireWholeIndex fails unless the sealed index holds one row per record, is not
// marked behind, and finds a record by its subject.
func requireWholeIndex(t *testing.T, out string, key *vault.Key) {
	t.Helper()
	m, s, idx := sealedIndexOf(t, out, key)
	if n, _ := idx.Count(); n != m.Len() || n == 0 {
		t.Fatalf("the sealed index holds %d rows for %d records", n, m.Len())
	}
	if s.IndexBehind() {
		t.Fatal("the sealed index is still marked behind")
	}
}

// covers: MA-758, R37
// A sealed index marked behind is caught up by the next keyed run with indexing on
// (Phase 2b-3b, design phase2b §6, P2b-C16): every record a run with indexing off
// committed is added from its sealed units, the mark is cleared, and the index is
// whole again.
func TestAnIndexBehindIsCaughtUpByTheNextIndexedRun(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	// A first run with indexing off: no index, nothing to be behind.
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	// Indexing turned on for those records: the record has no index yet, and the
	// incremental run exports nothing new — the catch-up must add them all.
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
	// Mark it behind with an unindexed run, then catch up.
	var s *container.Store
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, s, _ = sealedIndexOf(t, out, key); !s.IndexBehind() {
		t.Fatal("fixture: the unindexed run did not mark the index behind")
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
}

// damageIndexChunk flips the last byte of out's first sealed index chunk.
func damageIndexChunk(t *testing.T, out string, key *vault.Key) {
	t.Helper()
	_, s, _ := sealedIndexOf(t, out, key)
	p := filepath.Join(out, filepath.FromSlash(s.Chunks(vault.ContainerIndex)[0]))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 1
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-758, R37
// A damaged sealed index never stops a backup and heals itself in the same run: the
// catch-up of an index marked behind finds the damage when it loads it, goes on with
// an empty index, rebuilds it from the sealed units, and the run's commit seals it
// whole.
func TestADamagedSealedIndexIsRebuiltByTheRun(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	// An unindexed run marks the index behind, so the next indexed run loads it to
	// catch it up — and finds it damaged.
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	damageIndexChunk(t, out, key)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatalf("a damaged index stopped the backup: %v", err)
	}
	requireWholeIndex(t, out, key)
}

// indexMbox is a one-message mbox entry with the given subject and Message-ID.
func indexMbox(subject, id string) string {
	return "From someone@example.com Mon Mar  3 09:00:00 2025\n" +
		"From: Someone <someone@example.com>\nTo: you@example.com\nSubject: " + subject + "\n" +
		"Message-ID: <" + id + "@example.com>\nDate: Mon, 3 Mar 2025 09:00:00 +0000\n\nThe body of " + subject + ".\n\n"
}

// covers: MA-758, R37
// The sealed index is loaded only when a run first needs it (design phase2b §6,
// P2b-C15): a run with nothing new never decrypts it — damage in it goes unseen and
// its row count is still reported, from the record — and a run that does load it
// mid-walk and finds it damaged still heals it before it ends.
func TestTheSealedIndexIsLoadedOnlyWhenARunNeedsIt(t *testing.T) {
	store := filepath.Join(tmpDir(t), "store")
	mbox := filepath.Join(store, "Inbox")
	p14Write(t, mbox, indexMbox("first", "one"))
	out, key := encryptedOut(t)
	opts := Options{Inputs: []string{store}, Out: out, Key: key, Index: true}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
	damageIndexChunk(t, out, key)

	res, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
	if err != nil {
		t.Fatal(err)
	}
	if s.IndexBehind() {
		t.Fatal("a run with nothing new loaded the sealed index (it found the damage)")
	}
	if res.Indexed != m.Len() || res.Indexed == 0 {
		t.Fatalf("a run that never loads the index reports %d indexed, want the record's %d", res.Indexed, m.Len())
	}

	p14Write(t, mbox, indexMbox("first", "one")+indexMbox("second", "two"))
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatalf("a damaged index stopped the backup: %v", err)
	}
	requireWholeIndex(t, out, key)
	if m := loadSealed(t, out, key); m.Len() != 2 {
		t.Fatalf("the record holds %d messages, want 2", m.Len())
	}
}

// covers: MA-758, R37
// The keyed rebuild at engine level builds a new index from every record's sealed
// units and commits it whole, under the archive lock; without a key it is refused.
func TestTheKeyedIndexRebuild(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	m, _, _ := sealedIndexOf(t, out, key)
	rep, err := RebuildEncrypted(context.Background(), Options{Out: out, Key: key}, discard())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Added != m.Len() || rep.Unreadable != 0 {
		t.Fatalf("the rebuild added %d of %d records (%d unreadable)", rep.Added, m.Len(), rep.Unreadable)
	}
	requireWholeIndex(t, out, key)
	if _, err := RebuildEncrypted(context.Background(), Options{Out: out}, discard()); err == nil {
		t.Fatal("a rebuild without the key must be refused")
	}
}

// covers: MA-758, R37
// No index write may follow a run's last seal: the end-of-run guard marks an index
// written after it behind and commits once more, so the next keyed run catches it
// up; with nothing written after the seal it does nothing.
func TestAnIndexWriteAfterTheLastSealIsMarkedBehind(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	m, s, idx := sealedIndexOf(t, out, key)
	si := &sealedIndex{idx: idx}
	si.sealed, _ = idx.Changes()
	si.register(s)
	if err := guardLateIndexWrite(si, s, m, discard()); err != nil || s.IndexBehind() {
		t.Fatalf("with nothing written after the seal the guard must do nothing: %v, behind %v", err, s.IndexBehind())
	}
	if err := idx.UpdateFolder(firstKey(m), "Late"); err != nil {
		t.Fatal(err)
	}
	if err := guardLateIndexWrite(si, s, m, discard()); err != nil {
		t.Fatal(err)
	}
	if _, s, _ = sealedIndexOf(t, out, key); !s.IndexBehind() {
		t.Fatal("an index write after the last seal must leave the index durably marked behind")
	}
}

// covers: MA-758, R37
// A run's index write is counted sealed only when the commit that captured it lands:
// after a commit that failed (the archive lock lost before its pointer), the write is
// still unsealed, so the next commit captures it and the end-of-run guard sees it.
func TestAFailedCommitLeavesTheIndexUnsealed(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key}),
		StillHeld: func() error { return errors.New("the lock was taken") }})
	if err != nil {
		t.Fatal(err)
	}
	si, err := openSealedIndex(s, discard())
	if err != nil {
		t.Fatal(err)
	}
	defer si.idx.Close()
	if err := si.idx.UpdateFolder(firstKey(m), "Moved"); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(); !errors.Is(err, container.ErrLockLost) {
		t.Fatalf("fixture: want the lock-lost refusal, got %v", err)
	}
	if !si.unsealed() {
		t.Fatal("an index write whose commit failed counts as sealed")
	}
}

// covers: MA-758, R37
// A sealed index found damaged when it is first used is marked behind in the record
// at that moment, before any catch-up — so a run that cannot finish the catch-up
// still leaves the mark — and the run goes on with an empty index.
func TestADamagedIndexFoundAtLoadIsMarkedBehind(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	damageIndexChunk(t, out, key)
	_, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
	if err != nil {
		t.Fatal(err)
	}
	si, err := openSealedIndex(s, discard())
	if err != nil {
		t.Fatal(err)
	}
	defer si.idx.Close()
	if s.IndexBehind() {
		t.Fatal("fixture: the index was loaded at open")
	}
	folders, err := si.idx.Folders()
	if err != nil || len(folders) != 0 {
		t.Fatalf("a damaged index must be replaced by an empty one: %v, %v", folders, err)
	}
	if !s.IndexBehind() || !si.damaged {
		t.Fatalf("a damaged index found at load must be marked behind (%v) and owed a heal (%v)", s.IndexBehind(), si.damaged)
	}
}

func firstKey(m *state.Manifest) string {
	for k := range m.All() {
		return k
	}
	return ""
}
