package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// growManifest adds n records to the real archive in dir through the state package, under its
// first store, as a backup of n more messages records them, so its manifest is that of an archive
// of thousands of messages; it requires the manifest to end over 1 MiB.
func growManifest(t *testing.T, dir string, n int) {
	t.Helper()
	man := filepath.Join(dir, manifestName)
	m, err := state.Load(man)
	if err != nil {
		t.Fatal(err)
	}
	tok := ""
	for _, v := range m.Stores {
		if tok == "" || v < tok {
			tok = v
		}
	}
	if tok == "" {
		t.Fatalf("the archive in %s records no store", dir)
	}
	at := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		m.Add(fmt.Sprintf("%s\x00<grown-%08d@mail.example.com>", tok, i), state.Record{
			Path:        fmt.Sprintf("%s/Inbox/2024-03-01_0900_a-subject-slug-for-a-real-message_%08x.html", tok, i),
			Folder:      "Inbox",
			ExportedAt:  at,
			Fingerprint: fmt.Sprintf("%016x", i),
			FpScheme:    state.FpSchemeCurrent,
		})
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(man); err != nil || fi.Size() <= 1<<20 {
		t.Fatalf("the grown manifest must be over 1 MiB: %v %v", fi, err)
	}
}

// covers: MA-1034, R36
// An archive of thousands of messages, its manifest over 1 MiB as a real backup writes it, is a
// normal row of Your archives and opens with Open an archive folder… (the 1.5.3 re-walk's N1:
// from v1.1.0 such a row was greyed with no Switch, and Open an archive folder… refused the
// folder as one whose record "cannot be read"). The row is available, of the archive's kind,
// offers Switch to this archive and Delete; the Desktop's words for a folder with no archive are
// unchanged. Positive twin first: the same archive before it grew.
func TestALargeArchiveIsANormalRowAndOpens(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Large")
	rowOf := func() ArchiveRow {
		t.Helper()
		idx, err := archindex.ReadFile(e.cfg.IndexPath)
		if err != nil {
			t.Fatal(err)
		}
		r, ok := rowByPath(e.cfg.archiveRows(idx), dir)
		if !ok {
			t.Fatalf("no row for %s", dir)
		}
		return r
	}
	var picked string
	e.d.pick = func(context.Context, string) (string, error) { return picked, nil }
	open := func() map[string]any {
		t.Helper()
		_, m := e.post("/api/open-archive", map[string]any{})
		return m
	}

	// Positive twin: the archive as exported.
	if r := rowOf(); !r.Available || !r.OffersSwitch() || r.Kind != kindLocal {
		t.Fatalf("the positive twin: %+v", r)
	}
	picked = dir
	if m := open(); m["ok"] != true || m["path"] != dir {
		t.Fatalf("the positive twin: Open an archive folder… %v", m)
	}

	growManifest(t, dir, 6000)
	r := rowOf()
	if !r.Available || r.Kind != kindLocal || !r.OffersSwitch() || !r.DeleteOK {
		t.Fatalf("a large archive's row: available %v, kind %q, offers Switch %v, offers Delete %v (%s)", r.Available, r.Kind, r.OffersSwitch(), r.DeleteOK, r.DeleteWhy)
	}
	row := rowFor(t, e.page(), dir)
	if strings.Contains(row, "unavailable") || !strings.Contains(row, "data-switch-archive") || !strings.Contains(row, `data-delete-ok="true"`) {
		t.Fatalf("a large archive's row on the page: %s", row)
	}
	if m := open(); m["ok"] != true || m["path"] != dir {
		t.Fatalf("Open an archive folder… refused a large archive: %v", m)
	}

	// The Desktop's words for a folder with no archive, and for a record that cannot be read, are
	// its own (Settings → Archive location, Status & health), not the terminal's.
	empty := t.TempDir()
	picked = empty
	m := open()
	assure.Refused(t, 1, replyErr(m), assure.Code(1), assure.Names(empty, "isn't a MailArchive archive — choose the folder that holds the .mailarchive-manifest.json file."),
		assure.NoSideEffect(func() bool { return m["ok"] == false && !e.listed(empty) }))
	newer := filepath.Join(t.TempDir(), "Newer")
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatal(err)
	}
	// A damaged record (amended 2026-11-13: it was a version-99 one, which now says the newer
	// format's own words, MA-1297; a damaged record is still "cannot be read").
	if err := os.WriteFile(filepath.Join(newer, manifestName), []byte(`{"version":6,"stores":{damaged`), 0o644); err != nil {
		t.Fatal(err)
	}
	picked = newer
	m = open()
	assure.Refused(t, 1, replyErr(m), assure.Code(1), assure.Names(newer, "cannot be read — type the folder in Settings → Archive location, and Status & health shows how to repair it."),
		assure.NoSideEffect(func() bool { return m["ok"] == false && !e.listed(newer) }))
}
