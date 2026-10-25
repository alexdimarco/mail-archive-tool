package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// sealedArchive is an encrypted archive holding the PST fixture's messages as
// units, written by a keyed run.
func sealedArchive(t *testing.T) string {
	t.Helper()
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// plainArchive is a plaintext archive of the same fixture.
func plainArchive(t *testing.T) string {
	t.Helper()
	out := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Index: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// keylessOps is every app entry point that reads an archive's files by their
// logical names, or writes into it, run without a key.
func keylessOps(src string) map[string]func(out string) error {
	return map[string]func(out string) error{
		"Run": func(out string) error {
			_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out}, discard(), nil)
			return err
		},
		"Reindex": func(out string) error { _, _, err := Reindex(out, discard()); return err },
		"Rebuild": func(out string) error { _, err := Rebuild(out, discard()); return err },
		"Verify":  func(out string) error { _, err := Verify(out, VerifyOptions{}, discard(), nil); return err },
		"Extract": func(out string) error {
			_, err := Extract(out, FormatEML, filepath.Join(filepath.Dir(out), filepath.Base(out)+"-extract"), false, discard(), nil)
			return err
		},
	}
}

// covers: MA-728, R37
func TestKeylessOperationsRefuseAnEncryptedArchiveBeforeTouchingIt(t *testing.T) {
	src := encryptedSourcePST(t)
	// The positive twin: each operation succeeds on a plaintext archive.
	for name, op := range keylessOps(src) {
		if err := op(plainArchive(t)); err != nil {
			t.Fatalf("%s on a plaintext archive: %v", name, err)
		}
	}
	for name, op := range keylessOps(src) {
		// With its vault, and with both vault files deleted (A15: a deleted vault is
		// never "not encrypted" — its units still say so).
		for _, vaultGone := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/vault deleted=%v", name, vaultGone), func(t *testing.T) {
				out := sealedArchive(t)
				if vaultGone {
					for _, f := range []string{vault.FileName, vault.BackupName} {
						if err := os.Remove(filepath.Join(out, f)); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := snapshotTree(t, out)
				err := op(out)
				if !errors.Is(err, ErrEncryptedNeedsKey) {
					t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
				}
				assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(out, "password or recovery key"),
					assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, out)) }))
			})
		}
	}
	t.Run("RunGraph", func(t *testing.T) {
		out := sealedArchive(t)
		before := snapshotTree(t, out)
		_, err := RunGraph(context.Background(), GraphOptions{}, Options{Out: out}, discard())
		if !errors.Is(err, ErrEncryptedNeedsKey) {
			t.Fatalf("want ErrEncryptedNeedsKey before any sign-in, got %v", err)
		}
		assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(out),
			assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, out)) }))
	})
}

// covers: MA-728, R37
func TestAKeylessScheduledRunIntoAnEncryptedArchiveIsRecordedAsFailed(t *testing.T) {
	src := encryptedSourcePST(t)
	for name, run := range map[string]func(out string) error{
		"Run": func(out string) error {
			_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Unattended: true}, discard(), nil)
			return err
		},
		"RunGraph": func(out string) error {
			_, err := RunGraph(context.Background(), GraphOptions{}, Options{Out: out, Unattended: true}, discard())
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			out := sealedArchive(t)
			units := filepath.Join(out, "units")
			before := snapshotTree(t, units)
			err := run(out)
			if !errors.Is(err, ErrEncryptedNeedsKey) {
				t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
			}
			lr, st, rerr := state.ReadLastRun(out)
			if rerr != nil || st != state.LastRunPresent || lr.Status != state.RunFailed || lr.Error != err.Error() {
				t.Fatalf("the refusal is not the recorded last run: %+v (%v, %v)", lr, st, rerr)
			}
			assure.Refused(t, 1, lr.Error, assure.Code(1), assure.Names(out),
				assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, units)) }))
		})
	}
}

// covers: MA-728, R37
func TestRefuseKeylessLooksAtEveryFolderTheOperationTouches(t *testing.T) {
	plain, sealed := plainArchive(t), sealedArchive(t)
	if err := RefuseKeyless("archive into it", plain, filepath.Join(plain, "m.json"), filepath.Join(tmpDir(t), "dest")); err != nil {
		t.Fatalf("a plaintext archive, its manifest folder and an outside destination are not refused: %v", err)
	}
	for label, args := range map[string][2]string{
		"an out inside it":         {filepath.Join(sealed, "nested", "archive"), ""},
		"a -manifest inside it":    {plain, filepath.Join(sealed, "sub")},
		"a destination inside it":  {plain, filepath.Join(sealed, "extracted")},
		"the encrypted folder too": {sealed, ""},
	} {
		t.Run(label, func(t *testing.T) {
			err := RefuseKeyless("extract from it", args[0], args[1])
			if !errors.Is(err, ErrEncryptedNeedsKey) {
				t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
			}
			assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed, "extract from it"))
		})
	}
	// Extract refuses a destination inside an encrypted archive and writes nothing there.
	dest := filepath.Join(sealed, "extracted")
	before := snapshotTree(t, sealed)
	_, err := Extract(plain, FormatEML, dest, false, discard(), nil)
	if !errors.Is(err, ErrEncryptedNeedsKey) {
		t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed),
		assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, sealed)) }))
}
