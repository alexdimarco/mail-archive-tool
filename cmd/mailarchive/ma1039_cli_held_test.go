package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
)

// covers: MA-1039, R36
// The terminal's plan says, right after the line that it finishes an earlier delete, that a file of
// it is still held by another program (the 1.5.3 re-walk's N17). Positive twin first: with none
// held, no such line.
func TestTheTerminalPlanWarnsOfAFileStillHeld(t *testing.T) {
	p := &archdelete.DeletePlan{Folder: "/a/Arc", Name: "Arc", Kind: archdelete.Plain}
	if l := strings.Join(deletePlanLines(p, ""), "\n"); strings.Contains(l, "still in use") {
		t.Fatalf("the twin:\n%s", l)
	}
	p.Held = []string{"Mailbox/Inbox/x.html"}
	lines := deletePlanLines(p, "")
	if len(lines) == 0 || lines[0] != p.HeldLine() || !strings.HasPrefix(lines[0], "1 file this delete must remove is still in use by another program: Mailbox/Inbox/x.html.") {
		t.Fatalf("the plan:\n%s", strings.Join(lines, "\n"))
	}
}
