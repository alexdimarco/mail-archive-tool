package desktop

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1014, R36
// Every writer of Your archives on the Desktop's side changes known-archives.json under the
// list's lock (the delete residuals' security pass, CON-07): the command line's ListRow, Add
// another archive's registerArchive and the page's tidy-up (reconcileArchives). While another
// MailArchive program holds the lock past the bound, each is refused with archindex.BusyError,
// "Your archives (FILE) is being changed by another MailArchive window; try again", the list
// byte-identical; the tidy-up still returns the list it drew. Positive twin first: with the lock
// free, each adds its row.
func TestEveryDesktopWriterOfYourArchivesTakesTheListsLock(t *testing.T) {
	e := newDeleteEnv(t)
	archive := func(name string) string {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		buildArchiveAt(t, dir)
		return dir
	}
	cli := &CommandLineDelete{cfg: e.cfg}
	now := time.Now()
	writers := []struct {
		name  string
		write func(dir string) error
	}{
		{"ListRow", cli.ListRow},
		{"registerArchive", func(dir string) error { return e.cfg.registerArchive(dir, now) }},
		{"reconcileArchives", func(dir string) error {
			if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
				t.Fatal(err)
			}
			idx, err := e.cfg.reconcileArchives(now)
			if _, ok := rowIn(idx, dir); !ok {
				t.Errorf("the tidy-up did not return the list it drew, with %s", dir)
			}
			return err
		}},
	}

	// Positive twins: the lock free, each writer lists its archive.
	for _, w := range writers {
		dir := archive("Free " + w.name)
		if err := w.write(dir); err != nil || !e.listed(dir) {
			t.Fatalf("positive twin: %s with the lock free: %v (listed %v)", w.name, err, e.listed(dir))
		}
	}

	held, err := lockfile.Acquire(archindex.LockPath(e.cfg.IndexPath))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	for _, w := range writers {
		dir := archive("Held " + w.name)
		before, _ := os.ReadFile(e.cfg.IndexPath)
		err := w.write(dir)
		var busy *archindex.BusyError
		rc, msg := 0, ""
		if err != nil {
			rc, msg = 1, err.Error()
		}
		assure.Refused(t, rc, msg, assure.Code(1),
			assure.Names("Your archives ("+e.cfg.IndexPath+") is being changed by another MailArchive window; try again"),
			assure.NoSideEffect(func() bool {
				after, _ := os.ReadFile(e.cfg.IndexPath)
				return errors.As(err, &busy) && bytes.Equal(before, after)
			}))
	}
}
