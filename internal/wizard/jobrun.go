package wizard

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presyncjob"
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
	if err := app.RefuseKeyless("archive into it", j.Out); err != nil {
		return "", err
	}
	if j.Outlook {
		return "", fmt.Errorf("this is an Outlook backup; it runs on its schedule — Run now is not available for it here yet")
	}
	opts := optsForJob(j, j.Inputs, logger.Writer())
	logger.Printf("Running the scheduled backup now for %s", j.Out)
	result, runErr := app.Run(context.Background(), opts, logger, nil)
	if runErr != nil {
		return "", runErr
	}
	return exportSummary(j.Out, j.KeepRaw, result), nil
}

// optsForJob builds the app.Options a scheduled (RunHeadless) or Run-now
// (RunJobInProcess) local backup uses from the job's recorded answers, so the two
// run the SAME way. inputs is the set to archive (the job's inputs, or the
// Outlook-recreated PSTs). When the job carries the Thunderbird pre-sync
// (j.ThunderbirdSync present, §4.2.5), it is installed as app.Run's BeforeRead
// hook through the shared seam (a no-op off macOS and Windows). It NEVER gates
// the archive (R32): a hook-setup failure is logged and the run proceeds without
// the sync. logw receives the pre-sync's owner-facing narration and any
// invalid-"since" warning.
func optsForJob(j job.Job, inputs []string, logw io.Writer) app.Options {
	mode := export.Incremental
	if strings.EqualFold(j.Mode, "full") {
		mode = export.Full
	}
	var since time.Time
	if j.Since != "" {
		if t, perr := util.ParseSince(j.Since, time.Now()); perr == nil {
			since = t
		} else {
			fmt.Fprintf(logw, "warning: ignoring invalid since %q: %v\n", j.Since, perr)
		}
	}
	opts := app.Options{
		Inputs: inputs, Auto: j.Auto, Out: j.Out, Mode: mode, Since: since,
		CopyFirst: j.CopyFirst, Index: true, Pages: true, KeepRaw: j.KeepRaw,
		Unattended: true, FollowLinks: j.FollowLinks,
	}
	if j.ThunderbirdSync != nil {
		if err := presyncjob.SetHook(&opts, j.ThunderbirdSync.Profile, logw); err != nil {
			fmt.Fprintf(logw, "warning: Thunderbird pre-sync not set up (%v); archiving without it\n", err)
		}
	}
	return opts
}
