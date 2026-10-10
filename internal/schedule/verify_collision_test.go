package schedule

import (
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-456, R14, S28
// A scheduled verify is its own scheduler entry, so the one-archive-per-name
// guard applies to it exactly as to a backup. On a cron host, a verify for
// archive B under a name whose managed block already verifies archive A (the
// old README example's fixed `-name mailarchive-verify`, run for a second
// archive) is refused naming the name, A, and the -name remedy, and the
// crontab is not rewritten, so A's block survives. The positive twins:
// re-installing A's own verify under that name, also when A's descriptor
// records its BACKUP under another name (that guard is backup-only; a verify's
// distinct name must not trip it).
func TestVerifyScheduleRefusesAnotherArchivesEntry(t *testing.T) {
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })

	a, b := t.TempDir(), t.TempDir()
	verifyFor := func(out string) Spec {
		return Spec{
			Name: "mailarchive-verify", Interval: Weekly, At: "05:00", Exe: "/usr/local/bin/mailarchive",
			Args: []string{"verify", "-out", out, "-unattended", "-log", out + "/mailarchive-verify.log"},
			Log:  out + "/mailarchive-verify.log", Out: out,
		}
	}
	blockA, err := CronBlock(verifyFor(a))
	if err != nil {
		t.Fatal(err)
	}
	crontab, wrote := "MAILTO=me\n"+blockA+"\n", false
	before := crontab
	crontabList = func() (string, error) { return crontab, nil }
	crontabInstall = func(c string) error { crontab, wrote = c, true; return nil }

	// Positive twins: A's own verify re-installs, with and without A's backup
	// descriptor recorded under the backup's name.
	if err := refuseCollision(verifyFor(a), "linux"); err != nil {
		t.Errorf("re-installing archive A's own verify was refused: %v", err)
	}
	backupA := Spec{Name: DefaultNameFor(a), Interval: Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive", Args: []string{"-out", a}, Out: a}
	if err := WriteDescriptor(a, DescribeSpec(backupA)); err != nil {
		t.Fatal(err)
	}
	if err := refuseCollision(verifyFor(a), "linux"); err != nil {
		t.Errorf("A's verify was refused because A's descriptor records its backup: %v", err)
	}

	// B's verify under A's verify name: refused, nothing written.
	err = refuseCollision(verifyFor(b), "linux")
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("mailarchive-verify", a, "-name"),
		assure.NoSideEffect(func() bool { return !wrote && crontab == before }))

	// Through Install itself on a cron host (the crontab seams sandbox it).
	t.Run("Install", func(t *testing.T) {
		switch runtime.GOOS {
		case "linux", "freebsd", "openbsd", "netbsd":
		default:
			t.Skip("Install takes the cron path only on a cron host")
		}
		err := Install(verifyFor(b))
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("mailarchive-verify", a),
			assure.NoSideEffect(func() bool { return !wrote && strings.Contains(crontab, blockA) }))
	})
}
