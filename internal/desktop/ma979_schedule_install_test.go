package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-979, R36
// The Scheduled backup card's Install or Repair of the current archive, posted while that
// archive's delete is under way (before step 4 clears it), is refused in MA-979's words, never
// the running check's "A backup is running now" (the delete holds the archive lock), and nothing
// is installed (the residual fixes' adversarial pass, finding 3). Positive twin first: while a
// delete of another archive runs, Install of the current archive is not refused as being deleted.
func TestScheduleInstallOfTheCurrentArchiveBeingDeletedSaysSo(t *testing.T) {
	e := newDeleteEnv(t)
	var mu sync.Mutex
	installs := 0
	e.cfg.InstallBackup = func(schedule.Spec) error { mu.Lock(); installs++; mu.Unlock(); return nil }
	e.d.cfg = e.cfg
	count := func() int { mu.Lock(); defer mu.Unlock(); return installs }
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"interval": "weekly", "time": "03:00", "drawnFor": cur}

	release, done := blockedDelete(t, e, e.plainArchive("Elsewhere"))
	_, twin := e.post("/api/schedule-install", body)
	close(release)
	if r := <-done; r["status"] != "complete" {
		t.Fatalf("the delete of another archive: %v", r)
	}
	if twin["ok"] != true || count() != 1 {
		t.Fatalf("positive twin: Install of the current archive while another archive's delete runs: %v (installs %d)", twin, count())
	}
	descriptor := func() bool { _, err := os.Stat(filepath.Join(cur, schedule.DescriptorName)); return err == nil }
	hadDescriptor := descriptor()

	release, done = blockedDelete(t, e, cur)
	_, m := e.post("/api/schedule-install", body)
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.NoSideEffect(func() bool { return m["ok"] == false && count() == 1 && descriptor() == hadDescriptor }))
	close(release)
	if r := <-done; r["status"] != "complete" {
		t.Fatalf("the delete of the current archive: %v", r)
	}
}
