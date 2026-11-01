package schedule

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func onWindows() bool { return runtime.GOOS == "windows" }

// covers: MA-973
// A delete's discovery reads back which folder an entry this tool installed backs up, where
// the host keeps it (design-archive-delete §5.1): on Windows the wrapper's run line, or, for
// a direct task (no wrapper, the common case), the -out in the Arguments of the XML
// definition written beside it — UTF-16LE with a byte-order mark, split by the C-runtime
// rules winQuote quotes for, so a folder with spaces, quotes and trailing backslashes reads
// back whole; on Linux the managed crontab block; macOS cannot read a launchd entry back
// (UJ-6). An entry with no side-file, or one that runs a -job file, is not known.
func TestInstalledOutReadsTheFolderAnEntryBacksUp(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	outs := []string{`C:\Mail Box\archive`, `D:\100%\a&b`, `E:\`, `\\nas\backup\`, `C:\Users\Jörg\Eingänge`, `C:\odd "quoted" name\x`}
	for i, out := range outs {
		// A direct task: only the XML definition is beside the wrapper path.
		s := backupSpecFor(out, "direct-ma973-"+string(rune('a'+i)))
		s.WrapperPath = DefaultWrapperPath(s.Name)
		doc, err := SchtasksXML(s, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := writeFileAtomic(SchtasksXMLPath(s.WrapperPath), doc); err != nil {
			t.Fatal(err)
		}
		got, known := installedOutOn(s.Name, "windows")
		if !known || got != out {
			t.Errorf("a direct task backing up %q reads back as %q (known %v)", out, got, known)
		}
		// A wrapped task: its run line says it (a quote, which no Windows file name holds, is
		// written as an apostrophe there, batchQuote).
		if strings.Contains(out, `"`) {
			continue
		}
		w := wrapperSpecFor(out, "wrapped-ma973-"+string(rune('a'+i)))
		if err := writeWrapper(w); err != nil {
			t.Fatal(err)
		}
		if got, known := installedOutOn(w.Name, "windows"); !known || got != out {
			t.Errorf("a wrapped task backing up %q reads back as %q (known %v)", out, got, known)
		}
	}
	if got, known := installedOutOn("never-installed-ma973", "windows"); known || got != "" {
		t.Errorf("an entry with no side-file is known: %q", got)
	}
	// A GUI task runs a -job file: the side-files hold no -out.
	j := Spec{Name: "job-ma973", Interval: Daily, At: "02:00", Exe: `C:\p\mailarchive.exe`, Args: []string{"run-job", "-job", `C:\cfg\job-ma973.json`}}
	j.WrapperPath = DefaultWrapperPath(j.Name)
	doc, _ := SchtasksXML(j, time.Now())
	if err := writeFileAtomic(SchtasksXMLPath(j.WrapperPath), doc); err != nil {
		t.Fatal(err)
	}
	if got, known := installedOutOn(j.Name, "windows"); known {
		t.Errorf("a -job task reads back a folder: %q", got)
	}

	// Linux: the managed crontab block (its seams are sandboxed off Windows only).
	if onWindows() {
		return
	}
	tab := sandboxHostScheduler(t)
	cs := backupSpecFor("/srv/mail archive/a", "cron-ma973")
	line, err := CronLine(cs)
	if err != nil {
		t.Fatal(err)
	}
	*tab = "# other job\n0 1 * * * true\n" + CronMarker(cs.Name) + "\n" + line + "\n"
	if got, known := installedOutOn(cs.Name, "linux"); !known || got != "/srv/mail archive/a" {
		t.Errorf("a cron entry reads back as %q (known %v)", got, known)
	}
	if got, known := installedOutOn("cron-other-ma973", "linux"); known {
		t.Errorf("an entry with no block reads back as %q", got)
	}
	if got, known := installedOutOn(cs.Name, "darwin"); known {
		t.Errorf("a launchd entry reads back as %q: macOS cannot (UJ-6)", got)
	}
}

// covers: MA-973
// SplitWinArgs is the inverse of winArg: every argument a task's Arguments quote reads back
// exactly, empty ones and embedded quotes included.
func TestSplitWinArgsUndoesWinArg(t *testing.T) {
	args := []string{"-out", `C:\a b\`, `x"y`, "", `\\srv\share\`, `plain`, `tab	here`, `C:\trailing\\`}
	got := SplitWinArgs(taskArguments(args))
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("SplitWinArgs(taskArguments(%q)) = %q", args, got)
	}
	if out := argOut(SplitWinArgs(`-out=C:\x -mode full`)); out != `C:\x` {
		t.Errorf("-out=V reads as %q", out)
	}
}

// covers: MA-973
// The Windows side-files an absent entry may leave (§5.3 step 2): SideFilesExist sees either,
// RemoveSideFiles removes both and an already-gone one is no error. Off Windows there are none
// and nothing is removed.
func TestSideFilesOfAnEntry(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	name := "side-ma973"
	wrap := DefaultWrapperPath(name)
	if err := os.MkdirAll(filepath.Dir(wrap), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(SchtasksXMLPath(wrap), []byte("x"), 0o600)
	if SideFilesExist(name) != onWindows() {
		t.Fatalf("SideFilesExist = %v on this platform (true only on Windows)", SideFilesExist(name))
	}
	if err := RemoveSideFiles(name); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(SchtasksXMLPath(wrap))
	if onWindows() != os.IsNotExist(err) {
		t.Errorf("after RemoveSideFiles the XML is there: %v (removed only on Windows)", err)
	}
	if onWindows() {
		if SideFilesExist(name) {
			t.Error("the side-files are still seen after their removal")
		}
		if err := RemoveSideFiles(name); err != nil {
			t.Errorf("removing side-files already gone: %v", err)
		}
	}
}
