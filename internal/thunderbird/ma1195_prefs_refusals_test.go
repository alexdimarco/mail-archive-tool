package thunderbird

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

const offLine = `user_pref("mail.server.server1.offline_download", false);` + "\n"

// onePrefs is a prefs.js with one IMAP account, imap.gmail.com, set to off.
var onePrefs = live("server1") + imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + offLine

// refusedNaming asserts a clean refusal (an error, not a crash) naming each of names, that
// left untouched what it must.
func refusedNaming(t *testing.T, err error, untouched func() bool, names ...string) {
	t.Helper()
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 1, err.Error()
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(names...), assure.NoSideEffect(untouched))
}

// bytesStill reports whether path still holds want.
func bytesStill(path string, want []byte) func() bool {
	return func() bool {
		got, err := os.ReadFile(path)
		return err == nil && string(got) == string(want)
	}
}

// absent reports whether nothing is at path.
func absent(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// elsewhere writes a real prefs.js in its own folder, for a link to point at, and returns
// its path and bytes.
func elsewhere(t *testing.T) (string, []byte) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "prefs.js")
	if err := os.WriteFile(target, []byte(onePrefs), 0o600); err != nil {
		t.Fatal(err)
	}
	return target, []byte(onePrefs)
}

// overCap makes path a sparse file one byte over the prefs.js bound.
func overCap(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(prefsMaxBytes + 1); err != nil {
		t.Skip("no sparse files here:", err)
	}
}

// covers: MA-1195, R25
// prefs.js is read as a planted file may be (C0c, foldcheck M1): every backup of an IMAP
// store will read it, so a link at its name is refused, never followed, and a file over
// its 16 MB bound is refused, never read whole. A refusal gives no account (which the
// completeness check reads as "can't tell"), and the link's target is untouched. Positive
// twin first: a healthy prefs.js gives the account.
func TestFindAccountReadsPrefsJsAsAPlantedFileMayBe(t *testing.T) {
	healthy := profileWith(t, onePrefs, "imap.gmail.com")
	if got := accountKey(t, healthy, "imap.gmail.com"); got != "server1" {
		t.Fatalf("the positive twin = %s, want server1", got)
	}

	t.Run("a link", func(t *testing.T) {
		target, before := elsewhere(t)
		p := t.TempDir()
		store := filepath.Join(p, "ImapMail", "imap.gmail.com")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(p, "prefs.js")); err != nil {
			t.Skip("no links here:", err)
		}
		acct, err := FindAccountForStore(p, store)
		if acct != nil {
			t.Fatalf("an account was read through a link: %+v", acct)
		}
		refusedNaming(t, err, bytesStill(target, before), "prefs.js", "a link")
	})

	t.Run("over its bound", func(t *testing.T) {
		p := t.TempDir()
		store := filepath.Join(p, "ImapMail", "imap.gmail.com")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatal(err)
		}
		overCap(t, filepath.Join(p, "prefs.js"))
		acct, err := FindAccountForStore(p, store)
		if acct != nil {
			t.Fatalf("an account was read from an over-cap prefs.js: %+v", acct)
		}
		refusedNaming(t, err, func() bool {
			fi, err := os.Stat(filepath.Join(p, "prefs.js"))
			return err == nil && fi.Size() == prefsMaxBytes+1
		}, "prefs.js", "bound")
	})
}

// covers: MA-1195, R9, S10
// EnableOffline reads prefs.js through the same opener and never writes through a link
// (C0c, completeness foldcheck2 L1). A link, or a file over the bound, at prefs.js is
// refused with nothing written: no backup, the link's target unchanged. A link, or a
// folder, where the backup goes is refused before prefs.js is changed, and the link's
// target is unchanged. Positive twin first: a healthy prefs.js is backed up as it was
// and turned on, and both files are plain files.
func TestEnableOfflineNeverWritesThroughALink(t *testing.T) {
	healthy := profileWith(t, onePrefs, "imap.gmail.com")
	c, err := EnableOffline(healthy, "server1")
	if err != nil || !c.Changed {
		t.Fatalf("the positive twin: changed %v, err %v", c.Changed, err)
	}
	if b, err := os.ReadFile(assure.Reached(t, c.Backup, "the backup path")); err != nil || string(b) != onePrefs {
		t.Fatalf("the positive twin's backup is not the old prefs.js (%v):\n%s", err, b)
	}
	now, _ := os.ReadFile(filepath.Join(healthy, "prefs.js"))
	if !strings.Contains(string(now), `user_pref("mail.server.server1.offline_download", true);`) {
		t.Fatalf("the positive twin was not turned on:\n%s", now)
	}
	for _, f := range []string{"prefs.js", "prefs.js.mailarchive-bak"} {
		if fi, err := os.Lstat(filepath.Join(healthy, f)); err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("the positive twin's %s is not a plain file: %v %v", f, fi, err)
		}
	}

	t.Run("a link at prefs.js", func(t *testing.T) {
		target, before := elsewhere(t)
		p := t.TempDir()
		link := filepath.Join(p, "prefs.js")
		if err := os.Symlink(target, link); err != nil {
			t.Skip("no links here:", err)
		}
		_, err := EnableOffline(p, "server1")
		refusedNaming(t, err, func() bool {
			fi, lerr := os.Lstat(link)
			return bytesStill(target, before)() && absent(link+".mailarchive-bak") &&
				lerr == nil && fi.Mode()&fs.ModeSymlink != 0
		}, "prefs.js", "a link")
	})

	t.Run("a link where the backup goes", func(t *testing.T) {
		target, before := elsewhere(t)
		p := profileWith(t, onePrefs, "imap.gmail.com")
		if err := os.Symlink(target, filepath.Join(p, "prefs.js.mailarchive-bak")); err != nil {
			t.Skip("no links here:", err)
		}
		_, err := EnableOffline(p, "server1")
		refusedNaming(t, err, func() bool {
			return bytesStill(target, before)() && bytesStill(filepath.Join(p, "prefs.js"), []byte(onePrefs))()
		}, "prefs.js.mailarchive-bak", "a link")
	})

	t.Run("a folder where the backup goes", func(t *testing.T) {
		p := profileWith(t, onePrefs, "imap.gmail.com")
		if err := os.Mkdir(filepath.Join(p, "prefs.js.mailarchive-bak"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := EnableOffline(p, "server1")
		refusedNaming(t, err, bytesStill(filepath.Join(p, "prefs.js"), []byte(onePrefs)), "prefs.js.mailarchive-bak")
	})

	t.Run("a link swapped in after the check", func(t *testing.T) {
		// The moment between the Lstat and the write: the write is a rename over the name,
		// so a link planted then is replaced, and its target never written.
		target, before := elsewhere(t)
		if err := os.Symlink(target, filepath.Join(t.TempDir(), "probe")); err != nil {
			t.Skip("no links here:", err)
		}
		p := profileWith(t, onePrefs, "imap.gmail.com")
		prefsPath := filepath.Join(p, "prefs.js")
		swapped := 0
		util.BeforeAtomicRename = func(tmp string) {
			// Only at prefs.js's own write, which follows the backup's.
			if filepath.Dir(tmp) != p || swapped > 0 || absent(prefsPath+".mailarchive-bak") {
				return
			}
			if os.Remove(prefsPath) == nil && os.Symlink(target, prefsPath) == nil {
				swapped++
			}
		}
		t.Cleanup(func() { util.BeforeAtomicRename = func(string) {} })
		if _, err := EnableOffline(p, "server1"); err != nil {
			t.Fatalf("EnableOffline: %v", err)
		}
		if assure.Reached(t, swapped, "the swap in the moment before the rename") > 0 && !bytesStill(target, before)() {
			t.Fatal("the write went through a link swapped in after the check")
		}
		if fi, err := os.Lstat(prefsPath); err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("prefs.js is not a plain file after the write: %v %v", fi, err)
		}
	})

	t.Run("over its bound", func(t *testing.T) {
		p := t.TempDir()
		overCap(t, filepath.Join(p, "prefs.js"))
		_, err := EnableOffline(p, "server1")
		refusedNaming(t, err, func() bool {
			fi, serr := os.Stat(filepath.Join(p, "prefs.js"))
			return serr == nil && fi.Size() == prefsMaxBytes+1 && absent(filepath.Join(p, "prefs.js.mailarchive-bak"))
		}, "prefs.js", "bound")
	})
}
