package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
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
	fresh := 0
	for p := range chunkSet(t, out) {
		if !before[p] {
			fresh++
		}
	}
	if fresh != 2 {
		t.Fatalf("an unchanged capture wrote %d record chunks, want the header's two copies", fresh)
	}
}
