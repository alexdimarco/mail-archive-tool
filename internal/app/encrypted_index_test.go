package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
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
	si, err := openSealedIndex(s, out, m.Len(), false, discard())
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
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
	if err != nil {
		t.Fatal(err)
	}
	si, err := openSealedIndex(s, out, m.Len(), false, discard())
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

// forceScratchIndex makes every encrypted archive's index be worked on in a scratch
// file, by lowering the memory budget below nothing (so even a new, empty index is
// over it), for the test's length.
func forceScratchIndex(t *testing.T) {
	t.Helper()
	budget := indexMemoryBudget
	indexMemoryBudget = -1
	t.Cleanup(func() { indexMemoryBudget = budget })
}

// covers: MA-759, R37
// An index above the memory budget is worked on in a scratch file in MailArchive's
// per-user cache folder, never in the archive (phase2b §6, P2b-C15): while the run
// adds to it the file exists there, readable only by its owner; the run's commits
// seal it whole; the archive holds no search.db; and the file is gone when the run
// ends.
func TestALargeIndexIsWorkedOnInTheScratchFolder(t *testing.T) {
	forceScratchIndex(t)
	store := filepath.Join(tmpDir(t), "store")
	mbox := filepath.Join(store, "Inbox")
	p14Write(t, mbox, indexMbox("first", "one"))
	out, key := encryptedOut(t)
	path, err := indexScratchPath(out)
	if err != nil {
		t.Fatal(err)
	}
	var during []os.FileInfo
	add := indexAdd
	indexAdd = func(ix *index.Index, s string, f []string, m *model.Message, rel, k string) error {
		err := add(ix, s, f, m, rel, k) // the first add of a run may be what loads the index
		fi, _ := os.Stat(path)
		during = append(during, fi)
		return err
	}
	t.Cleanup(func() { indexAdd = add })
	opts := Options{Inputs: []string{store}, Out: out, Key: key, Index: true}
	for _, mail := range []string{indexMbox("first", "one"), indexMbox("first", "one") + indexMbox("second", "two")} {
		p14Write(t, mbox, mail)
		if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
			t.Fatal(err)
		}
		requireWholeIndex(t, out, key)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the scratch index outlived the run (%v)", err)
		}
	}
	if len(during) != 2 {
		t.Fatalf("%d messages added, want 2", len(during))
	}
	for _, fi := range during {
		if fi == nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("while the run adds to it, the index must be a scratch file readable only by its owner: %v", fi)
		}
	}
	for _, p := range treeOf(t, out) {
		if strings.Contains(p, "search.db") {
			t.Fatalf("the archive holds %s", p)
		}
	}
}

// covers: MA-759, R37
// An index is worked on in memory while its image, and an estimate for the archive's
// records, both fit the budget; either one over it moves it to a scratch file sized
// for the larger, with the margin.
func TestTheIndexScratchChoice(t *testing.T) {
	under := int(indexMemoryBudget/indexBytesPerRecord) - 1
	over := under + 2
	for _, c := range []struct {
		image   int64
		records int
		want    int64
	}{
		{0, 0, 0},
		{indexMemoryBudget, under, 0},
		{indexMemoryBudget + 1, 0, indexMemoryBudget + 1 + scratchMargin},
		{0, over, int64(over)*indexBytesPerRecord + scratchMargin},
		{indexMemoryBudget * 2, over, indexMemoryBudget*2 + scratchMargin},
	} {
		if got := indexScratchNeed(c.image, c.records); got != c.want {
			t.Errorf("image %d, %d records: scratch need %d, want %d", c.image, c.records, got, c.want)
		}
	}
}

// covers: MA-759, R37
// A scratch index a crashed run left — plaintext of the archive's index on local
// disk — is removed by the archive's next run, even one that works in memory and
// never loads the index.
func TestAScratchIndexACrashLeftIsRemovedByTheNextRun(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	opts := Options{Inputs: []string{src}, Out: out, Key: key, Index: true}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	path, _ := indexScratchPath(out)
	p14Write(t, path, "the plaintext index a crashed run left")
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the next run must remove a crashed run's scratch index (%v)", err)
	}
}

// covers: MA-759, R37
// With no room for the scratch index on its drive the backup still runs, without
// its index: the run says why, the index is marked behind, nothing is left in the
// scratch folder; the next run with room catches the index up whole. A keyed rebuild
// without room is refused with the scratch-space kind.
func TestNoRoomForTheScratchIndexStillBacksUp(t *testing.T) {
	forceScratchIndex(t)
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	free := scratchFree
	scratchFree = func(string) (uint64, error) { return 1 << 20, nil }
	t.Cleanup(func() { scratchFree = free })
	var said bytes.Buffer
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, log.New(&said, "", 0), nil); err != nil {
		t.Fatalf("no room for the index stopped the backup: %v", err)
	}
	if !strings.Contains(said.String(), "the search index is not updated this run") {
		t.Fatalf("the run must say why its index was not updated:\n%s", said.String())
	}
	m := loadSealed(t, out, key)
	if m.Len() == 0 {
		t.Fatal("fixture: nothing was backed up")
	}
	if _, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})}); err != nil || !s.IndexBehind() {
		t.Fatalf("the index must be marked behind (%v)", err)
	}
	path, _ := indexScratchPath(out)
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("a run with no room left something in the scratch folder (%v)", err)
	}
	if _, err := RebuildEncrypted(context.Background(), Options{Out: out, Key: key}, discard()); !errors.Is(err, ErrScratchSpace) || FailureKind(err) != state.FailScratch {
		t.Fatalf("a rebuild without room: want the scratch-space refusal, got %v (%s)", err, FailureKind(err))
	}
	scratchFree = free
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
}

// covers: MA-759, R37
// A message a run exported but could not add to the sealed index marks the index
// behind, so the next indexed run adds it (P2b-C16) — not left out of search for good.
func TestAMessageTheIndexMissedIsCaughtUpLater(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	add := indexAdd
	failed := 0
	indexAdd = func(ix *index.Index, s string, f []string, m *model.Message, rel, k string) error {
		if failed == 0 {
			failed++
			return errors.New("the index could not take this message")
		}
		return add(ix, s, f, m, rel, k)
	}
	t.Cleanup(func() { indexAdd = add })
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil)
	if err != nil || failed != 1 || res.IndexErrors != 1 {
		t.Fatalf("fixture: one failed add, got %d (%d index errors, %v)", failed, res.IndexErrors, err)
	}
	if _, s, _ := sealedIndexOf(t, out, key); !s.IndexBehind() {
		t.Fatal("a message the index missed must mark it behind")
	}
	indexAdd = add
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireWholeIndex(t, out, key)
}

func firstKey(m *state.Manifest) string {
	for k := range m.All() {
		return k
	}
	return ""
}
