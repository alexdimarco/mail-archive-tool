package job

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// madeFixture is a settings folder not made yet, its made-list's path, and a parent folder the
// archive folders are named in (none of them is made: the list only names them).
func madeFixture(t *testing.T) (file, parent string) {
	t.Helper()
	return filepath.Join(t.TempDir(), "settings", MadeListName), filepath.Join(t.TempDir(), "Mail archives")
}

func readMadeT(t *testing.T, file string) []Made {
	t.Helper()
	ms, err := ReadMade(file)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// covers: MA-1500
// The made-list (docs/design-per-mailbox-archives.md §10, C10, C25): MadeListFile is
// wizard-made.txt in MailArchive's settings folder. RecordMade writes one line per folder, the
// folder's absolute path, a tab and its mailbox's folder name, owner-only (0600), making the
// settings folder (0700) and the lock file beside it, and nothing in the archive folder. An
// identical line writes nothing (the file is not replaced); another folder is a new file renamed
// over the old one (a hard link to the old file keeps the old bytes); the same folder for another
// mailbox replaces its line, so a folder is never listed twice.
func TestTheMadeListRecordsEachFolderOnceOwnerOnlyByRename(t *testing.T) {
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if f, err := MadeListFile(); err != nil || f != filepath.Join(d, "wizard-made.txt") {
		t.Fatalf("the made-list is %q (%v), want wizard-made.txt in the settings folder %s", f, err, d)
	}
	file, parent := madeFixture(t)
	a := filepath.Join(parent, "qzsam.walker@gmail.com")
	if err := RecordMade(file, a, "imap.gmail.com"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != a+"\timap.gmail.com\n" {
		t.Fatalf("the made-list holds %q (%v)", data, err)
	}
	if got := readMadeT(t, file); len(got) != 1 || got[0] != (Made{Path: a, Name: "imap.gmail.com"}) {
		t.Fatalf("read back: %+v", got)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(file)
		di, _ := os.Stat(filepath.Dir(file))
		if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
			t.Errorf("the made-list is %v in a folder %v, want 0600 in 0700", fi.Mode().Perm(), di.Mode().Perm())
		}
	}
	if fi, err := os.Lstat(MadeLockPath(file)); err != nil || !fi.Mode().IsRegular() || filepath.Base(MadeLockPath(file)) != "wizard-made.lock" {
		t.Errorf("the lock file beside it: %v", err)
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Errorf("recording a folder made something in its parent (%v)", err)
	}

	// An identical line writes nothing: the very same file is still there.
	before, _ := os.Stat(file)
	if err := RecordMade(file, a+string(filepath.Separator), "imap.gmail.com"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(file); !os.SameFile(before, after) {
		t.Error("recording the same line again replaced the file")
	}

	// Another folder: a new file renamed over the old one, never the old one written in place.
	kept := filepath.Join(filepath.Dir(file), "old-made-list")
	if err := os.Link(file, kept); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(parent, "Local Folders")
	if err := RecordMade(file, b, "Local Folders"); err != nil {
		t.Fatal(err)
	}
	if old, _ := os.ReadFile(kept); string(old) != a+"\timap.gmail.com\n" {
		t.Errorf("the old file was written in place: %q", old)
	}
	if got := readMadeT(t, file); len(got) != 2 || got[1] != (Made{Path: b, Name: "Local Folders"}) {
		t.Fatalf("after a second folder: %+v", got)
	}

	// The same folder for another mailbox: one line for it, the newest.
	if err := RecordMade(file, a, "imap.gmail-1.com"); err != nil {
		t.Fatal(err)
	}
	if got := readMadeT(t, file); len(got) != 2 || got[0].Path != b || got[1] != (Made{Path: a, Name: "imap.gmail-1.com"}) {
		t.Fatalf("a folder recorded again: %+v", got)
	}
	if strings.Count(mustRead(t, file), a+"\t") != 1 {
		t.Errorf("a folder is listed twice:\n%s", mustRead(t, file))
	}
}

func mustRead(t *testing.T, file string) string {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// covers: MA-1500
// The made-list keeps at most 200 lines and 64 KB, the oldest dropped and the newest kept; a line
// that alone is over 64 KB, a path that is not absolute and a path or name holding a control
// character are refused with words naming the list, and leave it as it was.
func TestTheMadeListKeepsTheNewest200LinesAnd64KB(t *testing.T) {
	file, parent := madeFixture(t)
	var full []Made
	for i := 0; i < MadeMaxLines; i++ {
		full = append(full, Made{Path: filepath.Join(parent, fmt.Sprintf("mailbox-%03d", i)), Name: "n"})
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, encodeMade(full), 0o600); err != nil {
		t.Fatal(err)
	}
	newest := filepath.Join(parent, "mailbox-new")
	if err := RecordMade(file, newest, "n"); err != nil {
		t.Fatal(err)
	}
	got := readMadeT(t, file)
	if len(got) != MadeMaxLines || got[0] != full[1] || got[len(got)-1].Path != newest {
		t.Fatalf("200 lines and one more: %d lines, first %+v, last %+v", len(got), got[0], got[len(got)-1])
	}

	// By size: lines of about a kilobyte, so the 64 KB bound is reached before 200 lines.
	file2, _ := madeFixture(t)
	long := func(i int) string { return filepath.Join(parent, fmt.Sprintf("%04d-%s", i, strings.Repeat("x", 1000))) }
	for i := 0; i < 70; i++ {
		if err := RecordMade(file2, long(i), "n"); err != nil {
			t.Fatal(err)
		}
	}
	data := mustRead(t, file2)
	got = readMadeT(t, file2)
	if len(data) > MadeMaxBytes || len(got) >= 70 || got[len(got)-1].Path != long(69) || got[0].Path == long(0) {
		t.Fatalf("by size: %d bytes, %d lines, last %.40q", len(data), len(got), got[len(got)-1].Path)
	}
	if len(data)+len(long(70))+3 <= MadeMaxBytes {
		t.Errorf("more was dropped than the bound needs: %d bytes kept", len(data))
	}

	unchanged := func() bool { return mustRead(t, file2) == data }
	for what, rec := range map[string][2]string{
		"a line over 64 KB":     {filepath.Join(parent, strings.Repeat("y", MadeMaxBytes)), "n"},
		"a relative path":       {filepath.Join("Mail archives", "qz"), "n"},
		"a newline in the path": {filepath.Join(parent, "qz\n/forged"), "n"},
		"a tab in the name":     {filepath.Join(parent, "qz"), "imap\tforged"},
	} {
		err := RecordMade(file2, rec[0], rec[1])
		rc := 0
		if err != nil {
			rc = 1
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		t.Run(what, func(t *testing.T) {
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names("the wizard's list of the archive folders it made"),
				assure.NoSideEffect(unchanged))
		})
	}
}

// covers: MA-1500
// The made-list is read through the secure regular-file reader inside an os.Root on the settings
// folder (F-06): a line with a control character other than its one tab, one with no tab or a
// second tab, and one whose path is not absolute are skipped, the others kept as written (letters
// outside ASCII included); a link at its name is refused, its target never read; a folder at its
// name is refused; a file over 64 KB is refused, never read whole; a list or a settings folder
// that is not there is an empty list.
func TestTheMadeListReaderSkipsBadLinesAndRefusesWhatIsNotAFile(t *testing.T) {
	file, parent := madeFixture(t)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(parent, "qzsam.walker@gmail.com")
	letters := filepath.Join(parent, "Søren")
	lines := []string{
		good + "\timap.gmail.com",
		"relative/qz\tn",
		filepath.Join(parent, "notab"),
		filepath.Join(parent, "twotabs") + "\tna\tme",
		filepath.Join(parent, "esc\x1b[31m") + "\tn",
		filepath.Join(parent, "cr") + "\tn\r",
		filepath.Join(parent, "c1") + "\tn\u0085",
		filepath.Join(parent, "nul\x00") + "\tn",
		letters + "\tSøren",
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readMadeT(t, file)
	want := []Made{{Path: good, Name: "imap.gmail.com"}, {Path: letters, Name: "Søren"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("the lines kept: %+v, want %+v", got, want)
	}

	if ms, err := ReadMade(filepath.Join(t.TempDir(), "absent", MadeListName)); err != nil || ms != nil {
		t.Errorf("no settings folder: %v %v", ms, err)
	}
	if ms, err := ReadMade(filepath.Join(t.TempDir(), MadeListName)); err != nil || ms != nil {
		t.Errorf("no list: %v %v", ms, err)
	}

	refused := func(t *testing.T, f, needle string) {
		t.Helper()
		ms, err := ReadMade(f)
		rc, msg := 0, ""
		if err != nil {
			rc, msg = 1, err.Error()
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("the wizard's list of the archive folders it made", f, needle),
			assure.NoSideEffect(func() bool { return ms == nil }))
	}
	t.Run("over 64 KB", func(t *testing.T) {
		big := filepath.Join(t.TempDir(), MadeListName)
		data := bytes.Repeat([]byte(good+"\tn\n"), MadeMaxBytes/len(good)+1)
		if err := os.WriteFile(big, data, 0o600); err != nil {
			t.Fatal(err)
		}
		refused(t, big, "65536-byte bound")
	})
	t.Run("a folder at its name", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), MadeListName)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		refused(t, dir, "not a regular file")
	})
	t.Run("a link at its name", func(t *testing.T) {
		settings := t.TempDir()
		target := filepath.Join(settings, "elsewhere.txt")
		if err := os.WriteFile(target, []byte(good+"\tlinked\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(settings, MadeListName)
		// A link inside the settings folder, which the os.Root alone would follow.
		if err := os.Symlink(filepath.Base(target), link); err != nil {
			t.Skipf("this computer cannot make a link here: %v", err)
		}
		refused(t, link, "a link")
	})
}

// covers: MA-1500
// StrikeMade removes exactly the lines of the folders it is given (spelled as filepath.Clean
// spells them) and keeps the others in their order; a list naming none of them is not written;
// with no settings folder nothing is made, neither the folder nor the lock file.
func TestStrikeMadeRemovesOnlyTheNamedFolders(t *testing.T) {
	file, parent := madeFixture(t)
	a, b, c := filepath.Join(parent, "a"), filepath.Join(parent, "b"), filepath.Join(parent, "c")
	for _, p := range []string{a, b, c} {
		if err := RecordMade(file, p, filepath.Base(p)); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := os.Stat(file)
	if err := StrikeMade(file, []string{filepath.Join(parent, "other"), "", " "}); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(file); !os.SameFile(before, after) {
		t.Error("a strike naming no listed folder replaced the file")
	}
	if err := StrikeMade(file, []string{b + string(filepath.Separator)}); err != nil {
		t.Fatal(err)
	}
	if got := readMadeT(t, file); len(got) != 2 || got[0].Path != a || got[1].Path != c {
		t.Fatalf("after striking b: %+v", got)
	}

	none := filepath.Join(t.TempDir(), "settings", MadeListName)
	if err := StrikeMade(none, []string{a}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Dir(none)); !os.IsNotExist(err) {
		t.Errorf("a strike with no settings folder made it (%v)", err)
	}
}

// covers: MA-1500
// Every change of the made-list takes its lock (wizard-made.lock): while another program holds it
// past the wait, a record and a strike are refused with words naming the list and saying to try
// again, and the list is left as it was. Positive twin first: with the lock free both change it.
func TestAChangeOfTheMadeListIsRefusedWhileItsLockIsHeld(t *testing.T) {
	orig := madeLockWait
	t.Cleanup(func() { madeLockWait = orig })
	madeLockWait = 200 * time.Millisecond
	file, parent := madeFixture(t)
	a, b := filepath.Join(parent, "a"), filepath.Join(parent, "b")
	if err := RecordMade(file, a, "a"); err != nil {
		t.Fatalf("the positive twin, a record: %v", err)
	}
	if err := RecordMade(file, b, "b"); err != nil {
		t.Fatal(err)
	}
	if err := StrikeMade(file, []string{b}); err != nil {
		t.Fatalf("the positive twin, a strike: %v", err)
	}
	data := mustRead(t, file)
	held, err := lockfile.Acquire(MadeLockPath(file))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	for what, change := range map[string]func() error{
		"a record": func() error { return RecordMade(file, b, "b") },
		"a strike": func() error { return StrikeMade(file, []string{a}) },
	} {
		err := change()
		var busy *MadeBusyError
		rc, msg := 0, ""
		if errors.As(err, &busy) {
			rc, msg = 1, err.Error()
		}
		t.Run(what, func(t *testing.T) {
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(file, "another MailArchive window", "try again"),
				assure.NoSideEffect(func() bool { return mustRead(t, file) == data }))
		})
	}
}
