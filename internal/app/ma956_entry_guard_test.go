package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// The fixes to the emptied-folder guard from slice 2's adversarial pass
// (design-archive-delete §7: "a run that checked before a delete and got the lock after
// it is refused"; findings del-s2-A1-01, A2-01, A1-07, A2-05, A3-04, A2-04, A3-03).

// ma956OwnNames are a scheduled run-job's own files in its archive folder: the files it
// holds open, or writes, before it takes the archive lock (the run log, the scheduler's
// stderr file, -outlook's PST export). On Windows a delete cannot remove a file held open.
var ma956OwnNames = []string{"mailarchive.log", "mailarchive.stderr.log", "_outlook-pst"}

func ma956OwnPaths(out string) []string {
	var p []string
	for _, n := range ma956OwnNames {
		p = append(p, filepath.Join(out, n))
	}
	return p
}

// ma956WriteOwn writes the run's own files into out, as a run-job does before its lock.
func ma956WriteOwn(t *testing.T, out string) {
	t.Helper()
	ma956Write(t, filepath.Join(out, "mailarchive.log"), "a line\n")
	ma956Write(t, filepath.Join(out, "mailarchive.stderr.log"), "")
	if err := os.MkdirAll(filepath.Join(out, "_outlook-pst"), 0o700); err != nil {
		t.Fatal(err)
	}
	ma956Write(t, filepath.Join(out, "_outlook-pst", "Mailbox.pst"), "the mailbox")
}

// ma956DeleteKeeping is a delete that finished at the moment of the call and could not
// remove keep (on Windows: files the run holds open): everything else in out is removed,
// the folder is kept.
func ma956DeleteKeeping(t *testing.T, out string, keep ...string) {
	t.Helper()
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !slices.Contains(keep, e.Name()) {
			if err := os.RemoveAll(filepath.Join(out, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func ma956RefusedGoneArchive(t *testing.T, err error, out string, kept func() bool) {
	t.Helper()
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names(out, "held a MailArchive archive when this backup started", "holds none now", "remove its scheduled backup"),
		assure.NoSideEffect(kept))
	if k := FailureKind(err); k != state.FailArchiveAbsent {
		t.Errorf("an archive gone since the run's first check is refused as %s, got kind %q", state.FailArchiveAbsent, k)
	}
}

// covers: MA-956, R36
// A run that found an archive at its first check, and finds none under its lock, is
// refused, whatever is left in the folder: the run's own files a delete could not remove
// (Windows: held open), or nothing at all. The own-file exemption is only for a run that
// found no archive (a true first run). Both engine paths, and a caller's own first check
// (Options.ArchiveAtEntry) made before its pre-lock work. Positive twins first.
func TestMA956_ArchiveSeenThenGoneIsRefusedUnderTheLock(t *testing.T) {
	in := ma956Input(t)
	run := func(out string, atEntry bool) error {
		_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental,
			Unattended: true, OwnFiles: ma956OwnPaths(out), ArchiveAtEntry: atEntry}, discard(), nil)
		return err
	}
	ownOnly := func(out string) func() bool {
		return func() bool {
			left := ma956List(t, out)
			left = slices.DeleteFunc(left, func(n string) bool { return n == lockfile.Name })
			return slices.Equal(left, []string{"_outlook-pst", "mailarchive.log", "mailarchive.stderr.log"}) &&
				ma956Exists(filepath.Join(out, "_outlook-pst", "Mailbox.pst"))
		}
	}

	// Positive twins: a first run into a folder holding only its own files (no archive at
	// any check), and an archive left alone, still work.
	first := t.TempDir()
	ma956WriteOwn(t, first)
	if err := run(first, false); err != nil {
		t.Fatalf("a first run beside its own files must work: %v", err)
	}
	assure.Reached(t, ma956Exists(filepath.Join(first, ma956Manifest)), "the first run's archive")
	ma956SeamLock(t, func() {})
	if err := run(first, true); err != nil {
		t.Fatalf("an archive left alone, seen at entry, must work: %v", err)
	}

	// Run: the archive is there at the run's checks; a delete that cannot remove the run's own
	// files finishes before the lock.
	out := t.TempDir()
	if err := ma956Run(t, in, out, false); err != nil {
		t.Fatal(err)
	}
	ma956WriteOwn(t, out)
	ma956SeamLock(t, func() { ma956DeleteKeeping(t, out, ma956OwnNames...) })
	ma956RefusedGoneArchive(t, run(out, false), out, ownOnly(out))

	// Run, the caller's first check: the archive was there when the caller (run-job, the CLI)
	// checked, and was deleted during its pre-lock work, before app.Run's first check.
	caller := t.TempDir()
	ma956WriteOwn(t, caller)
	ma956SeamLock(t, func() {})
	ma956RefusedGoneArchive(t, run(caller, true), caller, ownOnly(caller))

	// Run: a delete that left the folder empty (kept, say, because it was busy for a moment).
	emptied := t.TempDir()
	if err := ma956Run(t, in, emptied, false); err != nil {
		t.Fatal(err)
	}
	ma956SeamLock(t, func() { ma956DeleteKeeping(t, emptied) })
	ma956RefusedGoneArchive(t, ma956Run(t, in, emptied, true), emptied, func() bool {
		left := ma956List(t, emptied)
		return len(left) == 0 || slices.Equal(left, []string{lockfile.Name})
	})

	// RunGraph: the archive is there when the capture starts (before its sign-in) and is
	// deleted, all but the run's own files, before its lock.
	_, srv := newFakeDelegServer()
	defer srv.Close()
	capture := func(out string, atEntry bool) error {
		cache := filepath.Join(t.TempDir(), "primed.json")
		writePrimedCache(t, cache)
		g := delegOpts(srv, cache)
		g.Unattended = true
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Unattended: true,
			OwnFiles: ma956OwnPaths(out), ArchiveAtEntry: atEntry}, discardLogger())
		return err
	}
	ma956SeamLock(t, func() {})
	firstCapture := t.TempDir()
	ma956WriteOwn(t, firstCapture)
	if err := capture(firstCapture, false); err != nil {
		t.Fatalf("a first capture beside its own files must work: %v", err)
	}
	captured := t.TempDir()
	if err := capture(captured, false); err != nil {
		t.Fatal(err)
	}
	ma956WriteOwn(t, captured)
	ma956SeamLock(t, func() { ma956DeleteKeeping(t, captured, ma956OwnNames...) })
	ma956RefusedGoneArchive(t, capture(captured, false), captured, ownOnly(captured))
	ma956SeamLock(t, func() {})
	callerCapture := t.TempDir()
	ma956WriteOwn(t, callerCapture)
	ma956RefusedGoneArchive(t, capture(callerCapture, true), callerCapture, ownOnly(callerCapture))

	// RunGraph: the delete finishes during the sign-in (its first request), the capture's
	// one long wait before its lock; RunGraph saw the archive before it signed in.
	signIn := t.TempDir()
	if err := capture(signIn, false); err != nil {
		t.Fatal(err)
	}
	ma956WriteOwn(t, signIn)
	cache := filepath.Join(t.TempDir(), "primed.json")
	writePrimedCache(t, cache)
	g := delegOpts(srv, cache)
	g.Unattended = true
	var once bool
	g.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if !once {
				once = true
				ma956DeleteKeeping(t, signIn, ma956OwnNames...)
			}
			return rt.RoundTrip(r)
		})
	}
	_, err := RunGraph(context.Background(), g, Options{Out: signIn, Mode: export.Incremental, Unattended: true, OwnFiles: ma956OwnPaths(signIn)}, discardLogger())
	assure.Reached(t, once, "the sign-in's first request")
	ma956RefusedGoneArchive(t, err, signIn, ownOnly(signIn))
}

// covers: MA-956, R36
// Whether a folder holds an archive is the delete design's test (§4 [NEW-isarchive-cap]):
// the manifest is a regular, non-empty file reached without a link. A link named as the
// manifest (to a manifest elsewhere), an empty file and a folder of that name are not an
// archive, so the person's files beside them are refused. Positive twin first.
func TestMA956_ArchiveTestIsTheDeleteDesigns(t *testing.T) {
	ok := ma956PersonsFolder(t)
	ma956Write(t, filepath.Join(ok, ma956Manifest), `{"version":3}`)
	if err := RefuseEmptiedFolder(ok, ""); err != nil {
		t.Fatalf("a regular manifest beside the person's files must pass: %v", err)
	}
	assure.Reached(t, ArchiveHere(ok, ""), "a regular manifest is an archive")

	cases := map[string]func(t *testing.T, out string){
		"an empty manifest": func(t *testing.T, out string) { ma956Write(t, filepath.Join(out, ma956Manifest), "") },
		"a folder named as the manifest": func(t *testing.T, out string) {
			if err := os.Mkdir(filepath.Join(out, ma956Manifest), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"a link to a manifest elsewhere": func(t *testing.T, out string) {
			elsewhere := filepath.Join(t.TempDir(), ma956Manifest)
			ma956Write(t, elsewhere, `{"version":3}`)
			if err := os.Symlink(elsewhere, filepath.Join(out, ma956Manifest)); err != nil {
				if runtime.GOOS == "windows" {
					t.Skip("making a link needs a privilege on Windows: ", err)
				}
				t.Fatal(err)
			}
		},
		"a link to a descriptor elsewhere": func(t *testing.T, out string) {
			elsewhere := filepath.Join(t.TempDir(), schedule.DescriptorName)
			ma956Write(t, elsewhere, "{}")
			if err := os.Symlink(elsewhere, filepath.Join(out, schedule.DescriptorName)); err != nil {
				if runtime.GOOS == "windows" {
					t.Skip("making a link needs a privilege on Windows: ", err)
				}
				t.Fatal(err)
			}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			out := ma956PersonsFolder(t)
			mk(t, out)
			if ArchiveHere(out, "") {
				t.Fatal("not an archive by the delete design's test")
			}
			before := ma956List(t, out)
			ma956RefusedEmptied(t, RefuseEmptiedFolder(out, ""), out, func() bool { return slices.Equal(before, ma956List(t, out)) })
		})
	}
}

// covers: MA-956, R36
// The run's own files are matched to the folder's names by identity, not by lower-cased
// text: an own path given in another spelling of the same file (another case where the
// file system ignores case, as on Windows and macOS) is the run's own; where the file
// system tells cases apart (Linux), a person's file named as the run's log in another case
// is the person's. The folder of an own path is matched by identity too (a link to it).
func TestMA956_OwnFilesMatchedByIdentity(t *testing.T) {
	// The folder named through a link to it: still the folder's own log.
	real := t.TempDir()
	ma956Write(t, filepath.Join(real, "mailarchive.log"), "a line\n")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, alias); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
	} else if err := RefuseEmptiedFolder(real, "", filepath.Join(alias, "mailarchive.log")); err != nil {
		t.Fatalf("the run's log named through a link to its folder must pass: %v", err)
	}

	out := t.TempDir()
	ma956Write(t, filepath.Join(out, "mailarchive.log"), "a line\n")
	_, err := os.Lstat(filepath.Join(out, "MAILARCHIVE.LOG"))
	ignoresCase := err == nil
	if ignoresCase {
		// Windows, macOS: the same file in another case is the run's own.
		if err := RefuseEmptiedFolder(out, "", filepath.Join(out, "MAILARCHIVE.LOG")); err != nil {
			t.Fatalf("the run's log given in another case must pass where case is ignored: %v", err)
		}
		return
	}
	// Linux: the run's log and the person's file of the same name in another case.
	if err := RefuseEmptiedFolder(out, "", filepath.Join(out, "mailarchive.log")); err != nil {
		t.Fatalf("positive twin: the run's own log must pass: %v", err)
	}
	ma956Write(t, filepath.Join(out, "MailArchive.log"), "the person's own log")
	before := ma956List(t, out)
	ma956RefusedEmptied(t, RefuseEmptiedFolder(out, "", filepath.Join(out, "mailarchive.log")), out,
		func() bool { return slices.Equal(before, ma956List(t, out)) })
	// A fixed name of MailArchive's in another case is the person's file there too.
	cased := t.TempDir()
	ma956Write(t, filepath.Join(cased, ".MailArchive-LastRun.json"), "{}")
	before = ma956List(t, cased)
	ma956RefusedEmptied(t, RefuseEmptiedFolder(cased, ""), cased, func() bool { return slices.Equal(before, ma956List(t, cased)) })
}

// covers: MA-956, R36
// PathIn: a log in the folder or below it, also named through a link to the folder, is in
// it; a log beside it, or in a folder whose name only begins like it, is not.
func TestMA956_PathIn(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "Archive")
	if err := os.MkdirAll(filepath.Join(out, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, PathIn(filepath.Join(out, "mailarchive.log"), out), "a log in the folder")
	assure.Reached(t, PathIn(filepath.Join(out, "logs", "x.log"), out), "a log below the folder")
	if alias := filepath.Join(t.TempDir(), "alias"); os.Symlink(out, alias) == nil {
		assure.Reached(t, PathIn(filepath.Join(alias, "mailarchive.log"), out), "a log named through a link to the folder")
	}
	for _, p := range []string{filepath.Join(parent, "mailarchive.log"), filepath.Join(parent, "Archive2", "x.log"), ""} {
		if PathIn(p, out) {
			t.Errorf("%q is not in %s", p, out)
		}
	}
}

// covers: MA-956, R36
// The refusal's words fit every entry point (findings del-s2-A2-04, A3-03): the cause is
// either a deleted archive or a folder of the person's own files, and the ways to make the
// first backup are the ones that exist — Archive now (Microsoft 365) and Make a new
// archive… in MailArchive Desktop, or the command line without -unattended — never the
// wizard alone (it cannot capture Microsoft 365), and the Run now a person clicked is not
// called unattended.
func TestMA956_RefusalWordsFitEveryEntryPoint(t *testing.T) {
	out := ma956PersonsFolder(t)
	before := ma956List(t, out)
	err := RefuseEmptiedFolder(out, "")
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names(out, "its archive may have been deleted, or it is a folder of your own files",
			"A scheduled backup, or Run now, does not make a new archive among them",
			"remove its scheduled backup", "Archive now", "Make a new archive…", "without -unattended", "choose an empty folder"),
		assure.Forbid("(unattended run)", "wizard", "notes.txt"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, ma956List(t, out)) }))
}

// covers: MA-956, R36
// The refusal for a folder that cannot be listed also fits the Run now a person clicked:
// it names the folder and never calls the run unattended (slice 2's re-verification, item 2).
func TestMA956_AFolderThatCannotBeListedIsNotCalledUnattended(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a folder's list is refused by permission bits only on macOS and Linux, and not to root")
	}
	out := ma956PersonsFolder(t)
	before := ma956List(t, out)
	if err := os.Chmod(out, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(out, 0o700) })
	err := RefuseEmptiedFolder(out, "")
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names(out, "cannot be listed to check that it holds an archive"),
		assure.Forbid("(unattended run)"),
		assure.NoSideEffect(func() bool {
			os.Chmod(out, 0o700)
			return slices.Equal(before, ma956List(t, out))
		}))
}
