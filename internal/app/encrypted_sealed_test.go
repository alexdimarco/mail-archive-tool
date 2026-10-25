package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// chunkSet is the chunk files under out.
func chunkSet(t *testing.T, out string) map[string]bool {
	t.Helper()
	set := map[string]bool{}
	for p, b := range snapshotTree(t, filepath.Join(out, container.ChunksDir)) {
		if b != nil {
			set[p] = true
		}
	}
	return set
}

// liveChunks is the chunk files (relative to chunks/) out's sealed record names, as
// its inventory lists them live.
func liveChunks(t *testing.T, out string, key *vault.Key) map[string]bool {
	t.Helper()
	_, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]bool{}
	for p := range s.KeepUnits() {
		if rest, ok := strings.CutPrefix(p, container.ChunksDir+"/"); ok {
			live[rest] = true
		}
	}
	if len(live) == 0 {
		t.Fatal("fixture: the record names no chunk")
	}
	return live
}

// covers: MA-754, R37
// A keyed run deletes nothing while it runs (P2b-C9): the record chunks a later commit
// supersedes are still on disk when the run ends — so a copy of the archive taken
// between runs always has every chunk its pointer names — and the next run's start
// sweep removes them, keeping every chunk the record names.
func TestAnEncryptedRunDeletesNothingUntilTheNextRunsSweep(t *testing.T) {
	out, key := encryptedOut(t)
	parent := tmpDir(t)
	first := mboxStore(t, parent, "first", []string{"Inbox"}, 2)
	if _, err := Run(context.Background(), Options{Inputs: []string{first}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	// The chunks the first run's record names (its own earlier commits' superseded
	// chunks are the second run's to sweep, at its start).
	afterFirst := liveChunks(t, out, key)
	second := mboxStore(t, parent, "second", []string{"Sent"}, 2)
	if _, err := Run(context.Background(), Options{Inputs: []string{first, second}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	for p := range afterFirst {
		if !chunkSet(t, out)[p] {
			t.Fatalf("a run deleted the chunk %s it superseded", p)
		}
	}
	// The next run starts later than every chunk it finds (their mtime is the start of
	// the run that wrote them).
	old := time.Now().Add(-time.Hour)
	for p := range chunkSet(t, out) {
		os.Chtimes(filepath.Join(out, container.ChunksDir, filepath.FromSlash(p)), old, old)
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{first, second}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	m := loadSealed(t, out, key)
	if m.Len() != 4 {
		t.Fatalf("%d records after the third run, want 4", m.Len())
	}
	swept := 0
	for p := range afterFirst {
		if !chunkSet(t, out)[p] {
			swept++
		}
	}
	if swept == 0 {
		t.Fatal("the next run's start sweep removed none of the chunks the second run superseded")
	}
}

// covers: MA-757, R37
// A keyed run keeps its search index sealed in the record (Phase 2b-3): no search.db
// or companion is ever written; the sealed index holds a row for every archived
// message; an unchanged re-run re-seals no index chunk; and a run with indexing off
// marks the sealed index behind, since the records it commits are not in it.
func TestAnEncryptedRunSealsItsSearchIndex(t *testing.T) {
	src := encryptedSourcePST(t)
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"search.db", "search.db-wal", "search.db-shm", "search.db-journal"} {
		if _, err := os.Stat(filepath.Join(out, name)); !os.IsNotExist(err) {
			t.Fatalf("an encrypted run wrote %s (%v)", name, err)
		}
	}
	load := func() (*state.Manifest, *container.Store) {
		m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
		if err != nil {
			t.Fatal(err)
		}
		return m, s
	}
	m, s := load()
	img, err := s.IndexImage()
	if err != nil {
		t.Fatal(err)
	}
	idx, err := index.OpenMemory(img)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := idx.Count()
	idx.Close()
	if n == 0 || n != m.Len() {
		t.Fatalf("the sealed index holds %d rows for %d records", n, m.Len())
	}
	before := s.Chunks(vault.ContainerIndex)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, s = load(); !reflect.DeepEqual(s.Chunks(vault.ContainerIndex), before) || s.IndexBehind() {
		t.Fatal("an unchanged re-run must re-seal no index chunk and leave the index whole")
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, s = load(); !s.IndexBehind() {
		t.Fatal("a run with indexing off must mark the sealed index behind")
	}
}

// covers: MA-756, R37
// A keyed Microsoft 365 capture keeps its go-back timeline sealed in the record
// (Phase 2b-2): no history file is written, and the sealed history holds the run's
// header, an event for every captured message and the clean-run footer.
func TestAnEncryptedCaptureSealsItsHistory(t *testing.T) {
	s := sweepServer(false, []string{"A1", "A2"}, []string{"B1"}, nil)
	defer s.Close()
	out, key := encryptedOut(t)
	if _, err := RunGraph(context.Background(), isoOptions(s), Options{Out: out, Key: key}, discard()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, state.HistoryName)); !os.IsNotExist(err) {
		t.Fatalf("an encrypted capture wrote a plaintext history file (%v)", err)
	}
	_, store, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
	if err != nil {
		t.Fatal(err)
	}
	var headers, folders, footers int
	for _, ev := range store.History() {
		switch {
		case ev.Run > 0 && ev.At != "":
			headers++
		case ev.Run > 0 && ev.Completed != "":
			footers++
		case ev.K != "":
			folders++
		}
	}
	if headers != 1 || folders != 3 || footers != 1 {
		t.Fatalf("the sealed history holds %d headers, %d message events, %d footers; want 1, 3, 1", headers, folders, footers)
	}
}

// covers: MA-754, R37
// A scheduled Microsoft 365 capture that finds nothing new restamps every record it
// observes, yet re-seals only the sealed header — its pair of copies — and no bucket
// (P2b-C12): the walk's end moves the mailbox's seen_at, and each record keeps its
// "latest" marker.
func TestAnUnchangedCaptureResealsOnlyTheHeader(t *testing.T) {
	s := sweepServer(false, []string{"A1", "A2", "A3"}, []string{"B1", "B2"}, nil)
	defer s.Close()
	out, key := encryptedOut(t)
	g := isoOptions(s)
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Key: key}, discard()); err != nil {
		t.Fatal(err)
	}
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Key: key}, discard()); err != nil {
		t.Fatal(err)
	}
	before := chunkSet(t, out)
	time.Sleep(1100 * time.Millisecond) // a later run stamp, whole seconds apart
	// A checkpoint after every message: mid-walk commits see records restamped with
	// this run's stamp, which must still seal as "latest" and change no bucket.
	if _, err := RunGraph(context.Background(), g, Options{Out: out, Key: key, CheckpointEvery: 1}, discard()); err != nil {
		t.Fatal(err)
	}
	// The manifest's own chunks (the history's grow by design, one per commit with
	// events: the run's header and footer).
	_, store, err := container.LoadManifest(out, container.LoadOptions{Key: key, RunStart: time.Now().UTC(), Units: unitFiles(&export.Units{Root: out, Key: key})})
	if err != nil {
		t.Fatal(err)
	}
	fresh := 0
	for _, p := range store.Chunks(vault.ContainerManifest) {
		if !before[strings.TrimPrefix(p, container.ChunksDir+"/")] {
			fresh++
		}
	}
	if fresh != 2 {
		t.Fatalf("an unchanged capture re-sealed %d record chunks, want the header's two copies", fresh)
	}
}
