package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// writeBackupDescriptor plants a recorded BACKUP schedule descriptor in out (a
// daily 02:00 export), the way an installed backup would, so a following verify
// schedule can be judged against it without touching the host scheduler.
func writeBackupDescriptor(t *testing.T, out, at string) {
	t.Helper()
	doc := `{"version":1,"name":"mailarchive-backup","interval":"daily","at":"` + at +
		`","exe":"/usr/local/bin/mailarchive","job":["-out","/x","-mode","incremental","-unattended"],"host":"h"}`
	if err := os.WriteFile(filepath.Join(out, schedule.DescriptorName), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-163, R14, R12, S28
// A scheduled verify job coexists with the archive's backup: it derives a
// distinct "-verify" entry name (never overwriting the backup's), and its preview
// prints the whole-run-lock / backup-window warning. But a verify pointed at the
// SAME cadence and time the archive's recorded backup uses is refused with a
// typed non-zero naming the backup and the remedy — the two would hold the lock
// against each other on every overlap.
func TestScheduleVerifyNameCollisionAndWarning(t *testing.T) {
	out := t.TempDir()
	writeBackupDescriptor(t, out, "02:00")

	// Same cadence + time as the recorded backup → refused, exit 1, named.
	code, stderr := runCLI("schedule", "-interval", "daily", "-at", "02:00", "--", "verify", "-out", out)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("backup schedule", "-at", "lock"))

	// A different slot → previewed (not applied), with the lock warning, the
	// verify-verdict-in-status note, and a name distinct from the backup's.
	code, stdout, stderr := runCLIOut(t, "schedule", "-interval", "weekly", "-at", "05:00", "--", "verify", "-out", out)
	if code != 0 {
		t.Fatalf("a verify at a distinct time was refused (%d): %s", code, stderr)
	}
	verifyName := schedule.DefaultNameFor(out) + "-verify"
	for _, want := range []string{
		verifyName,                             // distinct entry name, not the backup's
		"holds the archive lock for its whole", // the lock/backup-window warning
		"a verify's verdict is shown as Last verify",
		"NOT applied",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("verify preview lacks %q:\n%s", want, stdout)
		}
	}
	// The distinct name must not be the plain backup name.
	if strings.Contains(stdout, schedule.DefaultNameFor(out)+" ") {
		t.Errorf("verify preview used the plain backup name:\n%s", stdout)
	}
	if _, err := schedule.SanitizeName(verifyName); err != nil {
		t.Errorf("derived verify name %q is not SanitizeName-safe: %v", verifyName, err)
	}
}
