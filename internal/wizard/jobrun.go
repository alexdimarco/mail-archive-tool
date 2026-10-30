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
	"mail-archive-tool/internal/presync"
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
	opts := optsForJob(j, j.Inputs, logger, runScheduled)
	logger.Printf("Running the scheduled backup now for %s", j.Out)
	result, runErr := app.Run(context.Background(), opts, logger, nil)
	if runErr != nil {
		return "", runErr
	}
	return exportSummary(j.Out, j.KeepRaw, result), nil
}

// RunJobInProcessKeyed is RunJobInProcess for an encrypted archive: the Desktop's
// attended Run now (design-encrypted-archives-phase3 §6; the P3-3 plan, 3d). The key
// arrives only through apply, which the Desktop builds from a write lease
// (keyring.Apply) — the wizard never imports internal/keyring (§3.1). It runs
// attended, not as the schedule would (Unattended off: a scheduled keyed run stays
// refused, A14), stops when ctx is cancelled, refuses an absent out, a paused
// archive and an Outlook job before apply is called, and says what it did in words
// that name no file of the archive (U16).
func RunJobInProcessKeyed(ctx context.Context, jobPath string, logger *log.Logger, apply func(*app.Options) (done func(), err error)) (string, error) {
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
		return "", fmt.Errorf("this is an Outlook backup; Run now is not available for it here yet")
	}
	opts := optsForJob(j, j.Inputs, logger, runScheduled)
	opts.Unattended = false
	switch j.ProgramCopies { // the choice the archive was made with, until a run seals it
	case "on":
		opts.ProgramCopiesIfUnchosen = app.ProgramCopiesOn
	case "off":
		opts.ProgramCopiesIfUnchosen = app.ProgramCopiesOff
	}
	done, err := apply(&opts)
	if err != nil {
		return "", err
	}
	defer done()
	logger.Printf("Running the backup now for the encrypted archive %s", j.Out)
	result, runErr := app.Run(ctx, opts, logger, nil)
	if runErr != nil {
		return "", runErr
	}
	return encryptedSummary(result), nil
}

// encryptedSummary is what a keyed run says: no counts kept anywhere, no file of the
// archive named — its pages, report and index are sealed (U16).
func encryptedSummary(result app.Result) string {
	s := fmt.Sprintf("Backup complete: %d message(s) added or updated. They are sealed in the encrypted archive; open it in MailArchive Desktop to read them.", result.Stats.Exported)
	if result.IndexBehind {
		s += " The search index is missing some messages; the next backup, or Rebuild search index, brings it up to date."
	}
	return s
}

// runMode says who started a run (design-presync-attended §2.2, R38): the
// scheduler, with no one there, or the person at this computer. It is the only
// way a caller of optsForJob sets the attended mode; it lives in memory and no
// file carries it (C-36).
type runMode int

const (
	// runScheduled: no one is watching — the scheduled run-job, and (until slices
	// AS4 and AS8, C-44 and C-81) both Run nows, which keep today's behaviour.
	runScheduled runMode = iota
	// runAttended: the person started this run at this computer and is watching it.
	runAttended
)

// optsForJob builds the app.Options a scheduled (RunHeadless) or Run-now
// (RunJobInProcess) local backup uses from the job's recorded answers, so the two
// run the SAME way. inputs is the set to archive (the job's inputs, or the
// Outlook-recreated PSTs). When the job carries the Thunderbird pre-sync
// (j.ThunderbirdSync present, §4.2.5), it is installed as app.Run's BeforeRead
// hook through the shared seam (a no-op off macOS and Windows), attended only when
// mode says so. PresyncWanted records that the job asks for the sync and
// PresyncAttended the mode, apart from whether a hook was installed (C-33, C-34).
// It NEVER gates the archive (R32): a hook-setup failure is logged and the run
// proceeds without the sync. Everything it and the pre-sync write goes through
// logger one line at a time, so each line of mailarchive.log is dated (§4.8, T8).
func optsForJob(j job.Job, inputs []string, logger *log.Logger, mode runMode) app.Options {
	exportMode := export.Incremental
	if strings.EqualFold(j.Mode, "full") {
		exportMode = export.Full
	}
	var since time.Time
	if j.Since != "" {
		if t, perr := util.ParseSince(j.Since, time.Now()); perr == nil {
			since = t
		} else {
			logger.Printf("warning: ignoring invalid since %q: %v", j.Since, perr)
		}
	}
	opts := app.Options{
		Inputs: inputs, Auto: j.Auto, Out: j.Out, Mode: exportMode, Since: since,
		CopyFirst: j.CopyFirst, Index: true, Pages: true, KeepRaw: j.KeepRaw,
		Unattended: true, FollowLinks: j.FollowLinks,
	}
	opts.PresyncWanted = j.ThunderbirdSync != nil
	opts.PresyncAttended = mode == runAttended
	if j.ThunderbirdSync != nil {
		// A scheduled run names no button (neutral words); a Run now the Desktop's.
		surface := presync.SurfaceCLI
		if opts.PresyncAttended {
			surface = presync.SurfaceDesktop
		}
		h := presyncjob.HookOptions{
			Profile:  j.ThunderbirdSync.Profile,
			Log:      lineLogger{logger},
			Attended: opts.PresyncAttended,
			Surface:  surface,
		}
		if err := presyncjob.SetHook(&opts, h); err != nil {
			logger.Printf("warning: Thunderbird pre-sync not set up (%v); archiving without it", err)
		}
	}
	return opts
}

// lineLogger is the pre-sync's Log: each line written to it becomes one
// logger.Print, so it carries the logger's date and time (design-presync-attended
// §4.8, T8). An unterminated last piece is a line of its own; empty lines are
// dropped.
type lineLogger struct{ l *log.Logger }

func (w lineLogger) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			w.l.Print(line)
		}
	}
	return len(p), nil
}
