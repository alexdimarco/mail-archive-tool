package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-979, R36
// A delete that begins while a Switch to its folder is being saved is refused, its plan kept,
// until the save ends (the delete residuals' security pass, CON-01): the Switch checked for a
// delete, asked the scheduler, then wrote the setting, with nothing holding the folder between, so
// a delete begun and finished in that window left a removed folder as the current archive, the
// switch rules that delete applies never run. The `BackupInstalled` seam (timing only) begins the
// deletes inside the save. Positive twins first: a delete of another archive begins and completes
// during the save; once the save has ended, a new plan's delete of the folder completes.
func TestADeleteCannotBeginWhileASwitchToItsFolderIsSaved(t *testing.T) {
	e := newDeleteEnv(t)
	from, dir, elsewhere := e.plainArchive("From"), e.plainArchive("Target"), e.plainArchive("Elsewhere")
	if _, m := e.post("/api/settings", map[string]any{"Out": from}); m["ok"] != true {
		t.Fatalf("the first switch: %v", m)
	}
	p, op := e.plan(dir), e.plan(elsewhere)
	if p["ok"] != true || op["ok"] != true {
		t.Fatalf("the plans: %v %v", p, op)
	}
	tok := p["token"].(string)
	var twin, during map[string]any
	e.d.cfg.BackupInstalled = func() bool {
		if during == nil {
			twin = e.deleteWith(op["token"].(string), op["name"].(string), false)
			during = e.deleteWith(tok, p["name"].(string), false)
		}
		return false
	}
	if _, m := e.post("/api/settings", map[string]any{"Out": dir}); m["ok"] != true || e.d.cfg.effectiveOut() != dir {
		t.Fatalf("the switch: %v (current %q)", m, e.d.cfg.effectiveOut())
	}
	if twin["status"] != "complete" {
		t.Fatalf("positive twin: a delete of another archive during the switch: %v", twin)
	}
	// (The switch itself then ends plans made of other archives than the new current one, KEY-05,
	// so the kept token is not checked here; nothing of the folder is logged.)
	assure.Refused(t, http.StatusOK, replyErr(during), assure.Code(http.StatusOK), assure.Names(switchingRefusal),
		assure.NoSideEffect(func() bool {
			return during["ok"] == false && archdelete.IsArchive(dir) && e.listed(dir) && !strings.Contains(deletionsLog(t, e.cfg), dir)
		}))
	e.d.cfg.BackupInstalled = func() bool { return false }
	if r := e.planAndDelete(dir); r["status"] != "complete" {
		t.Errorf("positive twin: once the switch ended, a delete of the folder: %v", r)
	}
}

// covers: MA-979, R36
// A delete of the current archive that begins while the Scheduled backup card's Install of it runs
// is refused until the Install ends (the delete residuals' security pass, CON-02): the Install
// checked for a delete, then installed later with nothing holding the folder, so a delete whose
// step 2 removed the schedules could be followed by a scheduled task (and its job file) for a
// folder being emptied. The `InstallBackup` seam (timing only) begins the deletes inside the
// Install. Positive twins first: a delete of another archive begins and completes during the
// Install; once it has ended, a new plan's delete of the current archive completes.
func TestADeleteCannotBeginWhileAScheduledBackupOfItsFolderIsInstalled(t *testing.T) {
	e := newDeleteEnv(t)
	cur, elsewhere := e.plainArchive("Current"), e.plainArchive("Elsewhere")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	p, op := e.plan(cur), e.plan(elsewhere)
	if p["ok"] != true || op["ok"] != true {
		t.Fatalf("the plans: %v %v", p, op)
	}
	tok := p["token"].(string)
	var mu sync.Mutex
	installs := 0
	var twin, during map[string]any
	e.cfg.InstallBackup = func(schedule.Spec) error {
		mu.Lock()
		installs++
		first := installs == 1
		mu.Unlock()
		if first {
			twin = e.deleteWith(op["token"].(string), op["name"].(string), false)
			during = e.deleteWith(tok, p["name"].(string), false)
		}
		return nil
	}
	e.d.cfg = e.cfg
	if _, m := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "03:00", "drawnFor": cur}); m["ok"] != true {
		t.Fatalf("the Install: %v", m)
	}
	if twin["status"] != "complete" {
		t.Fatalf("positive twin: a delete of another archive during the Install: %v", twin)
	}
	assure.Refused(t, http.StatusOK, replyErr(during), assure.Code(http.StatusOK), assure.Names(installingRefusal),
		assure.NoSideEffect(func() bool {
			return during["ok"] == false && archdelete.IsArchive(cur) && e.listed(cur) && e.d.pending.get(tok, isDeletePlan) != nil
		}))
	// The install's descriptor is the archive's own file: a new plan lists it.
	_ = os.Remove(filepath.Join(cur, schedule.DescriptorName))
	if r := e.planAndDelete(cur); r["status"] != "complete" {
		t.Errorf("positive twin: once the Install ended, a delete of the current archive: %v", r)
	}
}
