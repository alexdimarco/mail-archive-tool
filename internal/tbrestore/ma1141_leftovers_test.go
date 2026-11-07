package tbrestore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1141, R41, S68
// Which earlier restores Desktop start, `status` and uninstall's plan name (design §7.2
// foldcheck2 note P2, foldcheck3 S7 and S10), each check a read (restores.log's bytes
// and Local Folders the same before and after):
//   - W-UNFINISHED only with no closing line, a reachable Local Folders that is still
//     its profile's (read again through §3.2's own lookup, S10), the marker present and
//     no entry of the parent name;
//   - after "abandoned", never W-UNFINISHED, but, while the folder is still there with
//     its marker and no entry of its name, the words of why it was left: W-LEFT's (also
//     for one left because an entry of its name was there and has since gone, S7), the
//     made-list's, W-COMMITERR's, or the taken name's;
//   - nothing when an entry of the parent name is there, nor for a committed restore
//     missing only its "ended", nor after "cleaned";
//   - uninstall's plan names exactly what Desktop start names, plus an open restore
//     whose Local Folders cannot be checked now, by its recorded path (S7), each with
//     "Nothing will clean it up after MailArchive is removed."
func TestRestoreLeftoversShown(t *testing.T) {
	in, msgs := sampleFolders()
	shown := func(t *testing.T, fx *fixture, forUninstall bool) []Leftover {
		t.Helper()
		logBefore, lfBefore := fx.logBytes(), fx.local()
		los, err := Leftovers(fx.log, forUninstall)
		if err != nil {
			t.Fatal(err)
		}
		if fx.logBytes() != logBefore || fx.local() != lfBefore {
			t.Fatal("naming leftovers wrote something")
		}
		return los
	}
	one := func(t *testing.T, los []Leftover, kind string, words ...string) {
		t.Helper()
		if len(los) != 1 || los[0].Kind != kind {
			t.Fatalf("want one %q, got %+v", kind, los)
		}
		mustContain(t, los[0].Words, words...)
	}

	t.Run("unfinished", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
		one(t, shown(t, fx, false), "unfinished", "did not finish", "Thunderbird does not show it", "removes what it wrote there", "'Restored — Work mail.sbd'", fx.lf.Dir)
		u := shown(t, fx, true)
		one(t, u, "unfinished", "Nothing will clean it up after MailArchive is removed.")
		// An entry of the parent name there: nothing shown.
		writeFile(t, filepath.Join(fx.lf.Dir, "Restored — Work mail"), "")
		if los := shown(t, fx, false); len(los) != 0 {
			t.Fatalf("an entry of the parent name: the person's folder now: %+v", los)
		}
	})
	t.Run("abandoned, something unlisted inside", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
		writeFile(t, filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd", "Inbox.msf"), "x")
		fx.settle(false)
		one(t, shown(t, fx, false), "left", "holds something MailArchive did not make", "delete it yourself")
	})
	t.Run("abandoned, the list unreadable", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
		lp := filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd", MadeListName)
		b, _ := os.ReadFile(lp)
		_ = os.WriteFile(lp, append([]byte("garbage\n"), b...), 0o600)
		fx.settle(false)
		one(t, shown(t, fx, false), "listbad", "could not read its list", "delete it yourself")
	})
	t.Run("abandoned, the commit not known saved", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
			if op == "sync" && name == "Restored — Work mail" {
				_ = os.WriteFile(filepath.Join(fx.lf.Dir, name), []byte("x"), 0o600)
				return errors.New("unconfirmed")
			}
			return nil
		}}
		fx.write(context.Background(), f)
		_ = os.Remove(filepath.Join(fx.lf.Dir, "Restored — Work mail"))
		one(t, shown(t, fx, false), "commiterr", "could not make sure", "restore again")
	})
	t.Run("abandoned, its name taken and gone", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
			if op == "create" && name == "Restored — Work mail" {
				return fs.ErrExist
			}
			return nil
		}}
		fx.write(context.Background(), f)
		one(t, shown(t, fx, false), "taken", "taken by something else", "delete that folder yourself")
	})
	t.Run("abandoned (3a), the entry since gone", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail.sbd/Sent"))
		writeFile(t, filepath.Join(fx.lf.Dir, "Restored — Work mail"), "")
		fx.settle(false)
		_ = os.Remove(filepath.Join(fx.lf.Dir, "Restored — Work mail"))
		one(t, shown(t, fx, false), "visible", "That folder has gone", "Thunderbird does not show the restored one now")
	})
	t.Run("committed, missing only its ended; cleaned", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		fx.writeCrashing(stepOf(t, in, msgs, "syncdir ."))
		if los := shown(t, fx, true); len(los) != 0 {
			t.Fatalf("a committed restore is never called unfinished: %+v", los)
		}
		fx2 := newFixture(t, in, msgs)
		fx2.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
		fx2.settle(false)
		if los := shown(t, fx2, true); len(los) != 0 {
			t.Fatalf("after cleaned: nothing: %+v", los)
		}
	})
	t.Run("Local Folders not this profile's now", func(t *testing.T) {
		fx := newFixture(t, in, msgs)
		fx.writeCrashing(stepOf(t, in, msgs, "create Restored — Work mail"))
		was := localFoldersOfSeam
		localFoldersOfSeam = func(string) (LocalFolders, error) { return LocalFolders{Dir: "/elsewhere"}, nil }
		defer func() { localFoldersOfSeam = was }()
		if los := shown(t, fx, false); len(los) != 0 {
			t.Fatalf("Desktop start names only a reachable Local Folders that is still the profile's: %+v", los)
		}
		u := shown(t, fx, true)
		one(t, u, "elsewhere", "may not have finished", fx.lf.Dir, "'Restored — Work mail.sbd'", "Nothing will clean it up")
		if !strings.HasSuffix(u[0].Container, "Restored — Work mail.sbd") {
			t.Fatalf("named by its recorded path: %q", u[0].Container)
		}
	})
}
