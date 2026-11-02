package main

import (
	"bytes"
	"strings"
	"testing"

	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1003, R36
// The command line's uninstall plan names the deletion log among what it removes and, under
// it, says to copy it first (design-archive-delete §8.1; O2-07). Positive twin first: a plan
// with no deletion log says nothing of it. Under -yes, the run that removes it, the plan says
// so and why it may (del-s5 re-verify residual 4; since the 1.5.3 friction review's D2, -yes
// removes a log holding any line only with -log-copied), and the result says it was removed.
func TestUninstallPlanAtTheTerminalNamesTheDeletionLog(t *testing.T) {
	rep := &uninstall.Report{ConfigFiles: []string{"/cfg/desktop-settings.json"}, KeptArchives: []string{"/archives/mine"}}
	var buf bytes.Buffer
	printUninstallPlan(&buf, rep, "")
	if out := buf.String(); strings.Contains(out, "deletions.log") || strings.Contains(out, "deletion log") {
		t.Fatalf("positive twin: no deletion log, none named:\n%s", out)
	}

	rep.ConfigFiles = append(rep.ConfigFiles, "/cfg/deletions.log")
	rep.DeletionsLog = "/cfg/deletions.log"
	buf.Reset()
	printUninstallPlan(&buf, rep, "")
	out := buf.String()
	want := "  - the deletion log: /cfg/deletions.log\n      Copy deletions.log first, somewhere you keep records (your Documents folder, for example), if you need it to check saved deletion records.\n"
	if !strings.Contains(out, want) {
		t.Fatalf("the plan must name the deletion log and say to copy it first (%q), got:\n%s", want, out)
	}
	if strings.Index(out, want) > strings.Index(out, "It will KEEP") {
		t.Fatalf("the deletion log must be among what is removed, before what is kept:\n%s", out)
	}

	// Under -yes the same run removes it: no "copy it first", but that this run removes it and
	// why it may; the result names it removed.
	for _, note := range []string{deletionsLogCopiedNote, deletionsLogEmptyNote} {
		buf.Reset()
		printUninstallPlan(&buf, rep, note)
		out = buf.String()
		want = "  - the deletion log: /cfg/deletions.log\n      " + note + "\n"
		if !strings.Contains(out, want) || strings.Contains(out, "Copy deletions.log first") || !strings.HasPrefix(note, "This run removes it: ") {
			t.Fatalf("under -yes the plan must say this run removes the deletion log (%q), got:\n%s", want, out)
		}
	}
	buf.Reset()
	printUninstallDone(&buf, rep)
	if done := buf.String(); !strings.Contains(done, "The deletion log was removed: /cfg/deletions.log\n") {
		t.Fatalf("the result must say the deletion log was removed:\n%s", done)
	}
	buf.Reset()
	printUninstallDone(&buf, &uninstall.Report{})
	if done := buf.String(); strings.Contains(done, "deletion log") || !strings.Contains(done, "Done —") {
		t.Fatalf("with no deletion log the result names none:\n%s", done)
	}
}
