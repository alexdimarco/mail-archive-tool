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
// so and when a copy had to be made instead, and the result says it was removed (del-s5
// re-verify residual 4).
func TestUninstallPlanAtTheTerminalNamesTheDeletionLog(t *testing.T) {
	rep := &uninstall.Report{ConfigFiles: []string{"/cfg/desktop-settings.json"}, KeptArchives: []string{"/archives/mine"}}
	var buf bytes.Buffer
	printUninstallPlan(&buf, rep, false)
	if out := buf.String(); strings.Contains(out, "deletions.log") || strings.Contains(out, "deletion log") {
		t.Fatalf("positive twin: no deletion log, none named:\n%s", out)
	}

	rep.ConfigFiles = append(rep.ConfigFiles, "/cfg/deletions.log")
	rep.DeletionsLog = "/cfg/deletions.log"
	buf.Reset()
	printUninstallPlan(&buf, rep, false)
	out := buf.String()
	want := "  - the deletion log: /cfg/deletions.log\n      Copy deletions.log first if you need it to check saved deletion records.\n"
	if !strings.Contains(out, want) {
		t.Fatalf("the plan must name the deletion log and say to copy it first (%q), got:\n%s", want, out)
	}
	if strings.Index(out, want) > strings.Index(out, "It will KEEP") {
		t.Fatalf("the deletion log must be among what is removed, before what is kept:\n%s", out)
	}

	// Under -yes the same run removes it: no "copy it first", but that this run removes it and
	// that a copy had to be made before; the result names it removed.
	buf.Reset()
	printUninstallPlan(&buf, rep, true)
	out = buf.String()
	want = "  - the deletion log: /cfg/deletions.log\n      This run removes it. A copy had to be made before running uninstall -yes (uninstall -dry-run shows this list and removes nothing).\n"
	if !strings.Contains(out, want) || strings.Contains(out, "Copy deletions.log first") {
		t.Fatalf("under -yes the plan must say this run removes the deletion log (%q), got:\n%s", want, out)
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
