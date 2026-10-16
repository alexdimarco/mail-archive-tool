//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// swapForSymlink replaces the directory dir by a symlink to target, the way a
// co-writer on the destination would: the real directory is moved aside, inside the
// root, and a link takes its name.
func swapForSymlink(t *testing.T, dir, target string) {
	t.Helper()
	if err := os.Rename(dir, dir+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
}

// listOutside lists every entry under dir.
func listOutside(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for p := range treeFiles(t, dir) {
		out = append(out, p)
	}
	return out
}

// covers: MA-417, R4, R6, R29, S52
// Containment: a directory of the export swapped for a symlink that points outside,
// mid-run, is not written through. The positive twin first (no swap: every message is
// exported). With the swap, nothing is written outside the export root, and the
// messages that came after it are listed as failed, never silently dropped.
func TestCopyExportSymlinkPlantedMidRunIsNotWrittenThrough(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(5)
	for i := range msgs {
		msgs[i].Received = msgs[0].Received // one month directory
	}
	if res := mustRun(t, e, cePlan(t, e, src, msgs), src); res.Counts.Exported != 5 {
		t.Fatalf("the positive twin exported %+v", res.Counts)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	os.MkdirAll(outside, 0o755)
	p := cePlan(t, e, src, msgs)
	root := filepath.Join(p.Dest, p.ExportName)
	e.faults = &copyFaults{hook: func(step string) {
		if step == "W6:1" {
			swapForSymlink(t, filepath.Join(root, "messages", "Inbox", "2026-03"), outside)
		}
	}}
	res := mustRun(t, e, p, src)
	if got := listOutside(t, outside); len(got) != 0 {
		t.Fatalf("a symlink in the export root was written through: %v appeared outside", got)
	}
	if res.Counts.Failed < 4 || res.Outcome == CopyOutcomeComplete {
		t.Errorf("the messages after the swap were not listed as failures: %s, %+v", res.Label, res.Counts)
	}
}

// covers: MA-417, R4, R6, R29, S52
// Final placement is a no-replace rename made relative to the containing directory's
// handle, opened through the root (RR2-C10): a fault hook at the rename seam that
// moves the temp file out of the export and swaps its parent for a symlink to where it
// went (so a path-based rename would carry the final name there) makes the message
// fail, and no message file appears outside the export root.
func TestCopyExportRenameSeamSwapCannotRedirectAFile(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(3)
	for i := range msgs {
		msgs[i].Received = msgs[0].Received
	}
	outside := filepath.Join(t.TempDir(), "outside")
	os.MkdirAll(outside, 0o755)
	p := cePlan(t, e, src, msgs)
	root := filepath.Join(p.Dest, p.ExportName)
	dir := filepath.Join(root, "messages", "Inbox", "2026-03")
	e.faults = &copyFaults{hook: func(step string) {
		if step != "W5tmp:1" {
			return
		}
		tmps, _ := filepath.Glob(filepath.Join(dir, copyTempPrefix+"*"))
		if len(tmps) != 1 {
			t.Errorf("the hook found %d temp files, want 1", len(tmps))
			return
		}
		os.Rename(tmps[0], filepath.Join(outside, filepath.Base(tmps[0])))
		swapForSymlink(t, dir, outside)
	}}
	res := mustRun(t, e, p, src)
	for _, f := range listOutside(t, outside) {
		if strings.HasSuffix(f, ".eml") {
			t.Fatalf("a message file reached the outside through a swapped parent: %s", f)
		}
	}
	if res.Counts.Failed == 0 {
		t.Errorf("the message whose parent was swapped was not listed as failed: %+v", res.Counts)
	}
}
