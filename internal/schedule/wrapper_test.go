package schedule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-73, R14, S28
// On Windows the task runs a batch wrapper: every token in it is quoted (a
// path with "&", "^", "(", "!" or "%" survives cmd.exe), "%" is doubled, stderr
// goes to the sibling .stderr.log. The task is registered from the XML
// definition (/Create /TN /XML /F) whose <Command> is the wrapper path; without
// the wrapper the program lands in <Command> directly.
func TestCmdWrapperQuotesEverything(t *testing.T) {
	s := Spec{
		Name: "mailarchive-1234abcd", Interval: Daily, At: "02:00",
		Exe:     `C:\Program Files\MailArchive\mailarchive.exe`,
		Args:    []string{"-out", `C:\Data\R&D (2026)\Mail!Archive`, "-mode", "incremental", "-since", "30d", "-log", `C:\Data\R&D (2026)\Mail!Archive\mailarchive-1234abcd.log`, "-input", `C:\Users\Alex\%weird%\x.pst`},
		Log:     `C:\Data\R&D (2026)\Mail!Archive\mailarchive-1234abcd.log`,
		Wrapper: true, WrapperPath: `C:\Users\Alex\AppData\Local\mailarchive\mailarchive-1234abcd.cmd`,
	}
	w := CmdWrapper(s)
	lines := strings.Split(strings.TrimSpace(w), "\r\n")
	run := lines[len(lines)-1]
	for _, want := range []string{
		`"C:\Program Files\MailArchive\mailarchive.exe"`,
		`"C:\Data\R&D (2026)\Mail!Archive"`,
		`"-mode" "incremental"`,
		`"C:\Users\Alex\%%weird%%\x.pst"`,
		`2>> "C:\Data\R&D (2026)\Mail!Archive\mailarchive-1234abcd.stderr.log"`,
	} {
		if !strings.Contains(run, want) {
			t.Errorf("wrapper run line lacks %s:\n%s", want, run)
		}
	}
	if !strings.HasPrefix(w, "@echo off\r\n") || strings.Contains(w, "enabledelayedexpansion") {
		t.Errorf("wrapper header wrong:\n%s", w)
	}
	// Every token on the run line is quoted: no bare token may remain.
	for _, tok := range strings.Fields(strings.SplitN(run, " 2>> ", 2)[0]) {
		if !strings.HasPrefix(tok, `"`) && !strings.HasSuffix(tok, `"`) {
			t.Errorf("unquoted token on the run line: %q", tok)
		}
	}

	// The task registers from the XML definition; the argv is the /Create-XML
	// pair, and the definition's <Command> is the wrapper path.
	argv, err := SchtasksCreateArgv(s)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"/Create", "/TN", s.Name, "/XML", SchtasksXMLPath(s.WrapperPath), "/F"}; !equalStrings(argv, want) {
		t.Errorf("create argv = %q, want %q", argv, want)
	}
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.Local)
	doc, err := SchtasksXML(s, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseTaskXML(t, doc).Actions.Exec.Command; got != s.WrapperPath {
		t.Errorf("XML <Command> should be the wrapper path, got %q", got)
	}
	// Without the wrapper the program lands in <Command> directly (MA-65).
	s.Wrapper = false
	doc, err = SchtasksXML(s, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseTaskXML(t, doc).Actions.Exec.Command; !strings.Contains(got, "mailarchive.exe") {
		t.Errorf("direct <Command> lost the program: %q", got)
	}
}

// covers: MA-97, R14, S28
// An installed schedule leaves a descriptor in the archive naming it (name,
// cadence, real executable and its identity, job, host); it round-trips, is
// removed on request, and a missing one is reported as os.ErrNotExist. The
// default schedule name derives from the archive path (one per archive, stable);
// -name is bounded and restricted to safe characters.
func TestDescriptorAndNames(t *testing.T) {
	out := t.TempDir()
	exe := filepath.Join(out, "mailarchive")
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Spec{Name: "n", Interval: Weekly, At: "03:30", Exe: exe, Args: []string{"-out", out, "-log", "x.log"}, Log: "x.log", Out: out}
	d := DescribeSpec(s)
	if d.Exe != exe || d.ExeSize != 3 || d.Host == "" || d.Interval != "weekly" || len(d.Job) != 4 {
		t.Errorf("descriptor incomplete: %+v", d)
	}
	if err := WriteDescriptor(out, d); err != nil {
		t.Fatal(err)
	}
	got, err := ReadDescriptor(out)
	if err != nil || got.Name != "n" || got.At != "03:30" || got.ExeSize != 3 || got.Job[1] != out {
		t.Errorf("descriptor did not round-trip: %+v (%v)", got, err)
	}
	if err := RemoveDescriptor(out); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDescriptor(out); !os.IsNotExist(err) {
		t.Errorf("after remove, want ErrNotExist, got %v", err)
	}
	if err := RemoveDescriptor(out); err != nil {
		t.Errorf("removing an absent descriptor must be a no-op: %v", err)
	}

	a, b := DefaultNameFor(out), DefaultNameFor(filepath.Join(out, "other"))
	if a == b || !strings.HasPrefix(a, "mailarchive-") || len(a) != len("mailarchive-")+8 || a != DefaultNameFor(out) {
		t.Errorf("DefaultNameFor: %q %q", a, b)
	}
	for _, bad := range []string{"", strings.Repeat("n", MaxNameLen+1), "has space", "../x", `a"b`} {
		if _, err := SanitizeName(bad); err == nil {
			t.Errorf("SanitizeName(%q) accepted", bad)
		}
	}
	if got, err := SanitizeName("nightly_v2.mail"); err != nil || got != "nightly_v2.mail" {
		t.Errorf("SanitizeName rejected a good name: %v", err)
	}
}
