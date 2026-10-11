//go:build windows

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// swapForJunction replaces the directory dir by a junction to target, the way a co-writer
// on the destination would: the real directory is moved aside, inside the root, and a
// junction (which needs no privilege, unlike a symlink) takes its name.
func swapForJunction(t *testing.T, dir, target string) {
	t.Helper()
	if err := os.Rename(dir, dir+".moved"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", dir, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s %s: %v: %s", dir, target, err, out)
	}
}

// covers: MA-417, R4, R6, R29, S52
// The Windows leg of the rename-seam case (RR2-C10; it runs on the Windows CI host only and
// has not been run where it was written): a fault hook at the rename seam moves the temp
// file out of the export and swaps its parent for a junction to where it went, so that a
// path-based rename would carry the final name there. The message fails, and no message
// file appears outside the export root. A junction is a reparse point that is not a
// symlink, which os.Root must refuse to follow out of the root all the same.
func TestCopyExportRenameSeamSwapForAJunctionCannotRedirectAFile(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	msgs := ceMsgs(3)
	for i := range msgs {
		msgs[i].Received = msgs[0].Received
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
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
		if err := os.Rename(tmps[0], filepath.Join(outside, filepath.Base(tmps[0]))); err != nil {
			t.Error(err)
			return
		}
		swapForJunction(t, dir, outside)
	}}
	res := mustRun(t, e, p, src)
	for f := range treeFiles(t, outside) {
		if strings.HasSuffix(f, ".eml") {
			t.Fatalf("a message file reached the outside through a junction swapped in for its parent: %s", f)
		}
	}
	if res.Counts.Failed == 0 {
		t.Errorf("the message whose parent was swapped was not listed as failed: %+v", res.Counts)
	}
}
