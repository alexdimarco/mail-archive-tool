package schedule

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-164, R14, S14
// The Windows schtasks install preview now carries the same legibility the cron
// preview has: the plain-language cadence gloss, a "StartBoundary is local time"
// label, and a resilience gloss for the sleeps-the-laptop persona (a run missed
// while the PC slept is caught up on wake; runs on battery; a night the PC is off
// is skipped) plus the failure-visibility note. A verify schedule additionally
// carries the whole-run-lock warning and the note that a verify's verdict shows
// on status as Last verify.
func TestSchtasksPreviewGloss(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 0, 0, 0, time.Local)
	backup := Spec{
		Name:        "mailarchive-1234abcd",
		Interval:    Weekly,
		At:          "03:30",
		Exe:         `C:\Program Files\MailArchive\mailarchive.exe`,
		Args:        []string{"-out", `C:\Data\Mail Archive`, "-auto"},
		Wrapper:     true,
		WrapperPath: filepath.Join(t.TempDir(), "mailarchive-1234abcd.cmd"),
	}
	text, err := schtasksPreview(backup, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"weekly on Sunday at 03:30",   // the cadence gloss the cron preview also has
		"StartBoundary is local time", // the timezone label
		"caught up on wake",           // resilience gloss
		"runs on battery",
		"a night the PC is off is skipped",
		"A failed run surfaces only via `mailarchive status`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("schtasks preview lacks %q:\n%s", want, text)
		}
	}

	// A daily backup glosses its own cadence.
	daily := backup
	daily.Interval = Daily
	daily.At = "02:00"
	dtext, err := schtasksPreview(daily, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dtext, "daily at 02:00") {
		t.Errorf("daily schtasks preview lacks its cadence gloss:\n%s", dtext)
	}

	// A verify schedule carries the lock warning and the Last-verify note.
	verify := backup
	verify.Args = []string{"verify", "-out", `C:\Data\Mail Archive`, "-unattended"}
	vtext, err := schtasksPreview(verify, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"holds the archive lock for its whole",
		"a verify's verdict is shown as Last verify",
	} {
		if !strings.Contains(vtext, want) {
			t.Errorf("verify schtasks preview lacks %q:\n%s", want, vtext)
		}
	}
}
