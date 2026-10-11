package desktop

import (
	"strings"
	"testing"
)

// covers: MA-435, MA-531, R29, S52
// The text of a row of an archive's index is shown with no control character and no bidirectional
// formatting control in it (E4-AO7): a right-to-left override in a subject or a sender's name made the
// rest of the line in the page read reversed. The list returns the text with only the controls gone.
func TestArchiveListRowsCarryNoBidiControls(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	db, err := sqlOpen(f.path("search.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE docs SET subject = ?, sender_name = ?, sender_email = ? WHERE key = ?`,
		"‮exe.gnp invoice⁦", "A‮⁩B", "x‮@example.com", f.key("hello")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	_, out := e.archRows(arch, "alice")
	found := false
	for _, r := range out["rows"].([]any) {
		row := r.(map[string]any)
		for _, k := range []string{"subject", "from", "fromName"} {
			if s, _ := row[k].(string); strings.ContainsAny(s, "‪‫‬‭‮⁦⁧⁨⁩") {
				t.Errorf("the row's %s %q carries a bidi control", k, s)
			}
		}
		if row["subject"] == "exe.gnp invoice" {
			found = true
			if row["fromName"] != "AB" || row["from"] != "x@example.com" {
				t.Errorf("the row's sender: %v", row)
			}
		}
	}
	if !found {
		t.Errorf("the row's text was lost, not just its controls: %v", out["rows"])
	}
}
