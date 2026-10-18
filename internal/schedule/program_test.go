package schedule

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"mail-archive-tool/internal/instance"
)

// covers: MA-615, R33, R18, S59
// programForSchedule chooses the program a schedule runs, for every OS, every
// install/former/other name and every reason. Non-Windows and the no-console
// build itself are self with no reason; a translocated self is ErrTranslocated
// on every OS; on Windows the console build and the former names look up the
// mailarchivew.exe sibling, taking it when SiblingQualifies holds and otherwise
// self with the failing reason. Nothing but the OS, the path and the sibling
// test decides it.
func TestMA615_ProgramForScheduleCore(t *testing.T) {
	// A sibling test that answers from a table keyed on the reason we want.
	qual := func(ok bool, r Reason) func(string, string) (bool, Reason) {
		return func(self, p string) (bool, Reason) { return ok, r }
	}
	win := func(name string) string { return "/Applications/MailArchive/" + name }

	cases := []struct {
		name       string
		goos, self string
		wrapped    bool
		qualifies  func(string, string) (bool, Reason)
		wantExe    string
		wantReason Reason
		wantErr    error
	}{
		{"linux is always self", "linux", "/usr/bin/mailarchive", false, qual(true, ReasonSibling), "/usr/bin/mailarchive", ReasonNone, nil},
		{"darwin is always self", "darwin", "/Applications/Mail Archive.app/Contents/MacOS/mailarchive", false, qual(true, ReasonSibling), "/Applications/Mail Archive.app/Contents/MacOS/mailarchive", ReasonNone, nil},
		{"windows console build takes the qualifying sibling", "windows", win("mailarchive.exe"), false, qual(true, ReasonSibling), win("mailarchivew.exe"), ReasonSibling, nil},
		{"windows former name mailarchive-desktop takes the sibling", "windows", win("mailarchive-desktop.exe"), false, qual(true, ReasonSibling), win("mailarchivew.exe"), ReasonSibling, nil},
		{"windows former name mailarchive-gui takes the sibling", "windows", win("mailarchive-gui.exe"), false, qual(true, ReasonSibling), win("mailarchivew.exe"), ReasonSibling, nil},
		{"windows no-console build itself is self, no lookup", "windows", win("mailarchivew.exe"), false, qual(true, ReasonSibling), win("mailarchivew.exe"), ReasonNone, nil},
		{"windows suffixed name is not an install name", "windows", win("mailarchivew-windows-amd64.exe"), false, qual(true, ReasonSibling), win("mailarchivew-windows-amd64.exe"), ReasonNone, nil},
		{"windows renamed copy is not an install name", "windows", win("MailArchive (1).exe"), false, qual(true, ReasonSibling), win("MailArchive (1).exe"), ReasonNone, nil},
		{"windows sibling absent falls back to self", "windows", win("mailarchive.exe"), false, qual(false, ReasonSiblingAbsent), win("mailarchive.exe"), ReasonSiblingAbsent, nil},
		{"windows sibling not regular falls back to self", "windows", win("mailarchive.exe"), false, qual(false, ReasonSiblingNotRegular), win("mailarchive.exe"), ReasonSiblingNotRegular, nil},
		{"windows sibling other owner falls back to self", "windows", win("mailarchive.exe"), false, qual(false, ReasonSiblingOtherOwner), win("mailarchive.exe"), ReasonSiblingOtherOwner, nil},
		{"windows pair out of step falls back to self", "windows", win("mailarchive.exe"), false, qual(false, ReasonPairOutOfStep), win("mailarchive.exe"), ReasonPairOutOfStep, nil},
		{"wrapped still takes the sibling while L-W2 holds", "windows", win("mailarchive.exe"), true, qual(true, ReasonSibling), win("mailarchivew.exe"), ReasonSibling, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exe, reason, err := programForSchedule(c.goos, c.self, c.wrapped, c.qualifies)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if filepath.ToSlash(exe) != filepath.ToSlash(c.wantExe) || reason != c.wantReason {
				t.Fatalf("got (%q, %q), want (%q, %q)", exe, reason, c.wantExe, c.wantReason)
			}
		})
	}

	// A translocated self is refused with no path, on every OS.
	for _, goos := range []string{"windows", "darwin", "linux"} {
		self := "/private/var/folders/aa/bb/C/AppTranslocation/DEAD-BEEF/d/Mail Archive.app/Contents/MacOS/mailarchive"
		exe, reason, err := programForSchedule(goos, self, false, qual(true, ReasonSibling))
		if !errors.Is(err, ErrTranslocated) || exe != "" || reason != ReasonNone {
			t.Errorf("%s translocated: got (%q, %q, %v), want ErrTranslocated with no path", goos, exe, reason, err)
		}
	}
}

// covers: MA-615, R33, R18, S59
// SiblingQualifies holds only for a regular file, same owner as self (file
// against file), that is the SameBuild; each failure names its reason. The
// other-owner case is the guard that must stay (prove-fail: drop the owner
// comparison → it passes wrongly), and pair_out_of_step is the SameBuild guard
// (prove-fail: drop the SameBuild call → it passes wrongly). §9.4.
func TestMA615_SiblingQualifies(t *testing.T) {
	origLstat, origFile, origSelf, origOwner := lstatFile, readFileBuildInfo, readSelfBuildInfo, instance.OwnerReader
	t.Cleanup(func() {
		lstatFile, readFileBuildInfo, readSelfBuildInfo, instance.OwnerReader = origLstat, origFile, origSelf, origOwner
	})

	tmp := t.TempDir()
	self := filepath.Join(tmp, "mailarchive.exe")
	writeReg := func(name string) string {
		p := filepath.Join(tmp, name)
		if err := os.WriteFile(p, []byte("MZ"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	writeReg("mailarchive.exe")
	good := writeReg("mailarchivew.exe")        // the qualifying sibling
	other := writeReg("mailarchivew-other.exe") // same shape, different owner
	skewed := writeReg("mailarchivew-old.exe")  // same owner, different build
	notReg := filepath.Join(tmp, "subdir")
	if err := os.Mkdir(notReg, 0o755); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(tmp, "gone.exe")

	// Same owner for self and every sibling except `other`.
	owners := map[string]string{self: "u1", good: "u1", skewed: "u1", other: "u2", notReg: "u1"}
	instance.OwnerReader = func(path string) (string, []string, error) {
		return owners[path], nil, nil
	}
	// self and `good` are the same build; `skewed` is a different one.
	theBuild := &debug.BuildInfo{GoVersion: "go1.26.0", Path: "mail-archive-tool/cmd/mailarchive", Main: debug.Module{Path: "mail-archive-tool", Version: "(devel)"}}
	readSelfBuildInfo = func() (*debug.BuildInfo, bool) { return theBuild, true }
	readFileBuildInfo = func(p string) (*debug.BuildInfo, error) {
		if p == good {
			return theBuild, nil
		}
		if p == skewed {
			return &debug.BuildInfo{GoVersion: "go1.27.2", Path: "mail-archive-tool/cmd/mailarchive-gui"}, nil
		}
		return nil, errors.New("not a Go binary")
	}

	cases := []struct {
		name   string
		p      string
		wantOK bool
		wantR  Reason
	}{
		{"a genuine same-build sibling qualifies", good, true, ReasonSibling},
		{"an absent sibling", absent, false, ReasonSiblingAbsent},
		{"a non-regular sibling", notReg, false, ReasonSiblingNotRegular},
		{"a sibling owned by another account", other, false, ReasonSiblingOtherOwner},
		{"a sibling of a different build", skewed, false, ReasonPairOutOfStep},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, r := SiblingQualifies(self, c.p)
			if ok != c.wantOK || r != c.wantR {
				t.Fatalf("SiblingQualifies(%s) = (%v, %q), want (%v, %q)", filepath.Base(c.p), ok, r, c.wantOK, c.wantR)
			}
		})
	}
}

// covers: MA-630, R18, S59
// ProgramNote renders a ProgramForSchedule result for every surface: empty when
// there is nothing to say (none, sibling, wrapped_console); otherwise the one
// note naming the program, why the sibling was not used, the console cost only
// when the console build mailarchive.exe is what runs, and the right "Do this"
// (the installer's Repair first when the sibling is missing).
func TestMA630_ProgramNote(t *testing.T) {
	for _, r := range []Reason{ReasonNone, ReasonSibling, ReasonWrappedConsole} {
		if got := ProgramNote("/opt/MailArchive/mailarchive.exe", r); got != "" {
			t.Errorf("ProgramNote(%q) = %q, want empty", r, got)
		}
	}

	con := "/opt/MailArchive/mailarchive.exe"  // the console build
	noc := "/opt/MailArchive/mailarchivew.exe" // some other recorded name (no console sentence)
	has := func(s, sub string) bool { return strings.Contains(s, sub) }

	cases := []struct {
		exe            string
		reason         Reason
		wantWhy        string
		wantConsole    bool
		installerFirst bool
	}{
		{con, ReasonSiblingAbsent, "is missing", true, true},
		{noc, ReasonSiblingAbsent, "is missing", false, true},
		{con, ReasonSiblingNotRegular, "is not a plain file", true, false},
		{con, ReasonSiblingOtherOwner, "belongs to another account", true, false},
		{con, ReasonPairOutOfStep, "is from a different release", true, false},
		{noc, ReasonPairOutOfStep, "is from a different release", false, false},
	}
	for _, c := range cases {
		got := ProgramNote(c.exe, c.reason)
		if !has(got, "This schedule runs "+filepath.Base(c.exe)+" because mailarchivew.exe beside it "+c.wantWhy+".") {
			t.Errorf("ProgramNote(%q,%q) = %q; missing the %q clause", c.exe, c.reason, got, c.wantWhy)
		}
		if gotConsole := has(got, "A console window will stay open while it runs."); gotConsole != c.wantConsole {
			t.Errorf("ProgramNote(%q,%q): console sentence = %v, want %v", c.exe, c.reason, gotConsole, c.wantConsole)
		}
		if gotInstaller := has(got, "with the installer's Repair"); gotInstaller != c.installerFirst {
			t.Errorf("ProgramNote(%q,%q): installer-first Do-this = %v, want %v", c.exe, c.reason, gotInstaller, c.installerFirst)
		}
		if !has(got, "choose Repair") {
			t.Errorf("ProgramNote(%q,%q) = %q; missing the Repair instruction", c.exe, c.reason, got)
		}
	}
}

// covers: MA-615, R33, R18, S59
// ProgramForSchedule resolves self through os.Executable()+EvalSymlinks+Clean
// and refuses a non-absolute result; the pure core then decides the rest.
func TestMA615_ProgramForScheduleResolvesSelf(t *testing.T) {
	orig := SelfResolver
	t.Cleanup(func() { SelfResolver = orig })

	// A non-absolute self is refused.
	SelfResolver = func() (string, error) { return "mailarchive", nil }
	if _, _, err := ProgramForSchedule(false); err == nil {
		t.Error("a non-absolute self must be an error")
	}

	// An absolute self on this host returns a usable program path.
	SelfResolver = func() (string, error) { return filepath.Join(t.TempDir(), "mailarchive"), nil }
	exe, reason, err := ProgramForSchedule(false)
	if err != nil || !filepath.IsAbs(exe) {
		t.Fatalf("got (%q, %q, %v), want an absolute path and no error", exe, reason, err)
	}
}
