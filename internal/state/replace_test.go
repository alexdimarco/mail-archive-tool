package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// renameFault is a fault-injected rename seam: the real os.Rename preceded by
// `fail` refusals carrying errno (fail < 0: every attempt is refused), each
// wait recorded instead of slept, and Windows' in-use classification applied
// on every OS. The manifest, its temp file and the final rename are real.
type renameFault struct {
	attempts int
	waits    []time.Duration
}

func injectRenameFault(t *testing.T, fail int, errno error) *renameFault {
	t.Helper()
	f := &renameFault{}
	origRename, origSleep, origInUse := renameFile, renameSleep, renameInUse
	t.Cleanup(func() { renameFile, renameSleep, renameInUse = origRename, origSleep, origInUse })
	renameInUse = windowsRenameInUse
	renameFile = func(oldpath, newpath string) error {
		f.attempts++
		if f.attempts > 100 {
			t.Fatalf("the manifest rename was attempted %d times: the retry is unbounded", f.attempts)
		}
		if fail < 0 || f.attempts <= fail {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errno}
		}
		return os.Rename(oldpath, newpath)
	}
	renameSleep = func(d time.Duration) { f.waits = append(f.waits, d) }
	return f
}

func (f *renameFault) waited() time.Duration {
	var total time.Duration
	for _, d := range f.waits {
		total += d
	}
	return total
}

// savedManifest saves a one-record manifest and returns it, its path and the
// bytes on disk, then adds a second record for the save under test.
func savedManifest(t *testing.T) (*Manifest, string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m.Add(Key("s", "Inbox", "mid:<1@x>"), Record{Path: "s/Inbox/1.html"})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m.Add(Key("s", "Inbox", "mid:<2@x>"), Record{Path: "s/Inbox/2.html"})
	return m, path, before
}

// leftoverTemps lists the manifest temp files left beside path.
func leftoverTemps(path string) []string {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".manifest-*.tmp"))
	return matches
}

func reloadedLen(t *testing.T, path string) int {
	t.Helper()
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return m.Len()
}

// covers: MA-399, R5, S29
// A manifest save waits out a moment of Windows file sharing instead of
// failing the run (a failed final save fails it, F53): a rename refused with
// ERROR_SHARING_VIOLATION or ERROR_ACCESS_DENIED — another process (status,
// serve's go-back handler, the desktop view, antivirus) holding the manifest
// or its temp file open — is retried with a short backoff and the save
// succeeds once the holder lets go. A refusal that persists is given up within
// the bounded budget and returned, the manifest of record untouched and no
// temp file left; any other error is returned at once. Windows' classification
// is applied on every OS through the seam (which OS waits is
// TestManifestRenameRetryIsWindowsOnly). Positive twin: an unobstructed save
// renames once and waits for nothing.
func TestManifestSaveWaitsOutWindowsSharing(t *testing.T) {
	t.Run("unobstructed", func(t *testing.T) {
		m, path, _ := savedManifest(t)
		f := injectRenameFault(t, 0, errorSharingViolation)
		if err := m.Save(); err != nil {
			t.Fatalf("an unobstructed save failed: %v", err)
		}
		if f.attempts != 1 || len(f.waits) != 0 {
			t.Fatalf("an unobstructed save took %d attempts and waited %v", f.attempts, f.waits)
		}
		if n := reloadedLen(t, path); n != 2 {
			t.Fatalf("the saved manifest holds %d records, want 2", n)
		}
	})

	for _, tc := range []struct {
		name  string
		errno syscall.Errno
	}{
		{"sharing violation", errorSharingViolation},
		{"access denied", errorAccessDenied},
	} {
		t.Run(tc.name+" that clears", func(t *testing.T) {
			m, path, _ := savedManifest(t)
			f := injectRenameFault(t, 3, tc.errno)
			if err := m.Save(); err != nil {
				t.Fatalf("a rename refused 3 times (%s) and then allowed failed the save: %v", tc.name, err)
			}
			assure.Reached(t, f.waits, "waits between the refused renames")
			if f.attempts != 4 || len(f.waits) != 3 {
				t.Fatalf("%s that clears: %d attempts, waits %v — want 4 attempts and 3 waits", tc.name, f.attempts, f.waits)
			}
			if n := reloadedLen(t, path); n != 2 {
				t.Fatalf("after waiting out a %s the manifest holds %d records, want 2", tc.name, n)
			}
			if left := leftoverTemps(path); len(left) != 0 {
				t.Fatalf("temp files left behind: %v", left)
			}
		})
	}

	t.Run("sharing violation that persists", func(t *testing.T) {
		m, path, before := savedManifest(t)
		f := injectRenameFault(t, -1, errorSharingViolation)
		err := m.Save()
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names("replace manifest", path),
			assure.NoSideEffect(func() bool {
				now, rerr := os.ReadFile(path)
				return rerr == nil && bytes.Equal(now, before) && len(leftoverTemps(path)) == 0
			}))
		if !errors.Is(err, errorSharingViolation) {
			t.Fatalf("the returned error does not carry the refusal: %v", err)
		}
		if w := f.waited(); w > renameRetryBudget || w < renameRetryBudget/2 {
			t.Fatalf("waited %v in all over %d attempts — want a bounded backoff close to %v", w, f.attempts, renameRetryBudget)
		}
	})

	t.Run("any other error", func(t *testing.T) {
		m, path, before := savedManifest(t)
		f := injectRenameFault(t, -1, syscall.Errno(2)) // ERROR_FILE_NOT_FOUND: not an in-use answer
		err := m.Save()
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names("replace manifest", path),
			assure.NoSideEffect(func() bool {
				now, rerr := os.ReadFile(path)
				return rerr == nil && bytes.Equal(now, before)
			}))
		if f.attempts != 1 || len(f.waits) != 0 {
			t.Fatalf("an error that waiting cannot clear was retried: %d attempts, waits %v", f.attempts, f.waits)
		}
	})
}

// covers: MA-399, R5
// Only Windows waits: there renameInUse is ERROR_ACCESS_DENIED and
// ERROR_SHARING_VIOLATION and nothing else (a missing file is not waited
// for); on every other OS no rename error is waited out — POSIX rename
// replaces a file others hold open, its failures do not clear by waiting, and
// the same numbers 5 and 32 mean EIO and EPIPE there.
func TestManifestRenameRetryIsWindowsOnly(t *testing.T) {
	link := func(e error) error { return &os.LinkError{Op: "rename", Old: "a", New: "b", Err: e} }
	inUse := []error{link(errorAccessDenied), link(errorSharingViolation)}
	other := []error{link(syscall.Errno(2)), link(syscall.EACCES), link(syscall.EXDEV), errors.New("disk full")}

	for _, e := range inUse {
		if !windowsRenameInUse(e) {
			t.Errorf("Windows' rule does not wait out %v", e)
		}
	}
	for _, e := range other {
		if windowsRenameInUse(e) {
			t.Errorf("Windows' rule waits out %v, which waiting cannot clear", e)
		}
	}

	if runtime.GOOS == "windows" {
		for _, e := range inUse {
			if !renameInUse(e) {
				t.Errorf("on Windows a manifest rename refused with %v is not waited out", e)
			}
		}
		for _, e := range other {
			if renameInUse(e) {
				t.Errorf("on Windows a manifest rename failing with %v is waited out", e)
			}
		}
		return
	}
	for _, e := range append(inUse, other...) {
		if renameInUse(e) {
			t.Errorf("on %s a manifest rename failing with %v is waited out; only Windows waits", runtime.GOOS, e)
		}
	}
}
