package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-531, MA-435, R9, R20
// LoadFrom parses a manifest's bytes that the caller read itself (through an os.Root, under a
// cap): the snapshot holds what Load holds, with every migration in memory only, is bound to
// no path, so Save refuses and writes nothing, and a manifest a newer mailarchive wrote is a
// typed *NewerFormatError whose text is Load's own (the loader's wording R-p carries).
func TestLoadFromIsASnapshotThatNeverSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mailarchive-manifest.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key := Key("alice", "Inbox", "id-1")
	m.Add(key, Record{Path: "alice/Inbox/2025-03-03_0900_hello_abc.html", Folder: "Inbox", ExportedAt: time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC)})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	snap, err := LoadFrom("the manifest", data)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if rec, ok := snap.Get(key); !ok || rec.Folder != "Inbox" {
		t.Fatalf("the snapshot lost the record: %+v %v", rec, ok)
	}
	before, _ := os.ReadDir(dir)
	if err := snap.Save(); err == nil || !strings.Contains(err.Error(), "read-only snapshot") {
		t.Errorf("a snapshot's Save = %v, want the refusal that it has no file", err)
	}
	if after, _ := os.ReadDir(dir); len(after) != len(before) {
		t.Errorf("a snapshot's Save wrote into the folder: %d entries before, %d after", len(before), len(after))
	}
	if cwd, _ := os.Getwd(); cwd != "" {
		if left, _ := filepath.Glob(filepath.Join(cwd, manifestTempPattern)); len(left) != 0 {
			t.Errorf("a snapshot's Save left a temp file in the working directory: %v", left)
		}
	}

	newer := []byte(`{"version":99,"entries":{}}`)
	_, lerr := LoadFrom(path, newer)
	var nf *NewerFormatError
	if !errors.As(lerr, &nf) || nf.Version != 99 {
		t.Fatalf("a newer manifest: %v, want *NewerFormatError for version 99", lerr)
	}
	os.WriteFile(path, newer, 0o600)
	if _, lerr2 := Load(path); lerr2 == nil || lerr2.Error() != lerr.Error() {
		t.Errorf("Load says %v, LoadFrom says %v: the loader's own wording must be one text", lerr2, lerr)
	}
	if _, cerr := LoadFrom("m.json", []byte(`{"version":`)); cerr == nil || !strings.Contains(cerr.Error(), "corrupt or truncated") {
		t.Errorf("a truncated manifest: %v", cerr)
	}
}
