package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// recordingPersister keeps every view it is asked to commit, and fails when told to.
type recordingPersister struct {
	views []View
	fail  error
}

func (p *recordingPersister) Commit(v View) error {
	if p.fail != nil {
		return p.fail
	}
	p.views = append(p.views, v)
	return nil
}

func sealedRecord(unit string) Record {
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	return Record{Path: "s/Inbox/m.html", Folder: "Inbox", ExportedAt: at, FirstSeen: at, LastSeen: at, FirstFolder: "Inbox", Present: true, Unit: unit,
		Sealed: &SealedSet{HTML: &SealedUnit{Rev: 1, SHA256: strings.Repeat("ab", 32), Size: 812}}}
}

// covers: MA-751, R37
// A sealed manifest (Phase 2b-1, P2b-C1) is built from its decrypted content through
// the same checks a manifest file gets: two records sharing a unit, a newer format
// and a malformed unit id are refused exactly as Load refuses them, and the sanitising
// of untrusted fields (a stray Locator) applies too.
func TestASealedManifestGetsEveryLoadCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mailarchive-manifest.json")
	twin := View{Version: manifestVersion, Entries: map[string]Record{
		"s\x00a": sealedRecord("0123456789abcdef0123456789abcdef"),
		"s\x00b": sealedRecord("0123456789abcdef0123456789abcdef"),
	}}
	if _, err := FromSealed(path, twin, &recordingPersister{}); !errors.Is(err, ErrUnitRecord) {
		t.Fatalf("two records sharing a unit: want ErrUnitRecord, got %v", err)
	}
	var newer *NewerFormatError
	if _, err := FromSealed(path, View{Version: manifestVersion + 1}, &recordingPersister{}); !errors.As(err, &newer) {
		t.Fatalf("a newer inner format: want NewerFormatError, got %v", err)
	}
	if _, err := FromSealed(path, View{Version: manifestVersion, Entries: map[string]Record{"s\x00a": sealedRecord("XYZ")}}, &recordingPersister{}); !errors.Is(err, ErrUnitRecord) {
		t.Fatalf("a malformed unit id: want ErrUnitRecord, got %v", err)
	}
	stray := sealedRecord("0123456789abcdef0123456789abcdef")
	stray.Locator = "folder:7"
	m, err := FromSealed(path, View{Version: manifestVersion, Entries: map[string]Record{"s\x00a": stray}}, &recordingPersister{})
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Entries["s\x00a"].Locator; got != "" {
		t.Fatalf("a stray Locator survived the sealed load: %q", got)
	}
	if !m.Sealed() {
		t.Fatal("a manifest built from sealed content must say it is sealed")
	}
	if fresh := newManifest(path); fresh.Sealed() {
		t.Fatal("a plaintext manifest must not say it is sealed")
	}
}

// covers: MA-751, R37
// Saving a sealed manifest hands its whole content to the persister and writes no
// manifest file; the OnSaved hook runs only after the persister commits, never after
// a failed commit (unit deletes wait for a durable record, P2a-C1).
func TestASealedSaveCommitsThroughItsPersisterOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mailarchive-manifest.json")
	p := &recordingPersister{}
	view := View{Version: manifestVersion,
		Entries:         map[string]Record{"s\x00a": sealedRecord("0123456789abcdef0123456789abcdef")},
		Stores:          map[string]string{"mailbox:a@example.com": "s"},
		CaptureGaps:     map[string]CaptureGap{},
		CollapsedTokens: map[string]bool{"s": true}}
	m, err := FromSealed(path, view, p)
	if err != nil {
		t.Fatal(err)
	}
	saved := 0
	m.OnSaved(func() { saved++ })
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if len(p.views) != 1 || saved != 1 {
		t.Fatalf("one save: %d commits, %d hooks", len(p.views), saved)
	}
	got := p.views[0]
	if got.Version != manifestVersion || !reflect.DeepEqual(got.Entries, view.Entries) || !reflect.DeepEqual(got.Stores, view.Stores) || !reflect.DeepEqual(got.CollapsedTokens, view.CollapsedTokens) {
		t.Fatalf("the persister was handed\n %+v\nwant the manifest's content\n %+v", got, view)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a sealed save wrote %d files beside the record; it must write none itself", len(entries))
	}

	p.fail = errors.New("the pointer rename failed")
	if err := m.Save(); !errors.Is(err, p.fail) {
		t.Fatalf("a failed commit: want its error, got %v", err)
	}
	if saved != 1 {
		t.Fatal("the OnSaved hook ran after a failed commit")
	}
}
