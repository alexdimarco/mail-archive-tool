package archindex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

var (
	t0 = time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	t1 = time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
)

// writeArchive makes dir a recognizable archive by writing a minimal manifest with
// the given store ids. Version matches the engine's current manifest version so
// LoadFrom accepts it without a migration.
func writeArchive(t *testing.T, dir string, stores map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := map[string]any{"version": 6, "stores": stores}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func refusal(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

func paths(idx Index) []string {
	var ps []string
	for _, e := range idx.Entries {
		ps = append(ps, e.Path)
	}
	return ps
}

func has(idx Index, p string) bool {
	for _, e := range idx.Entries {
		if clean(e.Path) == clean(p) {
			return true
		}
	}
	return false
}

// covers: MA-653
func TestMA653_ReconcileImportsMonotone(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "A")
	b := filepath.Join(dir, "B")
	notArchive := filepath.Join(dir, "C")
	writeArchive(t, a, map[string]string{"path:/mail/a.mbox": "a"})
	writeArchive(t, b, map[string]string{"mailbox:user@contoso.com": "b"})
	if err := os.MkdirAll(notArchive, 0o755); err != nil { // no manifest
		t.Fatal(err)
	}

	idx := Reconcile(Index{}, []string{a, b, notArchive, "", "  "}, t0)
	assure.Reached(t, idx.Entries, "imported entries")
	if len(idx.Entries) != 2 {
		t.Fatalf("want 2 archives imported (A, B), got %d: %v", len(idx.Entries), paths(idx))
	}
	if !has(idx, a) || !has(idx, b) {
		t.Fatalf("A and B must be imported; got %v", paths(idx))
	}
	if has(idx, notArchive) {
		t.Fatalf("a folder with no manifest must not be imported: %v", paths(idx))
	}

	// Monotone: a second reconcile adds no duplicate and refreshes LastSeenAt
	// without rewriting AddedAt, and never removes the entry omitted this round.
	idx2 := Reconcile(idx, []string{a}, t1)
	if len(idx2.Entries) != 2 {
		t.Fatalf("reconcile must be monotone (no dup, no drop); want 2, got %d: %v", len(idx2.Entries), paths(idx2))
	}
	for _, e := range idx2.Entries {
		if e.Path == clean(a) {
			if !e.AddedAt.Equal(t0) {
				t.Fatalf("AddedAt must be stable; want %v got %v", t0, e.AddedAt)
			}
			if !e.LastSeenAt.Equal(t1) {
				t.Fatalf("LastSeenAt must refresh to %v, got %v", t1, e.LastSeenAt)
			}
		}
	}
}

// covers: MA-657
func TestMA657_UnavailableMarkedNotRemoved(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "A")
	writeArchive(t, a, map[string]string{"path:/mail/a.mbox": "a"})
	idx := Reconcile(Index{}, []string{a}, t0)
	if !Available(idx.Entries[0]) {
		t.Fatal("a present archive must read as available")
	}

	// The folder goes offline (manifest removed / drive unmounted): the entry is
	// unavailable but is NEVER removed and NEVER recreated by a reconcile.
	if err := os.Remove(filepath.Join(a, manifestName)); err != nil {
		t.Fatal(err)
	}
	if Available(idx.Entries[0]) {
		t.Fatal("an archive whose manifest is gone must read as unavailable (greyed)")
	}
	idx2 := Reconcile(idx, []string{a}, t1)
	if len(idx2.Entries) != 1 || !has(idx2, a) {
		t.Fatalf("an unavailable entry must be kept, never removed or duplicated; got %v", paths(idx2))
	}

	// A path that never existed is simply unavailable.
	if Available(Entry{Path: filepath.Join(dir, "gone")}) {
		t.Fatal("a missing folder must read as unavailable")
	}
}

// covers: MA-660
func TestMA660_RecognitionIsLexicalAndCapped(t *testing.T) {
	dir := t.TempDir()

	// Positive twin: a healthy small manifest is recognized, kind derived live.
	good := filepath.Join(dir, "good")
	writeArchive(t, good, map[string]string{"path:/mail/a.mbox": "a"})
	if !IsArchive(good) {
		t.Fatal("a healthy archive must be recognized")
	}
	if k := assure.Reached(t, Kind(good), "derived kind"); k != "local" {
		t.Fatalf("kind must derive local from a path: store id, got %q", k)
	}

	// SD-1: an attacker-chosen UNC/absolute store id is handled LEXICALLY (prefix
	// only) — recognition never resolves it on disk, so it neither hangs nor opens
	// an SMB connection. It is still a valid archive, kind derived from the prefix.
	unc := filepath.Join(dir, "unc")
	writeArchive(t, unc, map[string]string{`path:\\attacker\share\x`: "a"})
	if !IsArchive(unc) || Kind(unc) != "local" {
		t.Fatalf("a UNC-shaped store id must be read lexically (kind local), got ok=%v kind=%q", IsArchive(unc), Kind(unc))
	}

	// SD-2: a manifest larger than the cap is refused, not slurped whole. The
	// fixture is VALID JSON over the cap (a huge store value), so the refusal turns
	// on the bound, not on a parse error — remove the LimitReader and this reddens.
	big := filepath.Join(dir, "big")
	if err := os.MkdirAll(big, 0o755); err != nil {
		t.Fatal(err)
	}
	bigManifest, err := json.Marshal(map[string]any{"version": 6, "stores": map[string]string{"path:/x": strings.Repeat("x", maxManifestBytes)}})
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(bigManifest)) <= maxManifestBytes {
		t.Fatalf("fixture must exceed the %d-byte cap, got %d", maxManifestBytes, len(bigManifest))
	}
	if err := os.WriteFile(filepath.Join(big, manifestName), bigManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if IsArchive(big) {
		t.Fatal("a manifest over the size cap must be refused (SD-2), not recognized")
	}

	// SD-1: a manifest that is a symlink cannot be followed out of the archive
	// folder — the os.Root open refuses it. (Symlinks need privilege on Windows.)
	if runtime.GOOS != "windows" {
		outside := filepath.Join(dir, "outside.json")
		if err := os.WriteFile(outside, []byte(`{"version":6,"stores":{"path:/x":"t"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		linkDir := filepath.Join(dir, "link")
		if err := os.MkdirAll(linkDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(linkDir, manifestName)); err != nil {
			t.Skipf("symlink unsupported here: %v", err)
		}
		if IsArchive(linkDir) {
			t.Fatal("a symlinked manifest must not be followed out of the folder (SD-1)")
		}
	}
}

// covers: MA-661
func TestMA661_IndexReadHardened(t *testing.T) {
	dir := t.TempDir()

	// Positive twin: a well-formed index written owner-only round-trips.
	good := filepath.Join(dir, Name)
	if err := WriteFile(good, Index{Entries: []Entry{{Path: "/archives/one", AddedAt: t0, LastSeenAt: t0}}}); err != nil {
		t.Fatalf("writing a healthy index must succeed: %v", err)
	}
	back, err := ReadFile(good)
	if err != nil {
		t.Fatalf("reading back a healthy index must succeed: %v", err)
	}
	assure.Reached(t, back.Entries, "round-tripped entries")

	// A newer-version index is refused, naming the file and the cause.
	newer := filepath.Join(dir, "newer.json")
	if err := os.WriteFile(newer, []byte(`{"version":99,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadFile(newer)
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(newer, "newer"))

	// An oversize index is refused before it is parsed.
	oversize := filepath.Join(dir, "oversize.json")
	if err := os.WriteFile(oversize, []byte(strings.Repeat("x", maxIndexBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadFile(oversize)
	rc, msg = refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(oversize, "limit"))

	if runtime.GOOS != "windows" {
		// A group/other-readable index is refused (it may hold where a person's
		// archives live); the remedy names chmod 600.
		loose := filepath.Join(dir, "loose.json")
		if err := os.WriteFile(loose, []byte(`{"version":1,"entries":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err = ReadFile(loose)
		rc, msg = refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("chmod 600"))

		// A symlinked index is refused rather than followed.
		linked := filepath.Join(dir, "linked.json")
		if err := os.Symlink(good, linked); err != nil {
			t.Skipf("symlink unsupported here: %v", err)
		}
		_, err = ReadFile(linked)
		rc, msg = refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("regular file"))
	}
}

// covers: MA-662
func TestMA662_FolderOnlySchema(t *testing.T) {
	data, err := json.Marshal(Index{Version: Version, Entries: []Entry{{Path: "/a", AddedAt: t0, LastSeenAt: t1}}})
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, top, "index json keys")
	allowedTop := map[string]bool{"version": true, "entries": true}
	for k := range top {
		if !allowedTop[k] {
			t.Fatalf("index carries an unexpected top-level field %q: it must list folders only", k)
		}
	}
	entries, _ := top["entries"].([]any)
	entry, _ := entries[0].(map[string]any)
	assure.Reached(t, entry, "entry json keys")
	allowedEntry := map[string]bool{"path": true, "added_at": true, "last_seen_at": true}
	// The active-archive invariant and archive-owns-its-state: none of these may
	// ever be cached in the index.
	for _, forbidden := range []string{"active", "current", "state", "disconnected", "schedule", "kind", "label"} {
		if _, bad := entry[forbidden]; bad {
			t.Fatalf("index entry must not carry per-archive field %q (archive owns its state)", forbidden)
		}
	}
	for k := range entry {
		if !allowedEntry[k] {
			t.Fatalf("index entry carries an unexpected field %q; folder + provenance only", k)
		}
	}
}

// covers: MA-663
func TestMA663_ForgetAndBound(t *testing.T) {
	idx := Index{Entries: []Entry{
		{Path: "/a"}, {Path: "/b"}, {Path: "/c"},
	}}
	got := Forget(idx, "/b")
	if len(got.Entries) != 2 || has(got, "/b") || !has(got, "/a") || !has(got, "/c") {
		t.Fatalf("Forget must remove exactly /b, leaving /a and /c; got %v", paths(got))
	}
	// Forgetting a non-member changes nothing.
	same := Forget(got, "/zzz")
	if len(same.Entries) != 2 {
		t.Fatalf("forgetting a non-member must change nothing; got %v", paths(same))
	}

	// SD-8 bound: a positive twin at exactly MaxEntries writes; one over is refused.
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok.json")
	if err := WriteFile(okPath, Index{Entries: make([]Entry, MaxEntries)}); err != nil {
		t.Fatalf("writing exactly MaxEntries must succeed: %v", err)
	}
	overPath := filepath.Join(dir, "over.json")
	err := WriteFile(overPath, Index{Entries: make([]Entry, MaxEntries+1)})
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("limit"))
	if _, statErr := os.Stat(overPath); statErr == nil {
		t.Fatal("an over-limit index must not be written (fail-closed)")
	}
}
