package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// useScratchRoot points MailArchive's scratch folder at a fresh folder for one test.
func useScratchRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(tmpDir(t), "scratch")
	orig := scratchRoot
	scratchRoot = func() (string, error) { return root, nil }
	t.Cleanup(func() { scratchRoot = orig })
	return root
}

// covers: MA-738, R37
// An encrypted -copy-first run takes its snapshot in MailArchive's scratch folder,
// never inside the archive (P2a-C12): the copy lands under the scratch folder while
// no snapshot ever appears under the archive, the run archives the mail, a stale
// snapshot a crashed run left there is swept, and the folder is gone afterwards. The
// positive twin: a plaintext -copy-first run still takes it on the archive's own drive
// (MA-100).
func TestAnEncryptedCopyFirstRunSnapshotsOutsideTheArchive(t *testing.T) {
	root := useScratchRoot(t)
	src := encryptedSourcePST(t)
	var dests []string
	var underOut []string
	orig := snapshotCopy
	t.Cleanup(func() { snapshotCopy = orig })
	var out string
	snapshotCopy = func(dst io.Writer, s io.Reader) (int64, error) {
		if f, ok := dst.(*os.File); ok {
			dests = append(dests, f.Name())
		}
		if out != "" {
			m, _ := filepath.Glob(filepath.Join(out, export.SnapshotPrefix+"*"))
			underOut = append(underOut, m...)
		}
		return io.Copy(dst, s)
	}

	plain := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: plain, CopyFirst: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if len(dests) != 1 || filepath.Dir(dests[0]) != plain {
		t.Fatalf("the positive twin: a plaintext snapshot is taken in the archive: %v", dests)
	}

	dests = nil
	var k *vault.Key
	out, k = encryptedOut(t)
	snapDir, err := snapshotDir(Options{Out: out, Key: k, CopyFirst: true})
	if err != nil || !strings.HasPrefix(snapDir, root) {
		t.Fatalf("an encrypted archive's snapshot folder is %q (%v), not under %s", snapDir, err, root)
	}
	if err := os.MkdirAll(snapDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(snapDir, ".mailarchive-snapshot-crashed.pst")
	if err := os.WriteFile(stale, []byte("a crashed run's copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, CopyFirst: true}, discard(), nil)
	if err != nil || res.Stats.Exported == 0 {
		t.Fatalf("an encrypted -copy-first run: %v (exported %d)", err, res.Stats.Exported)
	}
	if len(dests) != 1 || !strings.HasPrefix(dests[0], snapDir+string(filepath.Separator)) {
		t.Fatalf("the encrypted snapshot was taken at %v, not in %s", dests, snapDir)
	}
	if len(underOut) != 0 {
		t.Fatalf("a snapshot appeared inside the encrypted archive: %v", underOut)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a stale snapshot in the scratch folder must be swept")
	}
	if _, err := os.Stat(filepath.Dir(snapDir)); !os.IsNotExist(err) {
		t.Fatalf("the archive's scratch folder must be removed after the run (%v)", err)
	}
}

// covers: MA-738, R37
// The scratch folder's drive is checked and worded as its own: an encrypted
// -copy-first run whose largest input would not fit refuses before anything is
// written — the archive byte-identical, no lock, no scratch folder; a scheduled one
// records only the minimal failed run with the scratch kind — and a full scratch
// drive met by the copy itself is run-fatal in the working folder's words, never
// "the archive drive". The positive twin: with room, the run proceeds (above).
func TestAnEncryptedCopyFirstRunChecksTheScratchDrive(t *testing.T) {
	root := useScratchRoot(t)
	src := encryptedSourcePST(t)
	out, k := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err != nil {
		t.Fatal(err)
	}

	origFree := scratchFree
	t.Cleanup(func() { scratchFree = origFree })
	scratchFree = func(string) (uint64, error) { return 1 << 20, nil }
	before := snapshotTree(t, out)
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, CopyFirst: true}, discard(), nil)
	if !errors.Is(err, ErrScratchSpace) {
		t.Fatalf("want ErrScratchSpace, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(root, "-copy-first", "free space"), assure.NoSideEffect(func() bool {
		_, serr := os.Stat(root)
		return sameTree(before, snapshotTree(t, out)) && os.IsNotExist(serr)
	}))
	_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, CopyFirst: true, Unattended: true}, discard(), nil)
	if !errors.Is(err, ErrScratchSpace) {
		t.Fatalf("want ErrScratchSpace, got %v", err)
	}
	requireMinimalRefusalRecord(t, out, before, state.FailScratch, src)
	scratchFree = origFree

	origCopy := snapshotCopy
	t.Cleanup(func() { snapshotCopy = origCopy })
	snapshotCopy = func(dst io.Writer, s io.Reader) (int64, error) {
		return 0, &fs.PathError{Op: "write", Path: dst.(*os.File).Name(), Err: errFull}
	}
	_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k, CopyFirst: true}, discard(), nil)
	var fatal *RunFatalError
	if !errors.As(err, &fatal) || !strings.Contains(err.Error(), "working folder") || strings.Contains(err.Error(), "archive drive") || FailureKind(err) != state.FailScratch {
		t.Fatalf("a full scratch drive: %v (kind %q); want the working folder's run-fatal refusal", err, FailureKind(err))
	}
}

// covers: MA-741, R37
// A keyed run refuses an encrypted archive that holds MailArchive's settings folder
// or working folder, a credential path its caller names, or (a capture) its sign-in
// token cache — before discovery, any sign-in, folder, lock or record, every file of
// the archive unchanged (P2a-C14): those keep secrets and mail copies in plaintext.
// There is no exemption for an archive holding the settings folder (a plaintext
// archive only warns, P10). The working-folder case runs the real secretDirs, its
// scratch folder moved inside the archive. The positive twin: the same run with them
// outside archives the mail (MA-725).
func TestAKeyedRunRefusesAnArchiveHoldingItsSecrets(t *testing.T) {
	src := encryptedSourcePST(t)
	for label, setup := range map[string]func(t *testing.T, out string) (Options, GraphOptions){
		"the settings folder": func(t *testing.T, out string) (Options, GraphOptions) {
			orig := secretDirs
			t.Cleanup(func() { secretDirs = orig })
			secretDirs = func() []string { return []string{filepath.Join(out, "Library", "mailarchive")} }
			return Options{}, GraphOptions{}
		},
		// The real secretDirs, its working-folder entry inside the archive.
		"the working folder": func(t *testing.T, out string) (Options, GraphOptions) {
			orig := scratchRoot
			t.Cleanup(func() { scratchRoot = orig })
			scratchRoot = func() (string, error) { return filepath.Join(out, "Caches", "mailarchive", "scratch"), nil }
			return Options{}, GraphOptions{}
		},
		"a credential path": func(t *testing.T, out string) (Options, GraphOptions) {
			return Options{CredentialPaths: []string{filepath.Join(out, "client-secret.txt")}}, GraphOptions{}
		},
		"the token cache": func(t *testing.T, out string) (Options, GraphOptions) {
			return Options{}, GraphOptions{TokenCachePath: filepath.Join(out, "graph-token.json")}
		},
	} {
		t.Run(label, func(t *testing.T) {
			out, k := encryptedOut(t)
			opts, g := setup(t, out) // the subtest's t: a replaced var is restored before the next case
			opts.Out, opts.Key = out, k
			before := snapshotTree(t, out)
			var err error
			if g.TokenCachePath != "" {
				_, err = RunGraph(context.Background(), g, opts, discard())
			} else {
				opts.Inputs = []string{src}
				_, err = Run(context.Background(), opts, discard(), nil)
			}
			if !errors.Is(err, ErrEncryptedArchive) {
				t.Fatalf("want ErrEncryptedArchive, got %v", err)
			}
			assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(out, "plaintext"),
				assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, out)) }))
		})
	}
}

// covers: MA-741, R37
// secretDirs, unreplaced, is exactly MailArchive's settings folder and its current
// working folder — the two places the keyed-run refusal above guards — and follows a
// moved working folder, so the refusal is never tested only against a stub.
func TestSecretDirsAreTheSettingsAndWorkingFolders(t *testing.T) {
	settings, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	root := useScratchRoot(t)
	got := secretDirs()
	if len(got) != 2 || got[0] != settings || got[1] != root {
		t.Fatalf("secretDirs() = %q, want exactly [%q %q]", got, settings, root)
	}
}

// covers: MA-738, R37
// OutlookPSTDir: a plaintext archive's Outlook PSTs are exported where they always
// were (<out>/_outlook-pst, kept on failure by the caller); an encrypted archive's go to
// its scratch folder — still under an _outlook-pst segment, which marks them as scratch
// exports — emptied of a leftover first and removed by cleanup.
func TestOutlookPSTsOfAnEncryptedArchiveAreOutsideIt(t *testing.T) {
	root := useScratchRoot(t)
	plain := tmpDir(t)
	if dir, cleanup, err := OutlookPSTDir(plain, nil); err != nil || dir != filepath.Join(plain, "_outlook-pst") || cleanup != nil {
		t.Fatalf("the positive twin: a plaintext archive's PSTs stay at <out>/_outlook-pst: %q %v", dir, err)
	}
	out, k := encryptedOut(t)
	dir, cleanup, err := OutlookPSTDir(out, k)
	if err != nil || !strings.HasPrefix(dir, root) || filepath.Base(dir) != "_outlook-pst" || cleanup == nil {
		t.Fatalf("an encrypted archive's PST folder is %q (%v)", dir, err)
	}
	if state.SourceKind(state.PathSourceID(filepath.Join(dir, "Mailbox.pst"))) != state.SourceOutlookScratch {
		t.Fatal("a PST in the scratch folder must still count as an Outlook scratch export")
	}
	if err := os.WriteFile(filepath.Join(dir, "left.pst"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, _, err := OutlookPSTDir(out, k); err != nil || again != dir {
		t.Fatalf("the folder must be the same every run (a PST's path is its store's identity): %q", again)
	}
	if _, err := os.Stat(filepath.Join(dir, "left.pst")); !os.IsNotExist(err) {
		t.Fatal("a leftover PST must be cleared before an export")
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("cleanup must remove the PST folder")
	}
}
