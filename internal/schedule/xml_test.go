package schedule

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// parseTaskXML asserts the document is UTF-16LE with a BOM and an
// encoding="UTF-16" declaration, decodes it to UTF-8, and unmarshals it back
// into the task struct — the round-trip the design promises. It fails the test
// on any decode/parse error.
func parseTaskXML(t *testing.T, doc []byte) taskXML {
	t.Helper()
	if len(doc) < 2 || doc[0] != 0xFF || doc[1] != 0xFE {
		t.Fatalf("XML is not UTF-16LE with a BOM (first bytes %v)", doc[:minInt(2, len(doc))])
	}
	utf8, err := decodeUTF16LE(doc)
	if err != nil {
		t.Fatalf("decode UTF-16: %v", err)
	}
	if !strings.Contains(utf8, `encoding="UTF-16"`) {
		t.Errorf("declaration does not say encoding=\"UTF-16\":\n%s", utf8)
	}
	var got taskXML
	dec := xml.NewDecoder(strings.NewReader(utf8))
	// The bytes are already decoded to UTF-8; accept the UTF-16 declaration.
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	return got
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// covers: MA-146, R14, S28
// The Task Scheduler XML for daily and weekly specs — with a wrapper path
// holding a space, "&" and a non-ASCII rune — is UTF-16LE with a BOM, carries
// the 1.2 version and the schema namespace, and round-trips through
// xml.Unmarshal to the identical <Command> path, the right calendar schedule
// (ScheduleByDay/DaysInterval=1 for daily, ScheduleByWeek/WeeksInterval=1 +
// Sunday for weekly, Enabled), and the resilience settings (StartWhenAvailable,
// batteries allowed, IgnoreNew, WakeToRun false).
func TestSchtasksXMLRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.Local) // a Thursday, before 03:30
	base := Spec{
		Name:        "mailarchive-1234abcd",
		At:          "03:30",
		Exe:         `C:\Program Files\MailArchive\mailarchive.exe`,
		Wrapper:     true,
		WrapperPath: `C:\Users\Alex\R&D (2026)\föö\mailarchive-1234abcd.cmd`,
	}
	daily := base
	daily.Interval = Daily
	weekly := base
	weekly.Interval = Weekly

	for _, tc := range []struct {
		name            string
		s               Spec
		wantDay, wantWk bool
	}{
		{"daily", daily, true, false},
		{"weekly", weekly, false, true},
	} {
		doc, err := SchtasksXML(tc.s, now)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := parseTaskXML(t, doc)
		if got.Version != "1.2" || got.Xmlns != taskNamespace {
			t.Errorf("%s: version/namespace wrong: %q %q", tc.name, got.Version, got.Xmlns)
		}
		if got.Actions.Exec.Command != tc.s.WrapperPath {
			t.Errorf("%s: <Command> did not round-trip:\n got=%q\n want=%q", tc.name, got.Actions.Exec.Command, tc.s.WrapperPath)
		}
		st := got.Settings
		if !st.StartWhenAvailable || st.DisallowStartIfOnBatteries || st.StopIfGoingOnBatteries || st.MultipleInstancesPolicy != "IgnoreNew" || st.WakeToRun {
			t.Errorf("%s: settings are not the resilient set: %+v", tc.name, st)
		}
		cal := got.Triggers.Calendar
		if !cal.Enabled {
			t.Errorf("%s: trigger is not Enabled", tc.name)
		}
		if (cal.ByDay != nil) != tc.wantDay {
			t.Errorf("%s: ScheduleByDay present = %v, want %v", tc.name, cal.ByDay != nil, tc.wantDay)
		}
		if (cal.ByWeek != nil) != tc.wantWk {
			t.Errorf("%s: ScheduleByWeek present = %v, want %v", tc.name, cal.ByWeek != nil, tc.wantWk)
		}
		if tc.wantDay && cal.ByDay.DaysInterval != 1 {
			t.Errorf("%s: DaysInterval = %d, want 1", tc.name, cal.ByDay.DaysInterval)
		}
		if tc.wantWk && (cal.ByWeek.WeeksInterval != 1 || cal.ByWeek.DaysOfWeek.Sunday == nil) {
			t.Errorf("%s: weekly schedule wrong: %+v", tc.name, cal.ByWeek)
		}
		sb, err := time.ParseInLocation("2006-01-02T15:04:05", cal.StartBoundary, time.Local)
		if err != nil {
			t.Fatalf("%s: StartBoundary %q: %v", tc.name, cal.StartBoundary, err)
		}
		if !sb.After(now) {
			t.Errorf("%s: StartBoundary %v is not after now %v (a fresh task must never be a missed start)", tc.name, sb, now)
		}
	}
}

// covers: MA-147, R14, S14
// nextOccurrence returns the first fire at or after now: today when the time is
// still ahead, tomorrow (daily) or the next Sunday (weekly) when it has passed
// (or is exactly now), and it always lands on Sunday for weekly. A fresh task's
// StartBoundary is therefore never in the past (FC13).
func TestNextOccurrence(t *testing.T) {
	loc := time.UTC
	thu := time.Date(2026, 9, 3, 12, 0, 0, 0, loc) // 2026-09-03 is a Thursday
	if thu.Weekday() != time.Thursday {
		t.Fatalf("fixture is not a Thursday: %v", thu.Weekday())
	}

	// Daily, time still ahead today -> today.
	if got, want := nextOccurrence(thu, Daily, 14, 0), time.Date(2026, 9, 3, 14, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("daily ahead: got %v, want %v", got, want)
	}
	// Daily, time already passed today -> tomorrow.
	if got, want := nextOccurrence(thu, Daily, 2, 0), time.Date(2026, 9, 4, 2, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("daily passed: got %v, want %v", got, want)
	}
	// Daily, exactly now -> tomorrow (now itself would be a missed start).
	if got, want := nextOccurrence(thu, Daily, 12, 0), time.Date(2026, 9, 4, 12, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("daily equal-now: got %v, want %v", got, want)
	}

	// Weekly from a Thursday -> the coming Sunday (2026-09-06).
	if got, want := nextOccurrence(thu, Weekly, 3, 30), time.Date(2026, 9, 6, 3, 30, 0, 0, loc); got.Weekday() != time.Sunday || !got.Equal(want) {
		t.Errorf("weekly from Thursday: got %v (%v), want %v", got, got.Weekday(), want)
	}
	// Weekly on a Sunday, time still ahead -> today.
	sun := time.Date(2026, 9, 6, 1, 0, 0, 0, loc)
	if got, want := nextOccurrence(sun, Weekly, 3, 30), time.Date(2026, 9, 6, 3, 30, 0, 0, loc); !got.Equal(want) {
		t.Errorf("weekly same Sunday ahead: got %v, want %v", got, want)
	}
	// Weekly on a Sunday, time passed -> next Sunday.
	if got, want := nextOccurrence(time.Date(2026, 9, 6, 12, 0, 0, 0, loc), Weekly, 3, 30), time.Date(2026, 9, 13, 3, 30, 0, 0, loc); got.Weekday() != time.Sunday || !got.Equal(want) {
		t.Errorf("weekly next Sunday: got %v, want %v", got, want)
	}

	// Every result is strictly after now, for every interval.
	for _, now := range []time.Time{thu, sun} {
		for _, iv := range []Interval{Hourly, Daily, Weekly} {
			if o := nextOccurrence(now, iv, 3, 30); !o.After(now) {
				t.Errorf("nextOccurrence(%v, %s) = %v is not after now", now, iv, o)
			}
		}
	}
}

// covers: MA-148, R14, S14
// The Windows preview prints the schtasks command and the XML definition body
// decoded to readable UTF-8 (never the raw UTF-16 bytes), naming the definition
// path; and the Windows side-file removal deletes both the wrapper and the XML,
// tolerating either being already gone (and an empty wrapper path).
func TestSchtasksPreviewAndRemove(t *testing.T) {
	dir := t.TempDir()
	wrapperPath := filepath.Join(dir, "mailarchive-1234abcd.cmd")
	xmlPath := SchtasksXMLPath(wrapperPath)
	s := Spec{
		Name:        "mailarchive-1234abcd",
		Interval:    Weekly,
		At:          "03:30",
		Exe:         `C:\Program Files\MailArchive\mailarchive.exe`,
		Args:        []string{"-out", `C:\Data\Mail Archive`, "-auto"},
		Wrapper:     true,
		WrapperPath: wrapperPath,
	}

	text, err := schtasksPreview(s, time.Date(2026, 9, 3, 1, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"schtasks /Create", "/XML", xmlPath,
		"<Task", "<CalendarTrigger>", "<ScheduleByWeek>",
		"<Command>" + wrapperPath + "</Command>",
		"<StartWhenAvailable>true</StartWhenAvailable>",
		"@echo off", // the wrapper body is shown too
	} {
		if !strings.Contains(text, want) {
			t.Errorf("preview lacks %q:\n%s", want, text)
		}
	}
	if strings.ContainsRune(text, '\x00') {
		t.Error("preview leaked UTF-16 NUL bytes instead of decoded UTF-8")
	}

	// Remove deletes both files.
	if err := os.WriteFile(wrapperPath, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(xmlPath, []byte("<Task/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeSchtasksSideFiles(s); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{wrapperPath, xmlPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived removal (stat err = %v)", p, err)
		}
	}
	// Both already gone -> a clean no-op.
	if err := removeSchtasksSideFiles(s); err != nil {
		t.Errorf("removing absent side files must be a no-op: %v", err)
	}
	// An empty wrapper path removes nothing and errors nothing.
	if err := removeSchtasksSideFiles(Spec{}); err != nil {
		t.Errorf("empty wrapper path must be a no-op: %v", err)
	}
}
