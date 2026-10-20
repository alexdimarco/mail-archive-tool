package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
)

var (
	at0 = time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
	at1 = time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	at2 = time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
)

// archiveAt writes a minimal version-6 manifest under dir/name so the folder reads
// as a recognizable archive, and returns the folder path. The store ids decide the
// derived kind ("path:" → local, "mailbox:" → m365).
func archiveAt(t *testing.T, dir, name string, stores map[string]string) string {
	t.Helper()
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"version": 6, "stores": stores})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, manifestName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return folder
}

func rowByPath(rows []ArchiveRow, path string) (ArchiveRow, bool) {
	for _, r := range rows {
		if cleanPath(r.Path) == cleanPath(path) {
			return r, true
		}
	}
	return ArchiveRow{}, false
}

// covers: MA-664
// The Desktop maintains the folder-only index (wiring archindex into internal/desktop):
// reconcileArchives gathers the Desktop's own pointers — the archive in use, the wizard's
// last-touched breadcrumb and the weekly-backup location — folds the real archives among them
// into the index, skips a folder with no manifest, and writes the file back ONLY when the
// membership changed, so a re-render with the same set does not churn the file (SD-6, SD-9).
func TestMA664_DesktopReconcilesTheIndex(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, archindex.Name)
	settings := filepath.Join(dir, "s.json")
	a := archiveAt(t, dir, "A", map[string]string{"path:/mail/a.mbox": "a"})
	b := archiveAt(t, dir, "B", map[string]string{"mailbox:u@contoso.com": "b"})
	notArchive := filepath.Join(dir, "C") // the wizard breadcrumb points here, but it is no archive
	if err := os.MkdirAll(notArchive, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(settings, Settings{Out: a, BackupFor: b}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SettingsPath: settings, IndexPath: idxPath, LastArchive: func() string { return notArchive }}

	idx, err := cfg.reconcileArchives(at0)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	assure.Reached(t, idx.Entries, "imported archive entries")
	if len(idx.Entries) != 2 || !hasEntry(idx, a) || !hasEntry(idx, b) {
		t.Fatalf("want A and B imported, C (no manifest) skipped; got %v", entryPaths(idx))
	}
	b1, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("the first reconcile must write the index file: %v", err)
	}

	// Re-render with the same pointers: membership is unchanged, so the file is NOT
	// rewritten (no churn, no fsync, no LastSeenAt bump on disk).
	if _, err := cfg.reconcileArchives(at1); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	b2, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("an unchanged reconcile must not rewrite the index file")
	}

	// A new archive appears as the wizard breadcrumb: membership grows, so the file
	// IS rewritten and now carries it (the Desktop maintaining its cache).
	d := archiveAt(t, dir, "D", map[string]string{"path:/mail/d.mbox": "d"})
	cfg2 := Config{SettingsPath: settings, IndexPath: idxPath, LastArchive: func() string { return d }}
	idx3, err := cfg2.reconcileArchives(at2)
	if err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if len(idx3.Entries) != 3 || !hasEntry(idx3, d) {
		t.Fatalf("a newly-seen archive must be imported; got %v", entryPaths(idx3))
	}
	b3, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(b2, b3) {
		t.Fatalf("importing a new archive must rewrite the index file")
	}

	// The informational shell (no IndexPath) reconciles nothing and writes no index.
	shell := Config{SettingsPath: settings, IndexPath: ""}
	si, err := shell.reconcileArchives(at2)
	if err != nil {
		t.Fatalf("the shell reconcile must not error: %v", err)
	}
	if len(si.Entries) != 0 {
		t.Fatalf("with no IndexPath the list must be empty, got %v", entryPaths(si))
	}
}

// covers: MA-665
// The Desktop's archive list reads folder + liveness only: archiveRows marks Current on exactly
// the row whose folder is the active pointer (effectiveOut) — so changing out moves Current with no
// index write (the active-archive invariant) — reads an unreachable folder as Available=false
// (greyed, never dropped) with an empty Kind, and derives an available row's Kind live by the safe
// bounded read.
func TestMA665_ArchiveRowsDeriveLive(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "s.json")
	a := archiveAt(t, dir, "A", map[string]string{"path:/mail/a.mbox": "a"})
	b := archiveAt(t, dir, "B", map[string]string{"mailbox:u@contoso.com": "b"})
	gone := filepath.Join(dir, "gone") // in the index but never on disk
	if err := SaveSettings(settings, Settings{Out: a}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SettingsPath: settings}
	idx := archindex.Index{Entries: []archindex.Entry{{Path: a}, {Path: b}, {Path: gone}}}

	rows := cfg.archiveRows(idx)
	assure.Reached(t, rows, "archive rows")
	if len(rows) != 3 {
		t.Fatalf("want a row per entry, got %d", len(rows))
	}
	ra, _ := rowByPath(rows, a)
	rb, _ := rowByPath(rows, b)
	rg, _ := rowByPath(rows, gone)
	if !ra.Current || !ra.Available || ra.Kind != "local" {
		t.Fatalf("A is the active local archive: current=%v available=%v kind=%q", ra.Current, ra.Available, ra.Kind)
	}
	if rb.Current || !rb.Available || rb.Kind != "m365" {
		t.Fatalf("B is an available m365 archive, not current: current=%v available=%v kind=%q", rb.Current, rb.Available, rb.Kind)
	}
	if rg.Current || rg.Available || rg.Kind != "" {
		t.Fatalf("the gone folder must be unavailable (greyed) with no kind: current=%v available=%v kind=%q", rg.Current, rg.Available, rg.Kind)
	}

	// Current follows the active pointer, not the index: switch out to B and the
	// Current flag moves with no change to the index.
	if err := SaveSettings(settings, Settings{Out: b}); err != nil {
		t.Fatal(err)
	}
	rows2 := cfg.archiveRows(idx)
	ra2, _ := rowByPath(rows2, a)
	rb2, _ := rowByPath(rows2, b)
	if ra2.Current || !rb2.Current {
		t.Fatalf("after switching out to B, Current must move to B: A.current=%v B.current=%v", ra2.Current, rb2.Current)
	}
}

func hasEntry(idx archindex.Index, path string) bool {
	for _, e := range idx.Entries {
		if cleanPath(e.Path) == cleanPath(path) {
			return true
		}
	}
	return false
}

func entryPaths(idx archindex.Index) []string {
	var ps []string
	for _, e := range idx.Entries {
		ps = append(ps, e.Path)
	}
	return ps
}

// covers: MA-666
// The Overview's "Your archives" card (UX-1) lists the known archives and IS the switcher: the
// current archive is marked with no switch control, another reachable archive carries a "Switch to
// this archive" button that drives the SAME /api/settings save (so a switch keeps that path's
// weekly-backup safety question), and an unreachable archive is greyed with its state and remedy
// and no switch (UX-3). The card is hidden when the only known archive is the current one.
func TestMA666_YourArchivesCard(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, archindex.Name)
	settings := filepath.Join(dir, "s.json")
	a := archiveAt(t, dir, "A", map[string]string{"path:/mail/a.mbox": "a"})
	b := archiveAt(t, dir, "B", map[string]string{"mailbox:u@contoso.com": "b"})
	gone := filepath.Join(dir, "gone") // listed but never on disk
	if err := archindex.WriteFile(idxPath, archindex.Index{Entries: []archindex.Entry{
		{Path: a, AddedAt: at0, LastSeenAt: at0},
		{Path: b, AddedAt: at0, LastSeenAt: at0},
		{Path: gone, AddedAt: at0, LastSeenAt: at0},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(settings, Settings{Out: a}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: settings, IndexPath: idxPath, GOOS: "darwin"}))
	assure.Reached(t, page, "overview page")
	if !strings.Contains(page, `id="sec-archives"`) {
		t.Fatal("the Your-archives card is missing")
	}
	card := page[strings.Index(page, `id="sec-archives"`):]
	card = card[:strings.Index(card, "</div>")]
	for _, want := range []string{
		a, b, // both folders are listed
		"Current archive", // A is marked current
		`data-switch-archive data-path="` + b + `"`, // B is switchable via the shared save
		"local mail files",                          // A's kind label
		"Microsoft 365",                             // B's kind label
		"Not reachable right now",                   // the gone archive's state + remedy (UX-3)
		`class="archive-row unavailable"`,           // the gone archive is greyed
	} {
		if !strings.Contains(card, want) {
			t.Errorf("the Your-archives card lacks %q\ncard: %s", want, card)
		}
	}
	// The current archive and an unreachable one carry no switch button.
	if strings.Contains(card, `data-switch-archive data-path="`+a+`"`) {
		t.Error("the current archive must not carry a switch button")
	}
	if strings.Contains(card, `data-switch-archive data-path="`+gone+`"`) {
		t.Error("an unreachable archive must not carry a switch button (UX-3)")
	}

	// Hidden when the only known archive is the current one: nothing new to say.
	lonePath := filepath.Join(dir, "lone.json")
	if err := archindex.WriteFile(lonePath, archindex.Index{Entries: []archindex.Entry{{Path: a, AddedAt: at0, LastSeenAt: at0}}}); err != nil {
		t.Fatal(err)
	}
	lonePage, _ := getPage(t, DashboardHandler(Config{SettingsPath: settings, IndexPath: lonePath, GOOS: "darwin"}))
	if strings.Contains(lonePage, `id="sec-archives"`) {
		t.Error("the card must be hidden when the only known archive is the current one")
	}
}
