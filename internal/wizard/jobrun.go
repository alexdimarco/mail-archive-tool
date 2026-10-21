package wizard

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/util"
)

// RunJobInProcess runs the recorded backup job at jobPath IN-PROCESS and returns a one-line
// result — the manual twin of RunHeadless (the scheduled entry point), for the Desktop's "Run
// now". RunHeadless is left untouched (it carries the scheduler's own log file, exit code and
// Outlook PST recreate); this shares the same app.Run core for a local backup, logging to the
// caller's logger. It refuses an absent -out (PM4) and a sealed archive, and marks the run
// Unattended so the out-guard and the source-loss streak apply exactly as the scheduled run's.
// An Outlook backup runs on its schedule, not here.
func RunJobInProcess(jobPath string, logger *log.Logger) (string, error) {
	j, err := job.Read(jobPath)
	if err != nil {
		return "", err
	}
	if err := app.RequireExistingOut(j.Out); err != nil {
		return "", err
	}
	if err := seal.RefuseWrite(j.Out); err != nil {
		return "", err
	}
	if j.Outlook {
		return "", fmt.Errorf("this is an Outlook backup; it runs on its schedule — Run now is not available for it here yet")
	}
	mode := export.Incremental
	if strings.EqualFold(j.Mode, "full") {
		mode = export.Full
	}
	var since time.Time
	if j.Since != "" {
		if t, perr := util.ParseSince(j.Since, time.Now()); perr == nil {
			since = t
		}
	}
	opts := app.Options{
		Inputs: j.Inputs, Auto: j.Auto, Out: j.Out, Mode: mode, Since: since,
		CopyFirst: j.CopyFirst, Index: true, Pages: true, KeepRaw: j.KeepRaw,
		Unattended: true, FollowLinks: j.FollowLinks,
	}
	logger.Printf("Running the scheduled backup now for %s", j.Out)
	result, runErr := app.Run(context.Background(), opts, logger, nil)
	if runErr != nil {
		return "", runErr
	}
	return exportSummary(j.Out, j.KeepRaw, result), nil
}
