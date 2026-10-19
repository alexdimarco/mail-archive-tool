package schedule

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// wrapperSpecFor is a backup Spec whose Windows task runs through a batch wrapper.
func wrapperSpecFor(out, name string) Spec {
	s := backupSpecFor(out, name)
	s.Wrapper, s.WrapperPath = true, DefaultWrapperPath(name)
	return s
}

// covers: MA-455, MA-457, R14, R18, S14
// G8-AO1 (g8 adversarial pass, fresh-eyes re-check): on Windows a custom task
// name binds to an archive only when the wrapper that task runs is THIS
// archive's. Every task this tool installed has a wrapper, so "a wrapper of ours
// exists" says only that the name is one of ours, not whose: a descriptor in Y
// that names X's custom-named task would otherwise make `-out Y -remove` delete
// X's task, wrapper and XML. The wrapper is a file this tool wrote and may read
// back (UJ-6 is about the scheduler's own entries); its -out is compared as the
// cron block's is. The positive twins first: an archive's own custom-named task,
// and the same task after the archive moved (the wrapper still runs the old
// path, which is gone). Then Y's descriptor naming X's task is refused with the
// descriptor, the name, X's folder and the -name remedy, and X's wrapper stays.
func TestWindowsCustomTaskOfAnotherArchiveIsRefused(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	x, y := t.TempDir(), t.TempDir()

	own := wrapperSpecFor(y, "nightly-y-g8a5")
	if err := writeWrapper(own); err != nil {
		t.Fatal(err)
	}
	if err := nameBoundOn(y, DescribeSpec(own), "windows"); err != nil {
		t.Fatalf("an archive's own custom-named task does not bind: %v", err)
	}

	// The archive was renamed: its task's wrapper still runs the old path.
	parent := t.TempDir()
	oldDir, newDir := filepath.Join(parent, "old"), filepath.Join(parent, "new")
	moved := wrapperSpecFor(oldDir, "nightly-moved-g8a5")
	if err := writeWrapper(moved); err != nil {
		t.Fatal(err)
	}
	if err := nameBoundOn(newDir, DescribeSpec(moved), "windows"); err != nil {
		t.Fatalf("a moved archive's own custom-named task does not bind: %v", err)
	}

	// X's custom-named task, named by a descriptor in Y.
	xSpec := wrapperSpecFor(x, "nightly-x-g8a5")
	if err := writeWrapper(xSpec); err != nil {
		t.Fatal(err)
	}
	planted := DescribeSpec(backupSpecFor(y, "nightly-x-g8a5"))
	code, msg := refusal(nameBoundOn(y, planted, "windows"))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("nightly-x-g8a5", x, "-name"),
		assure.NoSideEffect(func() bool { _, err := os.Stat(xSpec.WrapperPath); return err == nil }))
}

// covers: MA-455, MA-650
// G8-AO1 for the direct (no-wrapper) install, the common Windows case after
// MA-650: the task leaves only its XML definition beside the wrapper path, never
// a .cmd. Its own custom name must still bind by that XML (so `-out X -remove`
// and the GUI's Remove find it), and a name with NEITHER a wrapper nor an XML is
// still refused as not this tool's, with the -name remedy. The owner-only
// LOCALAPPDATA directory is the same the wrapper proof always used.
func TestWindowsDirectInstallBindsByItsXML(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	y := t.TempDir()

	// Write only the XML side-file, the way installSchtasks does for a
	// normal-length command (no wrapper, no console window).
	own := backupSpecFor(y, "nightly-direct-g8a5")
	own.WrapperPath = DefaultWrapperPath(own.Name)
	xmlPath := SchtasksXMLPath(own.WrapperPath)
	if err := os.MkdirAll(filepath.Dir(xmlPath), 0o700); err != nil {
		t.Fatal(err)
	}
	doc, err := SchtasksXML(own, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(xmlPath, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(DefaultWrapperPath(own.Name)); err == nil {
		t.Fatal("a direct install must not leave a .cmd wrapper")
	}
	if err := nameBoundOn(y, DescribeSpec(own), "windows"); err != nil {
		t.Fatalf("a direct install's own custom-named task does not bind by its XML: %v", err)
	}

	// A name with no side-file at all (neither wrapper nor XML) is not this
	// tool's entry, and is refused with the -name remedy.
	code, msg := refusal(nameBoundOn(y, DescribeSpec(backupSpecFor(y, "never-installed-g8a5")), "windows"))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("never-installed-g8a5", "-name"))
}

// covers: MA-455, R14
// wrapperOut reads the archive a wrapper runs back the way CmdWrapper wrote it:
// a doubled % is one %, a doubled trailing backslash run is half as long, and
// the rem header's quoted name is not a token.
func TestWrapperOutReadsWhatCmdWrapperWrote(t *testing.T) {
	for _, out := range []string{`C:\Mail Box\archive`, `D:\100%\a&b`, `E:\`, `\\nas\backup\`, `C:\Users\Jörg\Eingänge`} {
		s := backupSpecFor(out, "nightly-rt-g8a5")
		s.Log = `C:\logs\x.log`
		if got := wrapperOutOf(CmdWrapper(s)); got != out {
			t.Errorf("wrapperOutOf(CmdWrapper(-out %q)) = %q", out, got)
		}
	}
	if got := wrapperOutOf("@echo off\r\nrem nothing to read\r\n"); got != "" {
		t.Errorf("a wrapper with no run line read as -out %q", got)
	}
}

// covers: MA-455, MA-457, R14, R18
// OPEN ENCODER (G8-AC2, R14; an owner decision). Install applies the entry
// first and the descriptor second (6a453b1: no descriptor may claim an entry
// that never loaded), so a descriptor write that fails after the entry is in
// leaves an entry nothing records. A directory planted at the descriptor path
// is refused up front (ee10d29); a MISSING -out folder is not, because an
// export job may name a folder its first run creates. The error names the entry
// and its removal, but the install still leaves the entry in. Closing it means
// refusing a missing -out for a scheduled job, or creating the folder at
// install, or rolling the entry back when the record fails: each changes what
// install does for a folder that is not there yet, and which one is the
// owner's. The encoder wants a failed install to leave no entry behind.
func TestInstallThatCannotRecordItselfLeavesNoEntryBehind(t *testing.T) {
	t.Skip("open (owner decision, G8-AC2): a missing -out folder still installs the entry before the descriptor write fails; the error names the entry, but it stays installed.")
	sandboxHostScheduler(t)
	out := filepath.Join(t.TempDir(), "no-such-folder", "archive")
	name := "nightly-missing-out-g8a5"
	err := Install(backupSpecFor(out, name))
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(name, "-remove"),
		assure.NoSideEffect(func() bool { return Query(name) != Installed }))
}

// covers: MA-455, R14, R18, S14
// OPEN ENCODER (G8-AO1, K1; an owner decision). An archive moved on this host is
// told from a copied descriptor by its old folder being gone (K1: the moved
// archive's own record and entry must stay reachable). An archive that is merely
// absent (an unplugged drive, an offline share) looks the same, so a descriptor
// in Y that records X's path and X's entry name binds while X is unplugged, and
// `-out Y -remove` deletes X's entry. The two cases differ only in whether X
// comes back, which nothing on this host can say; closing it either hides the
// moved archive's own entry (the K1 regression) or needs the owner's call on
// what a removal may do for a folder that is not there.
func TestAbsentArchivesEntryIsNotBoundFromAnotherFolder(t *testing.T) {
	t.Skip("open (owner decision, G8-AO1/K1): a gone folder is read as a moved archive, so another folder's descriptor can name its entry while it is unplugged; closing it undoes K1.")
	sandboxHostScheduler(t)
	x, y := t.TempDir(), t.TempDir()
	xName := DefaultNameFor(x)
	if err := Install(backupSpecFor(x, xName)); err != nil {
		t.Fatal(err)
	}
	if err := WriteDescriptor(y, DescribeSpec(backupSpecFor(x, xName))); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(x); err != nil { // X is unplugged
		t.Fatal(err)
	}
	_, err := RecordedBackupName(y)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(xName, "-name"),
		assure.NoSideEffect(func() bool { return Query(xName) == Installed }))
}
