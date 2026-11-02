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
