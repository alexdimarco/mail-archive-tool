package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-748, R37
// The records of an encrypted archive's harder cases (the Phase-2a adversarial pass,
// F17-F21 and the critic's unplugged drive): a scheduled keyed run whose archive's
// drive is not there is recorded as no record at all and refused with the drive's own
// words and kind, not "the key did not work"; an earlier plaintext run-history entry in
// a folder that becomes encrypted loses its text, counts and mode at the first
// encrypted entry; and a refused scheduled run writes no record into a PAUSED archive.
// The positive twin: a refused scheduled run into an archive that is not paused is
// recorded (MA-728).
func TestAnEncryptedArchivesRecordsInItsHarderCases(t *testing.T) {
	src := encryptedSourcePST(t)
	out, k := encryptedOut(t)

	gone := filepath.Join(tmpDir(t), "unplugged")
	_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: gone, Key: k, Unattended: true}, discard(), nil)
	if FailureKind(err) != state.FailArchiveAbsent || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("a keyed scheduled run with its drive gone: want the absent-drive refusal, got %v (kind %q)", err, FailureKind(err))
	}

	if err := state.AppendRunHistory(out, state.RunHistoryEntry{Status: state.RunFailed, Error: "input /Users/someone/Mail/SECRET-STORE.mbox does not exist", Mode: "incremental", Exported: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: k}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, state.RunHistoryName))
	if strings.Contains(string(raw), "SECRET-STORE") || strings.Contains(string(raw), "incremental") {
		t.Fatalf("an earlier entry's text or mode survived the first encrypted entry:\n%s", raw)
	}

	if err := seal.Seal(out, ""); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, out)
	RecordRefusedRun(Options{Out: out, Unattended: true}, fmt.Errorf("%w: refused", ErrEncryptedNeedsKey))
	if !sameTree(before, snapshotTree(t, out)) {
		t.Fatal("a refused run was recorded into a paused archive")
	}
}

// vaultCreate commits a vault into dir and returns its key.
func vaultCreate(dir string) (*vault.Key, error) {
	p, err := vault.PrepareCreate(dir, "failure kind test passphrase")
	if err != nil {
		return nil, err
	}
	return p.Commit()
}

// covers: MA-735, MA-754, R37
// FailureKind maps every run error onto the closed list, never onto its text: each
// typed failure has its kind, a tag names what types cannot, a run that failed two
// ways is named by its most serious typed cause, and anything else is run_failed.
func TestFailureKindIsAClosedListNeverText(t *testing.T) {
	secret := "/Users/someone/Mail/SECRET-STORE"
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: %s", ErrEncryptedNeedsKey, secret), state.FailEncryptedNeedsKey},
		{encRefusal(secret, "the key does not open it", nil), state.FailEncryptedRefused},
		{&sourceUnreachableError{path: secret}, state.FailSourceUnreachable},
		{&RunFatalError{Msg: secret}, state.FailArchiveUnwritable},
		{&outageError{errors.New(secret)}, state.ErrKindServiceUnavailable},
		{&IndexOpenError{Kind: IndexBusy, msg: secret}, state.ErrKindIndexBusy},
		{&SourceStoppedError{Msg: secret}, state.FailSourceUnreadable},
		{&PartialError{}, state.FailPartial},
		{withKind(state.FailLockLost, errors.New(secret)), state.FailLockLost},
		{manifestSaveFailure(secret, errors.New("disk full")), state.FailManifestSave},
		// The sealed record's (Phase 2b-1, MA-754): named by what the record is, not
		// by the refusal around it; a commit whose file list alone failed is not a
		// failed save.
		{recordRefusal(secret, fmt.Errorf("%w: %s", container.ErrRestoredOlder, secret)), state.FailRecordOlder},
		{recordRefusal(secret, fmt.Errorf("%w: %s", container.ErrUnfinishedSave, secret)), state.FailRecordOlder},
		{recordRefusal(secret, fmt.Errorf("%w: %s", container.ErrInventoryDamaged, secret)), state.FailRecordOlder},
		{recordRefusal(secret, fmt.Errorf("%w: %s", container.ErrRecord, secret)), state.FailManifestUnreadable},
		{recordRefusal(secret, fmt.Errorf("%w: %s", container.ErrNotSealed, secret)), state.FailEncryptedRefused},
		{manifestSaveFailure(secret, fmt.Errorf("%w: %s", container.ErrInventoryWrite, secret)), state.FailInventory},
		{fileFailureError(1, secret, errors.New("permission denied")), state.FailSourceUnreadable},
		{fmt.Errorf("archive is in use: %w", lockfile.ErrHeld), state.FailArchiveBusy},
		// As the run paths return them (F17, F18): tagged, yet named by what they are.
		{withKind(state.FailArchiveUnwritable, fmt.Errorf("archive is in use: %w", lockfile.ErrHeld)), state.FailArchiveBusy},
		{withKind(state.FailSignIn, &url.Error{Op: "Get", URL: "https://graph.example/me", Err: errors.New("dial tcp: no such host")}), state.ErrKindServiceUnavailable},
		{withKind(state.FailSignIn, errors.New("AADSTS7000222: the client secret expired")), state.FailSignIn},
		{RequireExistingOut(filepath.Join(os.TempDir(), "no-such-archive-"+secret[len(secret)-6:])), state.FailArchiveAbsent},
		// Two ways at once: the archive's own fault outranks the failed save.
		{fmt.Errorf("%w; %w", &RunFatalError{Msg: secret}, manifestSaveFailure(secret, errors.New("x"))), state.FailArchiveUnwritable},
		// A tag around a typed cause: the type names it.
		{withKind(state.FailMailboxFailed, &outageError{errors.New(secret)}), state.ErrKindServiceUnavailable},
		{errors.New(secret), state.FailRunFailed},
	}
	for _, c := range cases {
		got := FailureKind(c.err)
		if got != c.want {
			t.Fatalf("FailureKind(%v) = %q, want %q", c.err, got, c.want)
		}
		if strings.Contains(got, "SECRET") || state.FailureText(got) == "" {
			t.Fatalf("FailureKind(%v) = %q is not a kind of the closed list", c.err, got)
		}
	}
	if FailureKind(nil) != "" {
		t.Fatal("no error has no kind")
	}
	// A tag never changes the text a plaintext archive records.
	inner := errors.New(secret)
	if withKind(state.FailLockLost, inner).Error() != inner.Error() || !errors.Is(withKind(state.FailLockLost, inner), inner) {
		t.Fatal("a kind tag must keep the error's text and chain")
	}
}

// covers: MA-736, R37
// ErrorTextFor, the words stderr, the config-dir breadcrumbs and the Desktop activity
// log use: a plaintext archive's error as it is (the positive twin); an encrypted
// archive's location, encryption and failure kind, never the error's text.
func TestErrorTextForAnEncryptedArchiveIsTheKindNotTheText(t *testing.T) {
	secret := fileFailureError(1, "/Users/someone/Mail/SECRET-STORE", errors.New("permission denied"))
	plain := tmpDir(t)
	if got := ErrorTextFor(plain, secret); got != secret.Error() {
		t.Fatalf("a plaintext archive's error text must be kept: %q", got)
	}
	sealed, _ := encryptedOut(t)
	got := ErrorTextFor(sealed, secret)
	if !strings.Contains(got, sealed) || !strings.Contains(got, "encrypted archive") || !strings.Contains(got, state.FailureText(state.FailSourceUnreadable)) {
		t.Fatalf("an encrypted archive's text must name it and its kind: %q", got)
	}
	if strings.Contains(got, "SECRET") || strings.Contains(got, "permission denied") {
		t.Fatalf("an encrypted archive's text carries the error's text: %q", got)
	}
	if ErrorTextFor(sealed, nil) != "" {
		t.Fatal("no error has no text")
	}
}

// covers: MA-735, R37
// An encrypted archive's own records after real runs: a keyed run's last-run record
// and history entry are the allow-list (no job, program path, counts or duration),
// its lock names no source kind (only the A18 holder fields), a scheduled keyed
// refusal records only the minimal failed run with its kind — before the lock
// (another archive's key) and under it (an archive holding plaintext records) — and
// a plaintext run still records everything (the positive twin).
func TestAnEncryptedArchivesRunRecordsAfterRealRuns(t *testing.T) {
	src := encryptedSourcePST(t)
	plain := plainArchive(t)
	if lr, _, _ := state.ReadLastRun(plain); lr.Encrypted || lr.Exported == 0 || len(lr.Job) == 0 || lr.Exe == "" {
		t.Fatalf("the positive twin: a plaintext run records its counts, job and program: %+v", lr)
	}
	if lock, _ := os.ReadFile(filepath.Join(plain, lockfile.Name)); !strings.Contains(string(lock), "verb=export") {
		t.Fatalf("the positive twin: a plaintext run's lock names its verb: %q", lock)
	}

	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: key, Unattended: true}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	lr, _, err := state.ReadLastRun(out)
	if err != nil || !lr.Encrypted || lr.Status != state.RunOK || lr.Exported != 0 || len(lr.Job) != 0 || lr.Exe != "" || lr.Mode != "" || lr.PID != 0 {
		t.Fatalf("a keyed run's record carries more than the allow-list: %+v (%v)", lr, err)
	}
	h, _ := state.ReadRunHistory(out)
	if len(h) != 1 || h[0] != (state.RunHistoryEntry{When: lr.Finished, Status: state.RunOK, Encrypted: true}) {
		t.Fatalf("a keyed run's history entry is more than when and outcome: %+v", h)
	}
	if lock, _ := os.ReadFile(filepath.Join(out, lockfile.Name)); strings.Contains(string(lock), "verb=") {
		t.Fatalf("an encrypted archive's lock names a source kind: %q", lock)
	}

	// The source-loss streak counts an encrypted archive's unreachable source but does
	// not say what kind of source it is (A6); a plaintext archive's streak does.
	for _, enc := range []bool{false, true} {
		dir := tmpDir(t)
		updateSourceStreak(dir, &sourceUnreachableError{path: src}, lr.Finished, enc, nil)
		s, err := state.ReadSourceStreak(dir)
		if err != nil || s.ConsecutiveFails != 1 || (s.LastKind == "") != enc {
			t.Fatalf("encrypted=%v: the streak is %+v (%v)", enc, s, err)
		}
	}

	t.Run("a keyed run that fails partway", func(t *testing.T) {
		// A source that cannot be read fails the run inside it: the finished record
		// says the kind, not the file's path or the reader's error.
		bad := writeData(t, tmpDir(t), "SECRET-STORE.pst", []byte("not a pst file at all"))
		_, err := Run(context.Background(), Options{Inputs: []string{bad}, Out: out, Key: key, Unattended: true}, discard(), nil)
		if err == nil {
			t.Fatal("fixture: a run over an unreadable store succeeded")
		}
		lr, _, rerr := state.ReadLastRun(out)
		if rerr != nil || lr.Status != state.RunFailed || lr.ErrorKind != state.FailSourceUnreadable || lr.Error != "" {
			t.Fatalf("the failed keyed run's record is not its kind alone: %+v (%v)", lr, rerr)
		}
		for _, name := range []string{state.LastRunName, state.RunHistoryName, state.AttentionName} {
			if raw, _ := os.ReadFile(filepath.Join(out, name)); strings.Contains(string(raw), "SECRET-STORE") {
				t.Fatalf("%s names the source: %s", name, raw)
			}
		}
	})
	t.Run("a refused scheduled run inside an encrypted archive writes nothing", func(t *testing.T) {
		// A plaintext archive folder placed inside an encrypted one: its scheduled run
		// is refused, and the refusal is not recorded there — a record would break
		// the outer archive's allow-listed tree (P2a-C8).
		inner := filepath.Join(out, "inner")
		if err := os.MkdirAll(inner, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(inner, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		before := snapshotTree(t, out)
		_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: inner, Unattended: true}, discard(), nil)
		if !errors.Is(err, ErrEncryptedNeedsKey) {
			t.Fatalf("want ErrEncryptedNeedsKey, got %v", err)
		}
		if !sameTree(before, snapshotTree(t, out)) {
			t.Fatal("a refused run inside an encrypted archive wrote into it")
		}
		if err := os.RemoveAll(inner); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("refused before the lock", func(t *testing.T) {
		_, otherKey := encryptedOut(t)
		before := snapshotTree(t, out)
		_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Key: otherKey, Unattended: true}, discard(), nil)
		if !errors.Is(err, ErrEncryptedArchive) {
			t.Fatalf("want ErrEncryptedArchive, got %v", err)
		}
		requireMinimalRefusalRecord(t, out, before, state.FailEncryptedRefused, src)
	})
	t.Run("refused under the lock", func(t *testing.T) {
		mixed := plainArchive(t)
		p, err := vaultCreate(mixed)
		if err != nil {
			t.Fatal(err)
		}
		before := snapshotTree(t, mixed)
		_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: mixed, Key: p, Unattended: true}, discard(), nil)
		if !errors.Is(err, ErrEncryptedArchive) {
			t.Fatalf("want ErrEncryptedArchive, got %v", err)
		}
		requireMinimalRefusalRecord(t, mixed, before, state.FailEncryptedRefused, src)
	})
}
