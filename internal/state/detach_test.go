package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// covers: MA-810, R37
// An unencrypted copy (Phase 3 P3-4) reuses the decrypted manifest rather than a
// second one in memory: DetachPlaintext strips every record's Unit and Sealed in
// place, unbinds the persister and points the manifest at the copy's own file, so
// the next Save writes a plaintext manifest there — current version, no "unit" or
// "sealed" key — and the sealed record is never committed again.
func TestDetachPlaintextWritesAPlainManifestAtTheCopy(t *testing.T) {
	src := filepath.Join(t.TempDir(), ".mailarchive-manifest.json")
	p := &recordingPersister{}
	view := View{Version: manifestVersion, Entries: map[string]Record{
		"s\x00a": sealedRecord("0123456789abcdef0123456789abcdef"),
		"s\x00b": sealedRecord("fedcba9876543210fedcba9876543210"),
	}, Stores: map[string]string{"/mail/a.mbox": "s"}}
	m, err := FromSealed(src, view, p)
	if err != nil {
		t.Fatal(err)
	}
	hooked := false
	m.OnSaved(func() { hooked = true })
	if !m.UnitInUse("0123456789abcdef0123456789abcdef") {
		t.Fatal("the premise: the sealed manifest answers for its unit ids")
	}

	copyDir := t.TempDir()
	dest := filepath.Join(copyDir, ".mailarchive-manifest.json")
	if err := m.DetachPlaintext(dest); err != nil {
		t.Fatal(err)
	}
	if m.Sealed() {
		t.Fatal("a detached manifest still says it is sealed")
	}
	for k, r := range m.Entries {
		if r.Unit != "" || r.Sealed != nil {
			t.Fatalf("record %q kept its unit placement: %q %+v", k, r.Unit, r.Sealed)
		}
		if r.Path != "s/Inbox/m.html" || !r.Present {
			t.Fatalf("record %q lost its plaintext fields: %+v", k, r)
		}
	}
	if m.UnitInUse("0123456789abcdef0123456789abcdef") {
		t.Fatal("a detached manifest still answers for a unit id")
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if len(p.views) != 0 {
		t.Fatalf("the sealed record was committed %d times after detaching", len(p.views))
	}
	if hooked {
		t.Fatal("the sealed archive's after-save hook (its unit deletes) ran for the copy")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("the copy's manifest was not written: %v", err)
	}
	if s := string(data); strings.Contains(s, `"unit"`) || strings.Contains(s, `"sealed"`) {
		t.Fatalf("the copy's manifest names unit placements: %s", s)
	}
	back, err := Load(dest)
	if err != nil {
		t.Fatal(err)
	}
	if back.Version != manifestVersion || len(back.Entries) != 2 || len(back.Stores) != 1 || !reflect.DeepEqual(back.Stores, m.Stores) {
		t.Fatalf("the copy's manifest read back as version %d with %d records, stores %v", back.Version, len(back.Entries), back.Stores)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("detaching wrote at the sealed archive's own manifest path: %v", err)
	}
}

// covers: MA-810, R37
// DetachPlaintext refuses a manifest that is not sealed (a plaintext archive's own
// manifest must never be re-pointed) and refuses a destination that already holds a
// file — the copy never writes over anything — leaving the manifest sealed and bound
// to its persister. The positive twin is the test above.
func TestDetachPlaintextRefusesAPlainManifestAndAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	plain := newManifest(filepath.Join(dir, ".mailarchive-manifest.json"))
	if err := plain.DetachPlaintext(filepath.Join(dir, "copy.json")); !errors.Is(err, ErrNotSealed) {
		t.Fatalf("a plaintext manifest: want ErrNotSealed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "copy.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused detach wrote a file")
	}

	p := &recordingPersister{}
	m, err := FromSealed(filepath.Join(dir, "sealed.json"), View{Version: manifestVersion, Entries: map[string]Record{
		"s\x00a": sealedRecord("0123456789abcdef0123456789abcdef")}}, p)
	if err != nil {
		t.Fatal(err)
	}
	taken := filepath.Join(dir, "taken.json")
	if err := os.WriteFile(taken, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.DetachPlaintext(taken); !errors.Is(err, os.ErrExist) {
		t.Fatalf("an existing destination: want ErrExist, got %v", err)
	}
	if data, _ := os.ReadFile(taken); string(data) != "keep me" {
		t.Fatalf("the existing file was changed: %q", data)
	}
	if !m.Sealed() || m.Entries["s\x00a"].Unit == "" {
		t.Fatal("a refused detach changed the sealed manifest")
	}
	if err := m.Save(); err != nil || len(p.views) != 1 {
		t.Fatalf("a refused detach unbound the persister: %v, %d commits", err, len(p.views))
	}
}
