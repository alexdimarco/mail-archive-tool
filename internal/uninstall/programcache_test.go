package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/progcache"
)

// covers: MA-838, R36, R37
// Uninstall removes the downloaded MailArchive release copies (program design §5,
// PG2): a dry run names the cache's folder and removes nothing; a real run removes the
// cache's own files and folders and the settings files. While another MailArchive holds
// the cache (a download under way), the WHOLE uninstall is refused before anything is
// removed.
func TestUninstallRemovesTheProgramCache(t *testing.T) {
	noSystem(t)
	c, err := progcache.Open("")
	if err != nil {
		t.Fatal(err)
	}
	v := filepath.Join(c.Root(), "v1.5.0")
	os.MkdirAll(v, 0o700)
	os.WriteFile(filepath.Join(v, progcache.Assets[0]), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(v, progcache.SumsName), []byte("x"), 0o600)
	cfg := t.TempDir()
	os.WriteFile(filepath.Join(cfg, "last-archive.txt"), []byte("x"), 0o600)

	if runtime.GOOS != "windows" {
		l, err := lockfile.Acquire(filepath.Join(c.Root(), "lock"))
		if err != nil {
			t.Fatal(err)
		}
		_, uerr := Uninstall(cfg, nil, "", false)
		_, a := os.Stat(filepath.Join(v, progcache.Assets[0]))
		_, s := os.Stat(filepath.Join(cfg, "last-archive.txt"))
		l.Release()
		assure.Refused(t, 1, errText(uerr), assure.Code(1), assure.Names("under way"),
			assure.NoSideEffect(func() bool { return a == nil && s == nil }))
	}

	rep, err := Uninstall(cfg, nil, "", true)
	if err != nil || rep.ProgramCache != c.Root() {
		t.Fatalf("the dry run: %+v %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(v, progcache.Assets[0])); err != nil {
		t.Fatal("a dry run removed a cached copy")
	}
	if _, err := Uninstall(cfg, nil, "", false); err != nil {
		t.Fatal(err)
	}
	_, gone := os.Stat(c.Root())
	assure.Reached(t, os.IsNotExist(gone), "the program cache is gone")
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
