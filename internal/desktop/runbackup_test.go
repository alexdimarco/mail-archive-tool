package desktop

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/wizard"
)

// covers: MA-694
// "Run now" runs the recorded run-job backup in-process (archive-manager, tester request):
// wizard.RunJobInProcess refuses an absent -out (PM4: never a decoy) and an Outlook backup
// (which runs on its schedule, not here); the Desktop's /api/run-backup refuses an archive with
// no run-job backup. The full local run shares app.Run with the scheduled path (RunHeadless).
func TestMA694_RunBackupNow(t *testing.T) {
	// RunJobInProcess refuses an Outlook backup (its -out exists, so it reaches the Outlook check).
	outlookJob := filepath.Join(t.TempDir(), "o.json")
	if err := job.Write(outlookJob, job.Job{Name: "b", Out: t.TempDir(), Mode: "incremental", Outlook: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := wizard.RunJobInProcess(context.Background(), nil, outlookJob, log.New(io.Discard, "", 0), wizard.RunNow{}); err == nil || !strings.Contains(err.Error(), "Outlook") {
		t.Fatalf("an Outlook backup must be refused by Run now, got %v", err)
	}

	// RunJobInProcess refuses an absent -out (never recreate a decoy).
	goneJob := filepath.Join(t.TempDir(), "g.json")
	if err := job.Write(goneJob, job.Job{Name: "b", Out: filepath.Join(t.TempDir(), "gone"), Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	if _, err := wizard.RunJobInProcess(context.Background(), nil, goneJob, log.New(io.Discard, "", 0), wizard.RunNow{}); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("an absent -out must be refused, got %v", err)
	}

	// /api/run-backup refuses an archive that has no run-job backup (a Microsoft 365 archive
	// uses "Archive now").
	arch := t.TempDir()
	writeManifest(t, arch)
	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: arch})
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/run-backup", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", d.csrf)
	rec := httptest.NewRecorder()
	d.runBackup(rec, req)
	// In plain words, naming what to use instead (P3-8: no "run-job" jargon).
	if !strings.Contains(rec.Body.String(), `"ok":false`) || !strings.Contains(rec.Body.String(), "Run now has nothing to run") ||
		!strings.Contains(rec.Body.String(), "Archive now") || strings.Contains(rec.Body.String(), "run-job") {
		t.Fatalf("run-backup on an archive with no run-job must refuse, got %s", rec.Body.String())
	}
}
