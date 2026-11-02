package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1005, R36
// The Overview never mentions a deleted archive (docs/design-archive-delete.md §10): once a
// complete delete's result is dismissed, the page names the folder nowhere — no row, no held
// result, no "Being deleted" or "Delete not finished" — while the archive kept beside it is
// still listed. Positive twin first: before the delete the page lists the archive, and until
// the dismissal it carries the delete's result.
func TestTheOverviewNeverMentionsADeletedArchive(t *testing.T) {
	e := newDeleteEnv(t)
	gone := e.plainArchive("Retired mail 2019")
	kept := e.plainArchive("Kept mail")
	const name = "Retired mail 2019"
	if page := e.page(); !strings.Contains(page, name) || !strings.Contains(page, kept) {
		t.Fatalf("positive twin: the Overview lists both archives before the delete")
	}

	d := e.planAndDelete(gone)
	if d["ok"] != true || d["status"] != "complete" {
		t.Fatalf("the delete: %v", d)
	}
	if !strings.Contains(e.page(), name) {
		t.Fatal("positive twin: until it is dismissed, the Overview carries the delete's result")
	}
	if _, m := e.post("/api/archive/delete/dismiss", map[string]any{"id": d["id"]}); m["ok"] != true {
		t.Fatalf("dismiss: %v", m)
	}

	page := e.page()
	assure.Reached(t, strings.Contains(page, kept), "the archive kept beside it, still listed")
	outside := readHelpCard(t, page).outside
	for _, never := range []string{name, gone} {
		if strings.Contains(page, never) {
			t.Errorf("after the delete the Overview still names %q", never)
		}
	}
	// The guide explains Delete in general; outside it the page says nothing of a delete.
	for _, never := range []string{"Being deleted", "Delete not finished", "was deleted", "ot fully deleted", "Deletion record", "deletions.log"} {
		if strings.Contains(outside, never) {
			t.Errorf("after the delete the Overview still says %q", never)
		}
	}
	if held := e.heldOnPage(); len(held) != 0 {
		t.Errorf("the Overview still holds %d delete results", len(held))
	}
}

// covers: MA-1005, R36
// The same when the archive deleted is the current one (conformance item 6): step 4 clears it as
// the archive in use, so once the result is dismissed the page shows "No archive chosen yet"
// (design §5.3 step 4) and names the folder nowhere — not on the This archive card, the
// settings or any row — and says nothing of a delete outside the guide, while the other archive
// is still listed. Positive twin first: before the delete the page shows it as the current
// archive.
func TestTheOverviewNeverMentionsTheCurrentArchiveOnceDeleted(t *testing.T) {
	e := newDeleteEnv(t)
	const name = "Current mail 2018"
	gone := e.plainArchive(name)
	kept := e.plainArchive("Kept mail")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: gone}); err != nil {
		t.Fatal(err)
	}
	if page := e.page(); !strings.Contains(page, gone) || strings.Contains(page, "<h2>No archive chosen yet</h2>") {
		t.Fatal("positive twin: before the delete the page shows the archive as the current one")
	}

	d := e.planAndDelete(gone)
	if d["ok"] != true || d["status"] != "complete" {
		t.Fatalf("the delete: %v", d)
	}
	if _, m := e.post("/api/archive/delete/dismiss", map[string]any{"id": d["id"]}); m["ok"] != true {
		t.Fatalf("dismiss: %v", m)
	}

	page := e.page()
	assure.Reached(t, strings.Contains(page, kept), "the archive kept beside it, still listed")
	if !strings.Contains(page, "<h2>No archive chosen yet</h2>") {
		t.Error("after the current archive's delete the page does not say no archive is chosen")
	}
	if out := LoadSettings(e.cfg.SettingsPath).Out; out != "" {
		t.Errorf("the deleted archive is still the archive in use: %q", out)
	}
	outside := readHelpCard(t, page).outside
	for _, never := range []string{name, gone} {
		if strings.Contains(page, never) {
			t.Errorf("after the delete the Overview still names %q", never)
		}
	}
	for _, never := range []string{"Being deleted", "Delete not finished", "was deleted", "ot fully deleted", "Deletion record", "deletions.log"} {
		if strings.Contains(outside, never) {
			t.Errorf("after the delete the Overview still says %q", never)
		}
	}
}
