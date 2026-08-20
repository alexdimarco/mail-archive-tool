package schedule

import (
	"strings"
	"testing"
)

func sampleSpec(iv Interval, at string) Spec {
	return Spec{
		Name:     DefaultName,
		Interval: iv,
		At:       at,
		Exe:      "/usr/local/bin/mailarchive",
		Args:     []string{"-out", "/data/backup", "-mode", "incremental", "-auto"},
		Log:      "/data/backup/mailarchive-backup.log",
	}
}

// covers: MA-45, R14, S14
// The cron line carries the interval's schedule fields, the executable + export
// flags, and the append-redirect to the logfile; the block prepends the marker.
func TestCronLineGeneration(t *testing.T) {
	daily, err := CronLine(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(daily, "0 2 * * * ") {
		t.Errorf("daily schedule fields wrong: %q", daily)
	}
	for _, want := range []string{"/usr/local/bin/mailarchive", "-out /data/backup", "-mode incremental", "-auto", ">> /data/backup/mailarchive-backup.log 2>&1"} {
		if !strings.Contains(daily, want) {
			t.Errorf("cron line missing %q: %q", want, daily)
		}
	}

	hourly, err := CronLine(sampleSpec(Hourly, "02:15"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hourly, "15 * * * * ") {
		t.Errorf("hourly schedule fields wrong: %q", hourly)
	}

	weekly, err := CronLine(sampleSpec(Weekly, "03:30"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(weekly, "30 3 * * 0 ") {
		t.Errorf("weekly schedule fields wrong: %q", weekly)
	}

	block, err := CronBlock(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(block, CronMarker(DefaultName)+"\n") {
		t.Errorf("block missing leading marker: %q", block)
	}

	if _, err := CronLine(sampleSpec(Daily, "25:00")); err == nil {
		t.Error("expected error for out-of-range hour")
	}
}

// covers: MA-46, R14, S14
// The launchd plist names the label, lists the executable + args as
// ProgramArguments, and encodes the cadence as StartCalendarInterval (Minute
// always; Hour for daily/weekly; Weekday only for weekly).
func TestLaunchdPlistGeneration(t *testing.T) {
	daily, err := LaunchdPlist(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>" + LaunchdLabel(DefaultName) + "</string>",
		"<key>ProgramArguments</key>",
		"<string>/usr/local/bin/mailarchive</string>",
		"<string>-out</string>",
		"<string>/data/backup</string>",
		"<key>StartCalendarInterval</key>",
		"<key>Minute</key>",
		"<key>Hour</key>",
	} {
		if !strings.Contains(daily, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	if strings.Contains(daily, "<key>Weekday</key>") {
		t.Error("daily plist must not set Weekday")
	}

	weekly, err := LaunchdPlist(sampleSpec(Weekly, "03:30"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(weekly, "<key>Weekday</key>") {
		t.Error("weekly plist must set Weekday")
	}

	hourly, err := LaunchdPlist(sampleSpec(Hourly, "00:15"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hourly, "<key>Hour</key>") {
		t.Error("hourly plist must not pin an Hour")
	}
	if !strings.Contains(hourly, "<key>Minute</key>") {
		t.Error("hourly plist must set Minute")
	}
}

// covers: MA-47, R14, S14
// The schtasks command names the task, the run command, the schedule class and
// start time; weekly adds /D SUN; the delete command reverses it by name.
func TestSchtasksCommandGeneration(t *testing.T) {
	daily, err := SchtasksCreateCmd(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`/Create`, `/TN "mailarchive-backup"`, `/TR "`, `/SC DAILY`, `/ST 02:00`, `/F`} {
		if !strings.Contains(daily, want) {
			t.Errorf("schtasks create missing %q: %q", want, daily)
		}
	}
	if strings.Contains(daily, "/D SUN") {
		t.Error("daily task must not carry /D SUN")
	}

	weekly, err := SchtasksCreateCmd(sampleSpec(Weekly, "03:30"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(weekly, "/SC WEEKLY") || !strings.Contains(weekly, "/D SUN") {
		t.Errorf("weekly task wrong: %q", weekly)
	}

	del := SchtasksDeleteCmd(DefaultName)
	if !strings.Contains(del, "/Delete") || !strings.Contains(del, `/TN "mailarchive-backup"`) {
		t.Errorf("delete command wrong: %q", del)
	}
}

// covers: MA-48, R14, S14
// UpsertCronBlock is idempotent: applying the same spec's block twice leaves
// exactly one marker and one command line, and preserves unrelated crontab lines.
func TestUpsertCronBlockIdempotent(t *testing.T) {
	existing := "0 5 * * * /usr/bin/other-job\n"
	block, err := CronBlock(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	marker := CronMarker(DefaultName)

	once := UpsertCronBlock(existing, marker, block)
	twice := UpsertCronBlock(once, marker, block)

	if once != twice {
		t.Errorf("upsert not idempotent:\n once=%q\n twice=%q", once, twice)
	}
	if n := strings.Count(twice, marker); n != 1 {
		t.Errorf("marker appears %d times, want 1:\n%s", n, twice)
	}
	if !strings.Contains(twice, "/usr/bin/other-job") {
		t.Error("upsert dropped an unrelated crontab line")
	}
	if !strings.Contains(twice, "0 2 * * * ") {
		t.Error("upsert did not contain our scheduled command")
	}
}

// covers: MA-49, R14, S14
// RemoveCronBlock cleanly reverses an install: after upsert-then-remove the
// crontab is byte-for-byte the original, and unrelated lines survive.
func TestRemoveCronBlockReverses(t *testing.T) {
	existing := "0 5 * * * /usr/bin/other-job\n"
	block, err := CronBlock(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	marker := CronMarker(DefaultName)

	installed := UpsertCronBlock(existing, marker, block)
	if !strings.Contains(installed, marker) {
		t.Fatal("precondition: block not installed")
	}
	removed := RemoveCronBlock(installed, marker)
	if removed != existing {
		t.Errorf("remove did not restore original:\n got=%q\n want=%q", removed, existing)
	}
	if strings.Contains(removed, marker) {
		t.Error("marker survived removal")
	}
	// Removing from an empty crontab is a clean no-op.
	if got := RemoveCronBlock("", marker); got != "" {
		t.Errorf("remove from empty crontab = %q, want empty", got)
	}
}
