package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/export"
)

// covers: MA-1048, R36
// A spelling of the marker's name that the volume makes one name with it (another case, on APFS
// or NTFS) refuses every backup as the marker does, in the marker-only words, kind
// delete_unfinished, nothing written; a spelling the volume keeps apart (any case on a
// case-sensitive volume) is an ordinary file of the folder and the backup runs (the slice-2
// adversarial pass's case probe, kept). Positive twin first: the archive without it is backed up.
func TestMA1048_ACaseVariantTheVolumeJoinsIsTheMarkerForEveryBackup(t *testing.T) {
	ma1054NoCheck(t)
	in := ma956Input(t)
	run := func(out string, unattended bool) error {
		_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Unattended: unattended}, discard(), nil)
		return err
	}
	out := filepath.Join(t.TempDir(), "case")
	if err := run(out, false); err != nil {
		t.Fatal(err)
	}

	// Positive twin.
	if err := run(out, true); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	variant := ".MAILARCHIVE-Deleting.JSON"
	if err := os.WriteFile(filepath.Join(out, variant), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	vfi, err := os.Lstat(filepath.Join(out, variant))
	if err != nil {
		t.Fatal(err)
	}
	mfi, merr := os.Lstat(filepath.Join(out, delmark.Name))
	joined := merr == nil && os.SameFile(vfi, mfi)

	before := ma1054Files(t, out)
	err = run(out, true)
	if !joined {
		if err != nil {
			t.Fatalf("a spelling this volume keeps apart from the marker's name refused the backup: %v", err)
		}
		return
	}
	// ma1054Files leaves out only the exact name: the variant is the same file, so it is listed
	// under its own spelling (or the exact one) on both sides alike.
	ma1054Refused(t, err, out, delmark.MsgOnlyMarker, func() bool { return ma1044Same(before, ma1054Files(t, out)) })
}
