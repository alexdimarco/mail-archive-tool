package schedule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-97, R14, R12, S28
// The descriptor is untrusted input: control characters in its strings are
// stripped before they can reach a terminal or dialog, a name outside the
// schedule-name grammar is refused, and an unparseable cadence degrades to a
// visible placeholder rather than a crash. Install refuses to replace another
// archive's schedule: an archive that already records a different name, or a
// cron block under this name that backs up a different -out.
func TestDescriptorIsUntrusted(t *testing.T) {
	out := t.TempDir()
	bad := `{"version":1,"name":"nightly","interval":"daily","at":"02:00","exe":"/bin/x\u001b[2Jevil","host":"h\u0007ost","job":["-out","/a\r\n"]}`
	if err := os.WriteFile(filepath.Join(out, DescriptorName), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := ReadDescriptor(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range append([]string{d.Exe, d.Host}, d.Job...) {
		if strings.ContainsAny(v, "\x1b\x07\r\n") {
			t.Errorf("control characters survived in %q", v)
		}
	}
	os.WriteFile(filepath.Join(out, DescriptorName), []byte(`{"version":1,"name":"../etc","interval":"x","at":"y","exe":"/bin/x"}`), 0o644)
	if _, err := ReadDescriptor(out); err == nil || !strings.Contains(err.Error(), "invalid schedule") {
		t.Errorf("invalid name accepted: %v", err)
	}
	os.WriteFile(filepath.Join(out, DescriptorName), []byte(`{"version":1,"name":"ok","interval":"fortnightly","at":"25:99","exe":"/bin/x"}`), 0o644)
	d, err = ReadDescriptor(out)
	if err != nil || d.Interval != "daily" || d.At != "??:??" {
		t.Errorf("unparseable cadence not degraded legibly: %+v %v", d, err)
	}

	// Collision guards.
	os.WriteFile(filepath.Join(out, DescriptorName), []byte(`{"version":1,"name":"first","interval":"daily","at":"02:00","exe":"/bin/x"}`), 0o644)
	spec := Spec{Name: "second", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: out}
	if err := refuseCollision(spec); err == nil || !strings.Contains(err.Error(), "first") {
		t.Errorf("a second schedule for the same archive was not refused: %v", err)
	}
	crontab := CronMarker("shared") + "\n0 2 * * * /bin/x -out /other/archive -mode incremental\n"
	if got := cronBlockOut(crontab, CronMarker("shared")); got != "/other/archive" {
		t.Errorf("cronBlockOut = %q", got)
	}
	if got := cronBlockOut(crontab, CronMarker("absent")); got != "" {
		t.Errorf("cronBlockOut for an absent marker = %q", got)
	}

	// Validate refuses control characters and a relative wrapper path.
	bad2 := Spec{Name: "n", Interval: Daily, At: "02:00", Exe: "/bin/x", Args: []string{"-out", "/a\n* * * * * evil"}}
	if err := bad2.Validate(); err == nil || !strings.Contains(err.Error(), "control character") {
		t.Errorf("newline in an argument accepted: %v", err)
	}
	rel := Spec{Name: "n", Interval: Daily, At: "02:00", Exe: "/bin/x", Wrapper: true, WrapperPath: "relative.cmd"}
	if err := rel.Validate(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("relative wrapper path accepted: %v", err)
	}
	if !filepath.IsAbs(DefaultWrapperPath("n")) {
		t.Errorf("DefaultWrapperPath not absolute: %q", DefaultWrapperPath("n"))
	}
}
