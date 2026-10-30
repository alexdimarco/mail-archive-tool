//go:build darwin || windows

package wizard

import (
	"io"
	"log"
	"testing"

	"mail-archive-tool/internal/job"
)

// covers: MA-580
// On a platform with a pre-sync adapter, a job that carries ThunderbirdSync makes
// optsForJob install app.Run's BeforeRead hook, so the scheduled/Run-now local
// backup pre-syncs — the same seam the -thunderbird-sync CLI flag uses. (It only
// SETS the hook; no Thunderbird is launched here.)
func TestOptsForJobInstallsPresyncHookWhenOn(t *testing.T) {
	j := job.Job{Name: "x", Out: "/o", Mode: "incremental", ThunderbirdSync: &job.ThunderbirdSync{}}
	opts := optsForJob(j, []string{"/in/a"}, log.New(io.Discard, "", 0), runHooks{}, runScheduled)
	if opts.BeforeRead == nil {
		t.Error("a job carrying ThunderbirdSync must install the pre-sync BeforeRead hook")
	}
}
