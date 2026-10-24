package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-581
// reconstructLocalJob rebuilds a schedulable job from a LOCAL archive's own last-run
// record (secure read + strict whitelist mapper), PINS -out to the archive, and
// refuses an undeterminable record or a vanished input. underPath excludes an input
// inside <out>/_outlook-pst (an Outlook-app archive's deleted scratch PSTs).
func TestReconstructLocalJob(t *testing.T) {
	out := tbProfileArchive(t) // writes a last-run with -out <out> -mode incremental -input <store>
	j, ok := reconstructLocalJob(out)
	if !ok {
		t.Fatal("a Thunderbird archive's record should reconstruct")
	}
	if j.Out != out {
		t.Errorf("reconstructLocalJob must pin -out to the archive (%q), got %q", out, j.Out)
	}
	if j.ThunderbirdSync != nil {
		t.Error("reconstruct must not set the toggle; the handler adds it")
	}
	if len(j.Inputs) == 0 {
		t.Fatal("expected the reconstructed job to carry the archived input")
	}

	// A vanished input refuses (never schedule a broken job).
	for _, in := range j.Inputs {
		os.RemoveAll(in)
	}
	if _, ok := reconstructLocalJob(out); ok {
		t.Error("a vanished input must refuse reconstruction")
	}

	// No record at all refuses.
	if _, ok := reconstructLocalJob(filepath.Join(t.TempDir(), "nope")); ok {
		t.Error("no last-run record must refuse")
	}
}

// covers: MA-581
// underPath catches an input inside <out>/_outlook-pst but not a mere name prefix.
func TestUnderPath(t *testing.T) {
	scratch := filepath.Join("/a", "_outlook-pst")
	if !underPath(scratch, filepath.Join(scratch, "x.pst")) {
		t.Error("an input inside _outlook-pst should be caught")
	}
	if !underPath(scratch, scratch) {
		t.Error("the directory itself should be caught")
	}
	if underPath(scratch, filepath.Join("/a", "_outlook-pst-sibling")) {
		t.Error("a name-prefixed sibling must not be caught")
	}
}
