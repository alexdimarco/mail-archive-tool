package main

import (
	"bytes"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
)

// covers: MA-1038, R36
// The terminal's plan names the files Delete keeps, as the Desktop's panel does, and each mail
// folder with the mail folders the archive recorded inside it (the 1.5.3 re-walk's N7, N16: it
// said "Kept, not the archive's: 1" and "mail folder 1: mbox"); the result names the kept file
// too. Positive twin first: a plan that keeps nothing has no kept line.
func TestTheTerminalPlanNamesKeptFilesAndMailFolders(t *testing.T) {
	twin := &archdelete.DeletePlan{Folder: "/a/Twin", Name: "Twin", Kind: archdelete.Plain, Stores: []string{"Inbox"}, StoreFolders: [][]string{{"Inbox"}}}
	tl := strings.Join(deletePlanLines(twin, ""), "\n")
	if strings.Contains(tl, "Kept") || !strings.Contains(tl, "\n  mail folder 1: Inbox\n") {
		t.Fatalf("the twin's plan:\n%s", tl)
	}

	p := &archdelete.DeletePlan{Folder: "/a/Arc", Name: "Arc", Kind: archdelete.Plain, Stores: []string{"mbox"}, StoreFolders: [][]string{{"Inbox", "Sent"}},
		Kept: []string{"my-notes.txt", "todo.txt"}}
	pl := strings.Join(deletePlanLines(p, ""), "\n")
	for _, want := range []string{"\n  mail folder 1: mbox (Inbox, Sent)\n", "\n  Kept (not the archive's): my-notes.txt, todo.txt\n"} {
		if !strings.Contains(pl, want) {
			t.Errorf("the plan lacks %q:\n%s", want, pl)
		}
	}
	if strings.Contains(pl, "Kept, not the archive's") {
		t.Errorf("the plan still only counts the kept files:\n%s", pl)
	}

	// End to end: the archive's folder holds the person's notes.txt.
	e := newCLIDel(t)
	dir := e.plainArchive("Named")
	a := &scripted{isAttended: true, answers: []string{"Named"}}
	var out bytes.Buffer
	if err := deleteArchive(a, dir, "", "", &out); err != nil {
		t.Fatalf("the delete: %v\n%s", err, a.transcript())
	}
	result, _, _ := strings.Cut(out.String(), recordHeading)
	if !strings.Contains(a.transcript(), "Kept (not the archive's): notes.txt") ||
		!strings.Contains(result, "The folder is kept: it holds 1 file that is not the archive's: notes.txt.") {
		t.Fatalf("plan:\n%s\nresult:\n%s", a.transcript(), result)
	}
}
