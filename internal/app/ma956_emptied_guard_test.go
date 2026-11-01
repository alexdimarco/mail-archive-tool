package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// The emptied-folder guard (design-archive-delete §7, §5.3 "After step 7", §11; review
// C12, NF-3, R3:NF-R3-GONE-NOT-MA689): an unattended run never builds a new archive among
// the person's own files in a folder an archive was deleted from, and never recreates a
// folder that is gone.

const ma956Manifest = ".mailarchive-manifest.json"

// ma956PersonsFile is the person's own file a delete keeps the folder for.
var ma956PersonsFile = []byte("my own notes, not MailArchive's\n")

func ma956Input(t *testing.T) string {
	t.Helper()
	in := filepath.Join(t.TempDir(), "mail.mbox")
	ma956Write(t, in, "From a@b Mon Jan  1 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n")
	return in
}

func ma956Write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ma956Run(t *testing.T, in, out string, unattended bool) error {
	t.Helper()
	_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Unattended: unattended}, discard(), nil)
	return err
}

// ma956List is the folder's names, sorted ("" entries never).
func ma956List(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// ma956PersonsFolder is a folder holding only the person's own file.
func ma956PersonsFolder(t *testing.T) string {
	t.Helper()
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "notes.txt"), ma956PersonsFile, 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}

func ma956PersonsFileKept(out string) bool {
	b, err := os.ReadFile(filepath.Join(out, "notes.txt"))
	return err == nil && bytes.Equal(b, ma956PersonsFile)
}

func ma956Exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// ma956Delete is a delete that finished at the moment of the call: every name but the
// person's file is removed from out, and the folder is kept for it (§5.3 step 7).
func ma956Delete(t *testing.T, out string) {
	t.Helper()
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "notes.txt" {
			if err := os.RemoveAll(filepath.Join(out, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// ma956SeamLock replaces the run's lock taking with one that first does before(), as a
// delete that finishes between the run's checks and its lock would.
func ma956SeamLock(t *testing.T, before func()) {
	t.Helper()
	orig := acquireArchiveLock
	t.Cleanup(func() { acquireArchiveLock = orig })
	acquireArchiveLock = func(path, verb string) (*lockfile.Lock, error) {
		before()
		return orig(path, verb)
	}
}

func ma956RefusedEmptied(t *testing.T, err error, out string, keptOnly func() bool) {
	t.Helper()
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names(out, "holds no MailArchive archive", ma956Manifest, schedule.DescriptorName, "remove its scheduled backup", "run its first backup yourself"),
		assure.NoSideEffect(keptOnly))
	if k := FailureKind(err); k != state.FailArchiveAbsent {
		t.Errorf("an emptied folder is refused as the archive not being present (%s), got kind %q", state.FailArchiveAbsent, k)
	}
}

// covers: MA-956, R36
// The guard's rule itself: what passes (each positive twin first) and what is refused.
func TestMA956_EmptiedFolderGuardRule(t *testing.T) {
	// Passes: the cases a first run, an archive and a kept-but-empty folder are in.
	pass := map[string]func(t *testing.T) (out, manifest string, logs []string){
		"not there (left to RequireExistingOut)": func(t *testing.T) (string, string, []string) {
			return filepath.Join(t.TempDir(), "gone"), "", nil
		},
		"empty": func(t *testing.T) (string, string, []string) { return t.TempDir(), "", nil },
		"litter only": func(t *testing.T) (string, string, []string) {
			out := t.TempDir()
			for _, n := range []string{".DS_Store", "desktop.ini", "Thumbs.db", "._notes.txt"} {
				ma956Write(t, filepath.Join(out, n), "x")
			}
			return out, "", nil
		},
		"the descriptor only (a first run after an install)": func(t *testing.T) (string, string, []string) {
			out := t.TempDir()
			ma956Write(t, filepath.Join(out, schedule.DescriptorName), "{}")
			return out, "", nil
		},
		"the descriptor beside the person's files": func(t *testing.T) (string, string, []string) {
			out := ma956PersonsFolder(t)
			ma956Write(t, filepath.Join(out, schedule.DescriptorName), "{}")
			return out, "", nil
		},
		"an archive beside the person's files": func(t *testing.T) (string, string, []string) {
			out := ma956PersonsFolder(t)
			ma956Write(t, filepath.Join(out, ma956Manifest), "{}")
			return out, "", nil
		},
		"an archive whose manifest is kept elsewhere (-manifest)": func(t *testing.T) (string, string, []string) {
			m := filepath.Join(t.TempDir(), "elsewhere.json")
			ma956Write(t, m, "{}")
			return ma956PersonsFolder(t), m, nil
		},
		"an encrypted archive's key file beside other files": func(t *testing.T) (string, string, []string) {
			out := ma956PersonsFolder(t)
			ma956Write(t, filepath.Join(out, ".mailarchive-vault.json"), "{}")
			return out, "", nil
		},
		"the archive lock only (checked again under the run's lock)": func(t *testing.T) (string, string, []string) {
			out := t.TempDir()
			ma956Write(t, filepath.Join(out, lockfile.Name), "")
			return out, "", nil
		},
		"the run records an earlier refused run left (MA-646)": func(t *testing.T) (string, string, []string) {
			out := t.TempDir()
			for _, n := range []string{state.LastRunName, state.AttentionName, state.RunHistoryName, state.SourceStreakName} {
				ma956Write(t, filepath.Join(out, n), "{}")
			}
			return out, "", nil
		},
		"the Desktop's settings folder kept in it (CC16)": func(t *testing.T) (string, string, []string) {
			out := t.TempDir()
			cfg := filepath.Join(out, "MailArchive settings")
			if err := os.Mkdir(cfg, 0o700); err != nil {
				t.Fatal(err)
			}
			return out, "", []string{cfg + string(filepath.Separator)}
		},
		"the run's own log files only": func(t *testing.T) (string, string, []string) {
			out := t.TempDir()
			ma956Write(t, filepath.Join(out, "mailarchive.log"), "a line\n")
			ma956Write(t, filepath.Join(out, "mailarchive.stderr.log"), "")
			return out, "", []string{filepath.Join(out, "mailarchive.log"), filepath.Join(out, "mailarchive.stderr.log")}
		},
	}
	for name, mk := range pass {
		t.Run("passes/"+name, func(t *testing.T) {
			out, manifest, logs := mk(t)
			if err := RefuseEmptiedFolder(out, manifest, logs...); err != nil {
				t.Fatalf("must pass: %v", err)
			}
		})
	}

	// Refused: a folder of the person's files with no archive in it.
	refuse := map[string]func(t *testing.T) (out string, logs []string){
		"the person's file": func(t *testing.T) (string, []string) { return ma956PersonsFolder(t), nil },
		"the person's folder": func(t *testing.T) (string, []string) {
			out := t.TempDir()
			if err := os.Mkdir(filepath.Join(out, "Photos"), 0o700); err != nil {
				t.Fatal(err)
			}
			return out, nil
		},
		"the person's file beside litter, the lock and the run's log": func(t *testing.T) (string, []string) {
			out := ma956PersonsFolder(t)
			ma956Write(t, filepath.Join(out, ".DS_Store"), "x")
			ma956Write(t, filepath.Join(out, lockfile.Name), "")
			ma956Write(t, filepath.Join(out, "mailarchive.log"), "")
			return out, []string{filepath.Join(out, "mailarchive.log")}
		},
		"the person's file beside an earlier refused run's records": func(t *testing.T) (string, []string) {
			out := ma956PersonsFolder(t)
			ma956Write(t, filepath.Join(out, state.LastRunName), "{}")
			return out, nil
		},
		"a log file the run names in another folder": func(t *testing.T) (string, []string) {
			out := t.TempDir()
			ma956Write(t, filepath.Join(out, "mailarchive.log"), "the person's own log")
			return out, []string{filepath.Join(t.TempDir(), "mailarchive.log")}
		},
	}
	for name, mk := range refuse {
		t.Run("refused/"+name, func(t *testing.T) {
			out, logs := mk(t)
			before := ma956List(t, out)
			err := RefuseEmptiedFolder(out, "", logs...)
			ma956RefusedEmptied(t, err, out, func() bool { return slices.Equal(before, ma956List(t, out)) })
		})
	}
}

// covers: MA-956, R36
// app.Run: an unattended run refuses a folder of the person's files before it writes
// anything; a first run into an empty folder, a litter-only one or one holding only the
// descriptor still works; an archive beside the person's files and an attended run are
// unchanged.
func TestMA956_UnattendedRunRefusesEmptiedFolder(t *testing.T) {
	in := ma956Input(t)

	// Positive twins first.
	empty := t.TempDir()
	if err := ma956Run(t, in, empty, true); err != nil {
		t.Fatalf("a first unattended run into an empty folder must work: %v", err)
	}
	assure.Reached(t, ma956Exists(filepath.Join(empty, ma956Manifest)), "the first run's archive")
	litter := t.TempDir()
	ma956Write(t, filepath.Join(litter, ".DS_Store"), "x")
	ma956Write(t, filepath.Join(litter, "desktop.ini"), "x")
	if err := ma956Run(t, in, litter, true); err != nil {
		t.Fatalf("a first unattended run into a litter-only folder must work: %v", err)
	}
	desc := t.TempDir()
	ma956Write(t, filepath.Join(desc, schedule.DescriptorName), "{}")
	if err := ma956Run(t, in, desc, true); err != nil {
		t.Fatalf("a first unattended run into a folder holding only the descriptor must work: %v", err)
	}
	beside := ma956PersonsFolder(t)
	if err := ma956Run(t, in, beside, false); err != nil {
		t.Fatalf("an attended run into the person's folder is unchanged: %v", err)
	}
	if err := ma956Run(t, in, beside, true); err != nil {
		t.Fatalf("an unattended run into an archive beside the person's files must work: %v", err)
	}
	if !ma956PersonsFileKept(beside) {
		t.Fatal("the person's file beside an archive must be untouched")
	}

	// The refusal: nothing written, the person's file byte-identical.
	out := ma956PersonsFolder(t)
	err := ma956Run(t, in, out, true)
	ma956RefusedEmptied(t, err, out, func() bool {
		return slices.Equal(ma956List(t, out), []string{"notes.txt"}) && ma956PersonsFileKept(out)
	})

	// Before discovery too: a refused run's record (an earlier start failure's, MA-646) beside
	// the person's file is not written again when the input is gone as well.
	withRecord := ma956PersonsFolder(t)
	rec := filepath.Join(withRecord, state.LastRunName)
	ma956Write(t, rec, `{"status":"failed"}`)
	err = ma956Run(t, filepath.Join(t.TempDir(), "gone.mbox"), withRecord, true)
	ma956RefusedEmptied(t, err, withRecord, func() bool {
		b, _ := os.ReadFile(rec)
		return string(b) == `{"status":"failed"}` && slices.Equal(ma956List(t, withRecord), []string{state.LastRunName, "notes.txt"})
	})
}

// covers: MA-956, R36
// app.Run: the guard is checked again under the run's own lock. A run that passed its
// checks while the archive was there, and took the lock after a delete that kept the
// folder for the person's files, is refused and makes no archive; a run whose folder was
// removed in between is refused as "not present" (archive_absent, MA-689's kind), never
// "archive unwritable", and the folder is not recreated.
func TestMA956_RunRechecksUnderItsLock(t *testing.T) {
	in := ma956Input(t)

	// Positive twin: the seam alone changes nothing.
	ok := ma956PersonsFolder(t)
	if err := ma956Run(t, in, ok, false); err != nil {
		t.Fatal(err)
	}
	ma956SeamLock(t, func() {})
	if err := ma956Run(t, in, ok, true); err != nil {
		t.Fatalf("an unattended run through the lock seam must work: %v", err)
	}

	// A delete finishes between the checks and the lock; the folder is kept.
	out := ma956PersonsFolder(t)
	if err := ma956Run(t, in, out, false); err != nil {
		t.Fatal(err)
	}
	ma956SeamLock(t, func() { ma956Delete(t, out) })
	err := ma956Run(t, in, out, true)
	ma956RefusedEmptied(t, err, out, func() bool {
		left := ma956List(t, out)
		return !slices.Contains(left, ma956Manifest) && ma956PersonsFileKept(out) &&
			(slices.Equal(left, []string{"notes.txt"}) || slices.Equal(left, []string{lockfile.Name, "notes.txt"}))
	})

	// The folder is removed between the checks and the lock.
	gone := t.TempDir()
	if err := ma956Run(t, in, gone, false); err != nil {
		t.Fatal(err)
	}
	ma956SeamLock(t, func() {
		if err := os.RemoveAll(gone); err != nil {
			t.Fatal(err)
		}
	})
	err = ma956Run(t, in, gone, true)
	ma956RefusedGone(t, err, gone)
}

func ma956RefusedGone(t *testing.T, err error, gone string) {
	t.Helper()
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1), assure.Names(gone, "not present", "is the backup drive mounted"),
		assure.Forbid("panic", "goroutine", "open lock"),
		assure.NoSideEffect(func() bool { return !ma956Exists(gone) }))
	if k := FailureKind(err); k != state.FailArchiveAbsent {
		t.Errorf("a folder gone before the lock is refused as %s (MA-689), not %q", state.FailArchiveAbsent, k)
	}
}

// covers: MA-956, R36
// app.RunGraph (a scheduled Microsoft 365 capture): the same guard before the lock and
// again under it, and no folder created.
func TestMA956_UnattendedCaptureRefusesEmptiedFolder(t *testing.T) {
	_, srv := newFakeDelegServer()
	defer srv.Close()
	capture := func(out string) error {
		cache := filepath.Join(t.TempDir(), "primed.json")
		writePrimedCache(t, cache)
		g := delegOpts(srv, cache)
		g.Unattended = true
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Unattended: true}, discardLogger())
		return err
	}

	// Positive twin first: a first scheduled capture into an empty folder.
	empty := t.TempDir()
	if err := capture(empty); err != nil {
		t.Fatalf("a first scheduled capture into an empty folder must work: %v", err)
	}
	assure.Reached(t, ma956Exists(filepath.Join(empty, ma956Manifest)), "the first capture's archive")

	// Before the lock.
	out := ma956PersonsFolder(t)
	ma956RefusedEmptied(t, capture(out), out, func() bool {
		return slices.Equal(ma956List(t, out), []string{"notes.txt"}) && ma956PersonsFileKept(out)
	})

	// Under the lock: an archive beside the person's file, deleted between check and lock.
	kept := ma956PersonsFolder(t)
	ma956Write(t, filepath.Join(kept, schedule.DescriptorName), "{}") // the first run after an install
	if err := capture(kept); err != nil {
		t.Fatalf("a first scheduled capture beside the descriptor must work: %v", err)
	}
	ma956SeamLock(t, func() { ma956Delete(t, kept) })
	ma956RefusedEmptied(t, capture(kept), kept, func() bool {
		left := ma956List(t, kept)
		return !slices.Contains(left, ma956Manifest) && ma956PersonsFileKept(kept)
	})

	// Gone between check and lock: not recreated, refused as not present.
	gone := t.TempDir()
	ma956SeamLock(t, func() {
		if err := os.RemoveAll(gone); err != nil {
			t.Fatal(err)
		}
	})
	ma956RefusedGone(t, capture(gone), gone)
}

// covers: MA-956, R36
// An unattended run never creates -out (no MkdirAll), on both engine paths, so a folder
// gone between its check and its lock stays gone; an attended run still makes its folder
// (positive twin first). The seam watches the real os.MkdirAll.
func TestMA956_UnattendedNeverCreatesOut(t *testing.T) {
	var made []string
	orig := makeArchiveDir
	t.Cleanup(func() { makeArchiveDir = orig })
	makeArchiveDir = func(p string, m os.FileMode) error {
		made = append(made, p)
		return orig(p, m)
	}
	in := ma956Input(t)

	attended := filepath.Join(t.TempDir(), "new archive")
	if err := ma956Run(t, in, attended, false); err != nil {
		t.Fatalf("an attended first run makes its folder: %v", err)
	}
	assure.Reached(t, slices.Contains(made, attended), "the attended run's MkdirAll")

	made = nil
	if err := ma956Run(t, in, t.TempDir(), true); err != nil {
		t.Fatal(err)
	}
	_, srv := newFakeDelegServer()
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "primed.json")
	writePrimedCache(t, cache)
	g := delegOpts(srv, cache)
	g.Unattended = true
	if _, err := RunGraph(context.Background(), g, Options{Out: t.TempDir(), Mode: export.Incremental, Unattended: true}, discardLogger()); err != nil {
		t.Fatal(err)
	}
	if len(made) != 0 {
		t.Fatalf("an unattended run must never create -out, it called MkdirAll on %q", made)
	}
}

// covers: MA-956, R36
// Windows first: names in the folder are compared without regard to case — litter as
// Explorer writes it, and a run log given in another spelling of the folder and the file.
func TestMA956_WindowsNamesIgnoreCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows file names are case-insensitive; run on Windows (CI's Windows job, the lab)")
	}
	out := t.TempDir()
	ma956Write(t, filepath.Join(out, "DESKTOP.INI"), "x")
	ma956Write(t, filepath.Join(out, "MailArchive.LOG"), "")
	upper := strings.ToUpper(out)
	log := filepath.Join(upper, "mailarchive.log")
	if err := RefuseEmptiedFolder(upper, "", log); err != nil {
		t.Fatalf("litter and the run's log in another case must pass: %v", err)
	}
	ma956Write(t, filepath.Join(out, "notes.txt"), "mine")
	before := ma956List(t, out)
	ma956RefusedEmptied(t, RefuseEmptiedFolder(upper, "", log), upper, func() bool { return slices.Equal(before, ma956List(t, out)) })
}
