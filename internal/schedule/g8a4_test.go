package schedule

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-455, MA-457, R14, S14
// OPEN RESIDUAL ENCODER (G8-AO1, the part the binding cannot close; R14, owner
// ruling UJ-6). On launchd a custom entry name another archive holds cannot be
// told from this archive's without reading the plist back, which UJ-6 rules out
// (and the descriptor binding in bound.go holds only what the host can say
// without it: the archive's own derived names, the job's -out, a cron block's
// -out, a Windows wrapper of ours). So a descriptor handed over with archive B's
// custom entry name and a job that backs up its own folder still binds on
// launchd, and `-out Y -remove` unloads B's plist. Closing it means reading the
// plist's -out (a UJ-6 amendment) or namespacing custom names by archive (a
// naming change): an owner decision. The encoder installs archive B's backup
// under a custom name on a sandboxed launchd host and wants the name planted in
// Y's descriptor refused, naming the name and B, with B's plist unchanged.
func TestPlantedCustomNameOfAnotherArchiveIsRefusedOnLaunchd(t *testing.T) {
	t.Skip("open residual (owner decision, UJ-6): launchd entries are never read back, so a custom -name another archive holds binds to a descriptor that claims it; closing it would reopen UJ-6 or change how custom names are namespaced. Escalated by the g8 adversarial pass (G8-AO1).")
	sandboxHostScheduler(t)
	b, y := t.TempDir(), t.TempDir()
	if err := Install(backupSpecFor(b, "nightlyB-g8a")); err != nil {
		t.Fatal(err)
	}
	was, err := os.ReadFile(launchdPlistPath("nightlyB-g8a"))
	if err != nil {
		t.Skip("shown on a launchd host")
	}
	if err := WriteDescriptor(y, DescribeSpec(backupSpecFor(y, "nightlyB-g8a"))); err != nil {
		t.Fatal(err)
	}
	_, rerr := recordedBackupNameOn(y, "darwin")
	code, msg := refusal(rerr)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("nightlyB-g8a", b),
		assure.NoSideEffect(func() bool {
			after, err := os.ReadFile(launchdPlistPath("nightlyB-g8a"))
			return err == nil && string(after) == string(was)
		}))
}

// covers: MA-457, R14, S14
// OPEN ENCODER (G8-AC4, R14; an owner decision on locking). Nothing serialises
// schedule mutations: refuseCollision reads the descriptors, the scheduler step
// runs, and the descriptor is written last, so two installs for one archive under
// different names that overlap both pass the one-schedule-per-archive guard,
// install both entries, and leave the descriptor recording only one — the other
// is named nowhere. A guard that holds under overlap needs a lock that spans the
// read, the scheduler edit and the descriptor write (a lock file beside the
// descriptor, which the export's archive lock cannot be, as an export holds it
// for hours) and a decision on what a second operator sees while it is held. The
// encoder runs the two guards as the overlap orders them — both before either
// install writes — and wants one of them refused.
func TestConcurrentInstallsForOneArchiveCannotBothPassTheGuard(t *testing.T) {
	t.Skip("open (owner decision, G8-AC4): schedule mutations are not serialised; closing it needs a lock design across the guard, the scheduler edit and the descriptor write, which the archive lock (held for an export's whole run) cannot provide.")
	emptyCrontab(t)
	out := t.TempDir()
	a, b := backupSpecFor(out, "nightly-a-ac4"), backupSpecFor(out, "nightly-b-ac4")
	errA, errB := refuseCollision(a, "linux"), refuseCollision(b, "linux") // as the overlap orders them
	if errA == nil && errB == nil {
		t.Errorf("both overlapping installs passed the one-schedule-per-archive guard")
	}
}

// covers: MA-457, R14, S14
// OPEN ENCODER (K4, R14; the g8-Q6 ruling bounds the name guard to its two
// cross-kind refusals). A verify descriptor that records the name of the
// archive's DEFAULT backup entry, planted beside a backup descriptor that has
// gone (`-out X -remove` keeps a verify descriptor it does not trust, and the
// backup's own is deleted with its entry), reads as the archive's verify once the
// backup's descriptor is no longer there to contradict it, and then the backup
// cannot be re-installed under its default name: the install refuses it as the
// verify's. Closing it rejects a verify descriptor under the backup's derived
// name at read time and refuses a verify install under it — a third refusal
// beside g8-Q6's two, which is the owner's to rule. The encoder wants the
// default-named backup to install over such a planted record.
func TestPlantedVerifyDescriptorDoesNotBlockTheDefaultNamedBackup(t *testing.T) {
	t.Skip("open (owner decision, K4, g8-Q6): a planted verify descriptor recording the backup's default name blocks the default-named backup install once the backup's descriptor is gone; closing it adds a third cross-kind refusal beyond g8-Q6's two.")
	emptyCrontab(t)
	out := t.TempDir()
	if err := WriteVerifyDescriptor(out, DescribeSpec(verifySpecFor(out, DefaultNameFor(out)))); err != nil {
		t.Fatal(err)
	}
	if err := installOn(backupSpecFor(out, DefaultNameFor(out)), "linux"); err != nil {
		t.Errorf("the default-named backup is refused over a planted verify record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, DescriptorName)); err != nil {
		t.Errorf("the backup recorded no descriptor: %v", err)
	}
}
