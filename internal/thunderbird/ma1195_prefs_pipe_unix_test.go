//go:build unix

package thunderbird

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// unblockPipe opens a pipe for writing and closes it, so a reader blocked opening it (the
// defect this guards against) returns and the test leaves no goroutine stuck.
func unblockPipe(path string) {
	if f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		f.Close()
	}
}

// noHang runs fn and fails the test if it has not returned within five seconds.
func noHang(t *testing.T, what string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s waited on the pipe", what)
	}
	return nil
}

// mkPipe makes a pipe at path, skipping where there are none.
func mkPipe(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	t.Cleanup(func() { unblockPipe(path) })
}

// stillPipe reports whether path is still a pipe.
func stillPipe(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&fs.ModeNamedPipe != 0
}

// covers: MA-1195, R25
// A pipe at prefs.js is never waited on (C0c, foldcheck M1): a scheduled backup reads it
// unattended, so a planted pipe would hang the run, as TC-15 found for folderCache.json.
// FindAccountForStore refuses it at once with no account; EnableOffline refuses it with no
// backup written; and a pipe where the backup goes is refused before prefs.js changes.
// Positive twin first: a healthy profile gives the account.
func TestAPipeAtPrefsJsIsNeverWaitedOn(t *testing.T) {
	healthy := profileWith(t, onePrefs, "imap.gmail.com")
	if got := accountKey(t, healthy, "imap.gmail.com"); got != "server1" {
		t.Fatalf("the positive twin = %s, want server1", got)
	}

	t.Run("finding the account", func(t *testing.T) {
		p := t.TempDir()
		store := filepath.Join(p, "ImapMail", "imap.gmail.com")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatal(err)
		}
		pipe := filepath.Join(p, "prefs.js")
		mkPipe(t, pipe)
		var acct *Account
		err := noHang(t, "FindAccountForStore", func() error {
			a, err := FindAccountForStore(p, store)
			acct = a
			return err
		})
		if acct != nil {
			t.Fatalf("an account was read from a pipe: %+v", acct)
		}
		refusedNaming(t, err, func() bool { return stillPipe(pipe) }, "prefs.js", "a pipe")
	})

	t.Run("EnableOffline's read", func(t *testing.T) {
		p := t.TempDir()
		pipe := filepath.Join(p, "prefs.js")
		mkPipe(t, pipe)
		err := noHang(t, "EnableOffline", func() error { _, err := EnableOffline(p, "server1"); return err })
		refusedNaming(t, err, func() bool { return stillPipe(pipe) && absent(pipe+".mailarchive-bak") }, "prefs.js", "a pipe")
	})

	t.Run("a pipe where the backup goes", func(t *testing.T) {
		p := profileWith(t, onePrefs, "imap.gmail.com")
		pipe := filepath.Join(p, "prefs.js.mailarchive-bak")
		mkPipe(t, pipe)
		err := noHang(t, "EnableOffline", func() error { _, err := EnableOffline(p, "server1"); return err })
		refusedNaming(t, err, func() bool {
			return stillPipe(pipe) && bytesStill(filepath.Join(p, "prefs.js"), []byte(onePrefs))()
		}, "prefs.js.mailarchive-bak", "a pipe")
	})
}
