package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
			before := snapshotTree(t, out)
			err := run(out)
			if !errors.Is(err, ErrEncryptedNeedsKey) {
				t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
			}
			assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(out))
			requireMinimalRefusalRecord(t, out, before, state.FailEncryptedNeedsKey, src)
		})
	}
}

// requireMinimalRefusalRecord asserts what a recorded refusal of an encrypted
// archive changes, exactly (P2a-C10): the last-run record — failed, carrying kind
// and nothing beyond the A6 allow-list —, one run-history entry with the kind, the
// attention notice, and the lock file the record was written under; every other
// file is byte-identical, and none of the three names the archive or any of
// secrets (source paths, mailbox addresses).
func requireMinimalRefusalRecord(t *testing.T, out string, before map[string][]byte, kind string, secrets ...string) {
	t.Helper()
	after := snapshotTree(t, out)
	allowed := map[string]bool{state.LastRunName: true, state.RunHistoryName: true, state.AttentionName: true, ".mailarchive.lock": true}
	for p, b := range after {
		if !allowed[p] && !bytes.Equal(before[p], b) {
			t.Fatalf("the refusal changed %s, outside the last-run record, history and notice", p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Fatalf("the refusal removed %s", p)
		}
	}
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent || lr.Status != state.RunFailed || !lr.Encrypted || lr.ErrorKind != kind || lr.Error != "" {
		t.Fatalf("the last-run record is not the minimal failed one with kind %s: %+v (%v)", kind, lr, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(after[state.LastRunName], &raw); err != nil {
		t.Fatal(err)
	}
	for k := range raw {
		switch k {
		case "version", "status", "started", "finished", "error_kind", "encrypted":
		default:
			t.Fatalf("the last-run record carries %q, outside the A6 allow-list: %s", k, after[state.LastRunName])
		}
	}
	h, err := state.ReadRunHistory(out)
	if err != nil || len(h) == 0 || h[len(h)-1].ErrorKind != kind || h[len(h)-1].Error != "" {
		t.Fatalf("the history does not end with the minimal refusal entry: %+v (%v)", h, err)
	}
	notice := string(after[state.AttentionName])
	if !strings.Contains(notice, state.FailureText(kind)) {
		t.Fatalf("the attention notice does not say the kind's fixed words: %q", notice)
	}
	for _, name := range []string{state.LastRunName, state.RunHistoryName, state.AttentionName} {
		for _, s := range append([]string{out, filepath.Base(out)}, secrets...) {
			if s != "" && bytes.Contains(after[name], []byte(s)) {
				t.Fatalf("%s names %q", name, s)
			}
		}
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
