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
// with no deletion log says nothing of it.
func TestUninstallPlanAtTheTerminalNamesTheDeletionLog(t *testing.T) {
	rep := &uninstall.Report{ConfigFiles: []string{"/cfg/desktop-settings.json"}, KeptArchives: []string{"/archives/mine"}}
	var buf bytes.Buffer
	printUninstallPlan(&buf, rep)
	if out := buf.String(); strings.Contains(out, "deletions.log") || strings.Contains(out, "deletion log") {
		t.Fatalf("positive twin: no deletion log, none named:\n%s", out)
	}

	rep.ConfigFiles = append(rep.ConfigFiles, "/cfg/deletions.log")
	rep.DeletionsLog = "/cfg/deletions.log"
	buf.Reset()
	printUninstallPlan(&buf, rep)
	out := buf.String()
	want := "  - the deletion log: /cfg/deletions.log\n      Copy deletions.log first if you need it to check saved deletion records.\n"
	if !strings.Contains(out, want) {
		t.Fatalf("the plan must name the deletion log and say to copy it first (%q), got:\n%s", want, out)
	}
	if strings.Index(out, want) > strings.Index(out, "It will KEEP") {
		t.Fatalf("the deletion log must be among what is removed, before what is kept:\n%s", out)
	}
}
