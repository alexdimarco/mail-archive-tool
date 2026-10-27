package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// P3-8's friction fixes in the engine (MA-851): the refusals a person meets from a
// run say what happened and what to do, once, in plain words.

// recordAt records out as an encrypted archive this computer made, for the test.
func recordAt(t *testing.T, out, id string) {
	t.Helper()
	if _, err := encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, out, false, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { encrecord.Update("test", func(r *encrecord.Record) error { r.ForgetID(id); return nil }) })
}

// covers: MA-851, R37
// A keyless backup into a folder this computer recorded as an encrypted archive that
// is gone — its drive unplugged — is refused in the case's own words: "is the drive
// plugged in?" first, the folder named once, never "open it with its password",
// which cannot work there; nothing is created. One whose key files are missing is
// told how to put them back. The positive twin: the same folder, not recorded, is
// no encrypted archive and is not refused.
func TestAKeylessRunIntoARecordedFolderIsRefusedInItsCaseWords(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 1)
	out := filepath.Join(tmpDir(t), "archive")
	if err := RefuseKeyless("archive into it", out); err != nil {
		t.Fatalf("positive twin: a folder nobody recorded is not refused: %v", err)
	}

	recordAt(t, out, "0123456789abcdef0123456789abcdef")
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Index: true}, discard(), nil)
	if !errors.Is(err, ErrEncryptedNeedsKey) || FailureKind(err) != state.FailEncryptedNeedsKey {
		t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
	}
	msg := err.Error()
	assure.Refused(t, 1, msg, assure.Code(1), assure.Names("Is the drive plugged in?", "forget-archive"),
		assure.Forbid("open it in MailArchive with its password", "refusing to"),
		assure.NoSideEffect(func() bool { _, serr := os.Lstat(out); return errors.Is(serr, os.ErrNotExist) }))
	if !strings.HasPrefix(msg, "Is the drive plugged in?") || strings.Count(msg, out) != 1 {
		t.Fatalf("the drive question first, the folder named once: %q", msg)
	}

	keys := filepath.Join(tmpDir(t), "keys-gone")
	if err := os.MkdirAll(keys, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(keys, ".mailarchive-manifest.ptr"), []byte("sealed"), 0o600)
	recordAt(t, keys, "fedcba9876543210fedcba9876543210")
	kerr := RefuseKeyless("archive into it", keys)
	if !errors.Is(kerr, ErrEncryptedNeedsKey) {
		t.Fatalf("want ErrEncryptedNeedsKey, got %v", kerr)
	}
	assure.Refused(t, 1, kerr.Error(), assure.Code(1), assure.Names("restore-key-file", "key files are missing or damaged"),
		assure.Forbid("open it in MailArchive with its password"))
}

// covers: MA-851, R37
// A backup refused because another run holds the archive says so in plain words —
// what is using it and since when, and to try again when it finishes — said once,
// with none of the lock's diagnostics (process id, host, nonce), and writes
// nothing. It is still a busy refusal to the run records. The positive twin: the
// same backup runs once the other lets go.
func TestABusyArchiveIsRefusedInPlainWords(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 1)
	out := tmpDir(t)
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Index: true}, discard(), nil)
	if !errors.Is(err, lockfile.ErrHeld) || FailureKind(err) != state.FailArchiveBusy {
		held.Release()
		t.Fatalf("want a busy refusal, got %v", err)
	}
	msg := err.Error()
	assure.Refused(t, 1, msg, assure.Code(1),
		assure.Names("another backup or check is using this archive", "(started ", "try again when it finishes"),
		assure.Forbid("pid=", "host=", "nonce", "held by", out),
		assure.NoSideEffect(func() bool {
			_, serr := os.Stat(filepath.Join(out, ".mailarchive-manifest.json"))
			return errors.Is(serr, os.ErrNotExist)
		}))
	if strings.Count(msg, "try again") != 1 {
		t.Fatalf("said once: %q", msg)
	}
	held.Release()
	_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Index: true}, discard(), nil)
	assure.Reached(t, err == nil, "the backup runs once the other lets go")
}

// covers: MA-851, R37
// ReleaseBuild is the program copies' own test of a release build, so the Desktop
// words its checkbox by the same answer the backup acts on.
func TestReleaseBuildIsTheProgramCopiesTest(t *testing.T) {
	oi := programIdentity
	defer func() { programIdentity = oi }()
	programIdentity = func() (string, bool) { return "v1.5.0", true }
	release := ReleaseBuild()
	programIdentity = func() (string, bool) { return "v1.5.1-0.20261027143848-6e51f709870d+dirty", false }
	assure.Reached(t, release && !ReleaseBuild(), "a release and a development build told apart")
}
