// Package health judges an archive: its completeness, its last run and its
// schedule, as a GREEN/WARN/RED posture whose every WARN/RED names its remedy
// (ux-contract X6, design-schedule-v2 §3.4). The CLI's `status` prints it; the
// GUI shows it on launch. Gather collects the facts; Assess is pure.
package health

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/procs"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// Input is everything known about an archive before judging it.
type Input struct {
	Out string

	// Surface names who reads the report: "" for the CLI (commands and SQLite's
	// words are fine), "desktop" for MailArchive Desktop's Status & health card,
	// "gui" for mailarchive-gui's health view. The owner surfaces name their own
	// rebuild action instead of a command line and leave SQLite's text out (F3).
	Surface string

	// GOOS shapes OS-specific wording (e.g. the staleness question: cron runs
	// unattended, launchd/schtasks need a login). Empty defaults to the host.
	GOOS string

	// OutUnderSync names the cloud-sync service (e.g. "OneDrive") when the
	// archive sits inside a synced folder; "" when it does not.
	OutUnderSync string

	// HasManifestFile is true when a manifest file exists on disk (even if it
	// cannot be loaded); HasManifest is true only when it loaded. A present-but-
	// unreadable manifest (newer format, corrupt) is a RED, never "no archive"
	// (friction #7a): ManifestLoadErr carries the loader's own wording.
	HasManifestFile bool
	ManifestLoadErr error

	HasManifest                 bool
	Messages                    int
	Fillable, Terminal, Unknown int
	// ShownAsSource counts records whose page is shown as plain text, per reason
	// (RV-10), from the manifest's body_fallback; nil when none.
	ShownAsSource map[string]int
	WithFixity                 int // records carrying at least one file digest (N of Messages)
	ReportPath                  string
	ReportExists                bool // the verification report exists on disk

	// Extractable is how many records have a preserved original (.eml) present
	// on disk (WithEML) out of the total (Total) — extract's migration signal
	// (design K4). It is FILE PRESENCE (an Lstat per record), never a recorded
	// digest, and is the same signal `extract` drains and `verify` counts, so the
	// three surfaces cannot diverge (QC1). health must not import app, so the
	// caller (`status`) computes it via app.ExtractableCount and sets it here;
	// it is display-only and never changes the posture.
	Extractable Extractable

	// HasIndex is true when search.db opened; HasIndexFile when it exists on
	// disk at all. A present index that cannot be opened (corrupt) carries the
	// open error in IndexErr and is a RED, never "no search index" (F82).
	//
	// IndexBusy marks an open that failed only because another connection kept
	// search.db locked past the busy timeout (index.Busy): the file is in use,
	// not damaged, so it is never index_unreadable.
	HasIndex     bool
	Indexed      int
	HasIndexFile bool
	IndexErr     error
	IndexBusy    bool

	// History is the go-back timeline's coverage (design §3.4, X6): whether the
	// log exists, how many runs and events it records, and whether it carries a
	// torn tail or an unreadable line. HistoryReadErr is a hard read failure. An
	// absent log is not a problem (a one-shot local import has none; a live
	// capture writes it on the next run); a torn tail is a recoverable WARN (the
	// next run repairs it); a hard read error is a RED.
	History        state.HistoryStat
	HistoryReadErr error

	// HasRange and the two bounds are the oldest/newest indexed message dates.
	HasRange                 bool
	RangeOldest, RangeNewest time.Time

	LastRun      state.LastRun
	LastRunState state.LastRunState
	LastRunErr   error

	// LastVerify is the separate record `verify` writes (its integrity verdict),
	// distinct from the export LastRun so a scheduled verify's result is visible
	// on status without disturbing the export record (UJ-A1).
	LastVerify      state.LastVerify
	LastVerifyState state.LastVerifyState
	LastVerifyErr   error

	// HasDescriptorFile is true when the descriptor file exists (even if it
	// cannot be read); HasDescriptor only when it loaded. `schedule -remove`
	// never deletes an unreadable descriptor, so status must still report one.
	HasDescriptorFile bool
	HasDescriptor     bool
	Desc              schedule.Descriptor
	DescErr           error
	// DescNameUnbound is true when the entry name the descriptor records cannot
	// be shown to be THIS archive's (a copied or planted record, G8-AO1): a
	// removal command is never printed from it. The zero value is a bound name.
	DescNameUnbound bool
	SchedState      schedule.State
	ExeExists       bool
	ExeIsThis       bool
	ThisHost        string

	// The scheduled verify's own descriptor (.mailarchive-verify-schedule.json,
	// owner ruling FB-4), written when `schedule` installs a verify: it carries
	// the verify's cadence, so verify_stale is judged against the verify itself.
	// HasVerifyDescriptorFile is true when the file exists (even if it cannot be
	// read), HasVerifyDescriptor only when it loaded; VerifySchedState is what the
	// host scheduler says about the name it records. A verify installed by an
	// earlier release has no such file (an even older one recorded the verify in
	// Desc, in the backup's place).
	HasVerifyDescriptorFile bool
	HasVerifyDescriptor     bool
	VerifyDesc              schedule.Descriptor
	VerifyDescErr           error
	VerifySchedState        schedule.State
	// VerifyExeExists / VerifyExeIsThis are the backup's ExeExists / ExeIsThis
	// for the program the verify descriptor records.
	VerifyExeExists bool
	VerifyExeIsThis bool
	// VerifyNameUnbound is DescNameUnbound for the verify descriptor's name.
	VerifyNameUnbound bool

	// LockHeld is the authoritative liveness signal: the archive lock dies
	// with its process, so a "running" record whose lock is free belongs to a
	// run that never finished (pid numbers are reused after a reboot).
	//
	// LockVerb is the verb the holder named in the lock's holder line
	// (lockfile.HolderVerb: "export", "verify", …), "" when the lock is free
	// or its holder names none (status's own probe). It says WHICH run holds
	// the lock, so a running record is live only while its own kind of run
	// holds it (runRunning, verifyRunning).
	LockHeld bool
	LockVerb string

	PIDAlive func(int) bool

	// WriteProbeErr is why a small file could not be written into the archive
	// folder (read-only, full, a lost drive), nil when it could. A run that
	// cannot write there cannot record its own failure, so status is where the
	// custodian sees it (F2).
	WriteProbeErr error
}

// Extractable is the extractability facet of an archive: WithEML of Total
// records have a preserved original (.eml) present on disk — the file-presence
// signal `extract` drains and `verify` counts (design K4). It is set by the
// caller from app.ExtractableCount (health must not import app); it is
// informational, like fixity coverage, and never changes the posture.
type Extractable struct {
	WithEML int
	Total   int
}

// Report is the judgement: a posture and the reasons behind any WARN/RED. Codes
// is a parallel array of stable, machine-readable snake_case identifiers — one
// per entry in Reasons — so a CI gate keys on a code instead of string-matching
// prose that can change (friction #12).
type Report struct {
	Posture string // GREEN | WARN | RED
	Reasons []string
	Codes   []string
}

func (r *Report) warn(code, msg string) {
	if r.Posture != "RED" {
		r.Posture = "WARN"
	}
	r.Reasons = append(r.Reasons, "WARN: "+msg)
	r.Codes = append(r.Codes, code)
}

func (r *Report) red(code, msg string) {
	r.Posture = "RED"
	r.Reasons = append(r.Reasons, "RED: "+msg)
	r.Codes = append(r.Codes, code)
}

// note appends an informational reason (no WARN/RED prefix, no posture change),
// keeping Reasons and Codes parallel so every reason carries a code.
func (r *Report) note(code, msg string) {
	r.Reasons = append(r.Reasons, msg)
	r.Codes = append(r.Codes, code)
}

// IntervalOf maps a cadence to its period.
func IntervalOf(iv string) time.Duration {
	switch schedule.Interval(iv) {
	case schedule.Hourly:
		return time.Hour
	case schedule.Weekly:
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// Assess applies the posture rules, failing closed on missing evidence.
func Assess(in Input, now time.Time) Report {
	r := Report{Posture: "GREEN"}

	// A manifest that exists on disk but cannot be loaded (a newer format, or a
	// corrupt/truncated file) is a RED that names the file, the loader's own
	// wording, and the remedy — never the "no archive here" advice status used
	// to give (friction #7a). The loader message is control-stripped: a corrupt
	// manifest can put arbitrary bytes into json's error text.
	if in.HasManifestFile && !in.HasManifest && in.ManifestLoadErr != nil {
		r.red("manifest_unreadable", fmt.Sprintf("the archive's manifest cannot be read: %s", util.StripControl(in.ManifestLoadErr.Error())))
	}

	// Completeness.
	if in.Fillable > 0 {
		msg := fmt.Sprintf("%d message(s) still missing content — download for offline use in your mail app, then re-run; incremental fills them", in.Fillable)
		if in.ReportExists {
			msg += fmt.Sprintf(" (see %s)", in.ReportPath)
		}
		r.warn("incomplete_content", msg)
	}
	if in.Unknown > 0 {
		r.warn("unexamined_entries", fmt.Sprintf("%d entr%s predate completeness tracking and are re-examined by incremental runs until none remain", in.Unknown, plural(in.Unknown, "y", "ies")))
	}
	// Plain `reindex` only prunes rows whose file is gone — it can never add a
	// missing row — so the remedy for an index that is short of the manifest is
	// the rebuild from the archive's own files (F50).
	if in.HasIndex && in.HasManifest && in.Indexed < in.Messages {
		r.warn("index_behind", fmt.Sprintf("the search index holds %d of %d messages — %s", in.Indexed, in.Messages, rebuildHow(in)))
	}
	// A search index that is present but cannot be read is a RED (search, serve
	// and the next run all fail on it); one that is gone from an archive that
	// records messages is a WARN unless the last run deliberately skipped
	// indexing (-index=false). Both name the rebuild from the archive's own
	// files (F82). The open error is control-stripped like the manifest's.
	idxPath := filepath.Join(in.Out, "search.db")
	switch {
	case in.HasIndexFile && !in.HasIndex && in.IndexErr != nil && in.IndexBusy:
		// In use, not damaged: never the rebuild remedy. Under a run's archive
		// lock that is expected and only a note; with the lock free something
		// outside mailarchive held it and the counts are unknown, so a WARN.
		msg := fmt.Sprintf("the search index %s was busy (kept locked by another connection) and could not be read just now, so its counts are not shown", idxPath)
		if in.LockHeld {
			r.note("index_busy", msg+" — a run is using this archive; run status again when it finishes")
		} else {
			r.warn("index_busy", msg+" — close any other program that has it open, then run status again")
		}
	case in.HasIndexFile && !in.HasIndex && in.IndexErr != nil:
		// The rebuild needs the archive's manifest (it refuses without one), so
		// with none here the remedy that works is moving the file aside (F6).
		remedy := rebuildHow(in)
		if !in.HasManifest && !in.HasManifestFile {
			remedy = fmt.Sprintf("there is no manifest in %s to rebuild it from, so move search.db out of that folder; the next run builds a fresh index", in.Out)
		}
		cause := fmt.Sprintf(" (%s)", util.StripControl(indexOpenCause(in.IndexErr)))
		if in.Surface != "" {
			cause = "" // SQLite's words are for the log and -json, not the owner
		}
		r.red("index_unreadable", fmt.Sprintf("the search index %s cannot be read%s — search, serve and the folder pages need it; %s", idxPath, cause, remedy))
	case !in.HasIndex && !in.HasIndexFile && in.HasManifest && in.Messages > 0 && !(in.LastRunState == state.LastRunPresent && in.LastRun.NoIndex):
		r.warn("index_missing", fmt.Sprintf("the archive has no search index (%s) although the manifest records %d message(s) — search, serve and the folder pages need it; %s (or ignore this if the archive is deliberately kept without one, -index=false)", idxPath, in.Messages, rebuildHow(in)))
	}

	// The archive's own location. A cloud-sync folder re-uploads every change
	// and can conflict with the sync (P3).
	if in.OutUnderSync != "" {
		r.warn("cloud_sync", fmt.Sprintf("the archive is inside %s's synced folder — a backup here re-uploads on every change and can corrupt during a sync; use a local, non-synced folder, or exclude it from sync / keep it always on this device", in.OutUnderSync))
	}

	// Last run.
	period := 24 * time.Hour
	if in.HasDescriptor {
		period = IntervalOf(in.Desc.Interval)
	}
	switch in.LastRunState {
	case state.LastRunUnreadable:
		r.warn("lastrun_unreadable", fmt.Sprintf("the last-run record %s is unreadable (corrupt or truncated); the next run rewrites it", filepath.Join(in.Out, state.LastRunName)))
	case state.LastRunAbsent:
		if in.HasDescriptor {
			r.warn("never_ran", "a schedule is installed but no run has ever been recorded here — wait for the first run, or run the job by hand to check it works")
		}
	case state.LastRunPresent:
		lr := in.LastRun
		switch lr.Status {
		case state.RunFailed:
			r.red("run_failed", fmt.Sprintf("the last run (%s) FAILED: %s", lr.Started.Local().Format("2006-01-02 15:04"), runErrorText(in, lr)))
			if looksLikeAuthFailure(lr.Error) {
				r.note("auth_remedy", "  remedy: authentication failed — for an app-auth job the client secret may have expired (rotate it in Entra and rewrite the secret file); for a device-auth job the sign-in may have expired (re-run `mailarchive graph -auth device …` to sign in again)")
			}
		case state.RunRunning:
			alive := runRunning(in)
			switch {
			case alive && now.Sub(lr.Started) <= period:
				r.note("run_in_progress", fmt.Sprintf("a run is in progress (pid %d, started %s)", lr.PID, lr.Started.Local().Format("2006-01-02 15:04")))
			case alive:
				r.red("run_stuck", fmt.Sprintf("a run started %s is still running (pid %d) — longer than the schedule period; check the log", lr.Started.Local().Format("2006-01-02 15:04"), lr.PID))
			default:
				r.red("run_never_finished", fmt.Sprintf("the run started %s never finished (pid %d is gone: crash, kill, or power loss) — check the log and run again", lr.Started.Local().Format("2006-01-02 15:04"), lr.PID))
			}
		case state.RunCancelled:
			r.warn("run_cancelled", fmt.Sprintf("the last run (%s) was cancelled before finishing; progress was kept", lr.Started.Local().Format("2006-01-02 15:04")))
		}
		if in.HasDescriptor && lr.Status != state.RunRunning && now.Sub(lr.Started) > 2*period {
			r.warn("stale", fmt.Sprintf("the last run was %s ago, more than twice the %s schedule period — %s", HumanAge(now.Sub(lr.Started)), in.Desc.Interval, stalenessQuestion(in.GOOS, in.Desc.At)))
		}
		// Problems the last run found with its own setup and kept running past
		// (design-local-confidentiality P10): a credential, or the desktop's
		// settings and sign-in, inside the archive. The recorded text names the
		// file, the archive and the remedy. Only the codes defined here are shown
		// (the record is writable by anyone who can write the archive), and they
		// come from the last run alone, so a run without the problem clears them.
		for _, w := range lr.Warnings {
			switch w.Code {
			case "credential_in_archive", "config_in_archive":
				r.warn(w.Code, w.Message)
			}
		}
	}

	// Last verify: the archive's integrity verdict, from verify's own record.
	// modified/missing is real corruption (RED); unrecorded-only is a coverage
	// gap (WARN); a verify that has not run for more than twice its own
	// schedule's cadence is stale (WARN, assessVerifySchedule).
	//
	// A "running" record is judged by the finished verdict it carries (F79): a
	// verify in progress (the archive lock held by a verify, verifyRunning) is
	// a note; otherwise that verify never finished (crash, kill, power loss)
	// and is a WARN — and either way the previous verdict, a not-attested RED
	// included, is still reported.
	lv, judge := in.LastVerify, in.LastVerifyState == state.LastVerifyPresent
	if judge && lv.Status == state.VerifyRunning {
		started := lv.Started.UTC().Format("2006-01-02 15:04")
		if verifyRunning(in) {
			r.note("verify_in_progress", fmt.Sprintf("a verify is in progress (started %s UTC)", started))
		} else {
			// With a scheduled verify recorded, name its log and the run that
			// replaces this record. assessVerifySchedule does not repeat this
			// reason: this block is the single owner of verify_never_finished.
			sched := ""
			if in.HasVerifyDescriptor {
				sched = fmt.Sprintf("; check its log %s (the verify started %s local time) — the next scheduled run of %q also replaces this record", verifyLogHint(in), lv.Started.Local().Format("2006-01-02 15:04"), in.VerifyDesc.Name)
			}
			r.warn("verify_never_finished", fmt.Sprintf("the verify started %s UTC never finished (Ctrl-C, crash, kill, or power loss) — run `mailarchive verify -out %q` again; the last finished verify's verdict is still reported%s", started, in.Out, sched))
		}
		judge = lv.Previous != nil
		if judge {
			lv = *lv.Previous
		}
	}
	if judge && lv.Status == state.VerifyDone {
		when := ""
		if lv.Finished != nil {
			when = lv.Finished.UTC().Format("2006-01-02 15:04")
		}
		switch {
		case lv.Modified+lv.Missing > 0:
			r.red("not_attested", fmt.Sprintf("the last verify (%s UTC) found the archive NOT intact: %d modified, %d missing — restore the affected files from a backup or re-export them from the source, then run `mailarchive verify -out %q` again", when, lv.Modified, lv.Missing, in.Out))
		case lv.Unrecorded > 0:
			r.warn("verify_unrecorded", fmt.Sprintf("the last verify (%s UTC) left %d file(s) with no recorded fixity — run `mailarchive verify -record -out %q` to baseline them", when, lv.Unrecorded, in.Out))
		}
		// Exit 1 on a produced report is an error after the check (a -record
		// baseline whose save failed): its counts stand, but it did not finish
		// cleanly, so say so (F79).
		if lv.ExitCode == 1 {
			r.warn("verify_failed", fmt.Sprintf("the last verify (%s UTC) checked the archive but then failed with an error (see its log) — fix the cause and run `mailarchive verify -out %q` again", when, in.Out))
		}
	}
	assessVerifySchedule(&r, in, now)

	// Go-back timeline coverage (design §3.4, X6). An absent log is not a
	// problem (a one-shot local import has none; a live archive writes it on its
	// next run), so it never warns. A hard read failure is a RED (a past-date
	// view is unavailable); a present log with a torn tail is a recoverable WARN
	// the next run heals; other unreadable lines are a WARN that reindex compacts.
	switch {
	case in.HistoryReadErr != nil:
		r.red("history_unreadable", fmt.Sprintf("the go-back history log %s could not be read (%s) — the point-in-time view is unavailable; run `mailarchive reindex -out %q` to compact and heal the log", filepath.Join(in.Out, state.HistoryName), util.StripControl(in.HistoryReadErr.Error()), in.Out))
	case in.History.Exists && in.History.TornTail && in.History.BadLines <= 1:
		r.warn("history_torn_tail", fmt.Sprintf("the go-back history log has a torn tail (a run crashed mid-line) — the next run repairs it, or run `mailarchive reindex -out %q` to compact and heal it now; past-date views may be incomplete until then", in.Out))
	case in.History.Exists && (in.History.BadLines > 0 || in.History.TornTail):
		r.warn("history_corrupt", fmt.Sprintf("the go-back history log has %d unreadable line(s) — those events are skipped, so past-date views may be incomplete; run `mailarchive reindex -out %q` to compact the log", in.History.BadLines, in.Out))
	}

	// A scheduled backup that cannot write into its archive folder fails every
	// run and cannot record the failure there (no last-run record, no attention
	// file), so the previous ok would stand: say it here (F2). Only for a
	// schedule recorded for this host — a read-only copy with no schedule, or
	// another machine's job, is not a failing backup.
	if in.WriteProbeErr != nil && in.HasDescriptor && (in.ThisHost == "" || in.Desc.Host == "" || in.ThisHost == in.Desc.Host) {
		remedy := "make the folder writable (or remount the drive read-write)"
		if diskFull(in.WriteProbeErr) {
			remedy = "free space on the disk that holds it"
		}
		r.red("archive_unwritable", fmt.Sprintf("the archive folder %s cannot be written (%s, as the account running status; if the backup runs as another account, check that one) — a scheduled backup that cannot write there cannot save its manifest, its last-run record or this failure, so the last run shown above may be out of date; %s, then run the job again", in.Out, util.StripControl(probeCause(in.WriteProbeErr)), remedy))
	}

	// Schedule.
	switch {
	case in.DescErr != nil && !errors.Is(in.DescErr, os.ErrNotExist):
		r.warn("descriptor_unreadable", in.DescErr.Error())
	case !in.HasDescriptor:
		r.warn("no_schedule", fmt.Sprintf("no schedule is recorded for this archive — %s (or the GUI's \"Keep this archive current\"), or ignore this if a systemd timer, NAS task or other scheduler already keeps it current", scheduleRemedy(in)))
	default:
		if jo := jobOutOf(in.Desc.Job); jo != "" && filepath.IsAbs(jo) && filepath.IsAbs(in.Out) && !sameArchivePath(jo, in.Out, in.GOOS) {
			// Substitute this archive's job into the re-install remedy (no "..."
			// placeholder) so the operator can paste it (friction #14).
			reinstall := scheduleCommand(jobWithOut(in.Desc.Job, in.Out), in.GOOS)
			if in.DescNameUnbound {
				// The record is untrusted: an entry it names that is not shown to be
				// this archive's may be another archive's live one, so no removal
				// command is printed from it (G8-AO1).
				r.warn("moved_archive", fmt.Sprintf("the installed schedule %q backs up %s, not this archive (%s) — re-run %s; this record is not shown to be this archive's own, so it names no entry to remove: if it was copied here, delete %s", in.Desc.Name, jo, in.Out, reinstall, filepath.Join(in.Out, schedule.DescriptorName)))
			} else {
				r.warn("moved_archive", fmt.Sprintf("the installed schedule %q backs up %s, not this archive (%s) — re-run %s, then remove the stale one with `mailarchive schedule -name %q -remove`", in.Desc.Name, jo, in.Out, reinstall, in.Desc.Name))
			}
		}
		if in.ThisHost != "" && in.Desc.Host != "" && in.ThisHost != in.Desc.Host {
			r.warn("other_host", fmt.Sprintf("the schedule was installed on host %q, not this one (%q); its state cannot be checked from here", in.Desc.Host, in.ThisHost))
		} else {
			switch in.SchedState {
			case schedule.SchedulerUnavailable:
				r.warn("scheduler_unavailable", "this host's scheduler cannot be queried (no crontab/schtasks?) — the schedule may not run here")
			case schedule.NotInstalled:
				r.warn("not_installed", fmt.Sprintf("the schedule %q is recorded but not present in this host's scheduler — re-run `mailarchive schedule ... -install`", in.Desc.Name))
			}
			if !in.ExeExists {
				r.red("exe_missing", fmt.Sprintf("the scheduled program %s no longer exists — the job cannot run; re-run `mailarchive schedule ... -install` from the current binary", in.Desc.Exe))
			} else if !in.ExeIsThis {
				r.warn("exe_moved", fmt.Sprintf("the schedule runs %s, which is not this binary — after an upgrade or move, re-run `mailarchive schedule ... -install`", in.Desc.Exe))
			}
		}
	}
	assessVerifyEntry(&r, in)
	r.worstFirst()
	return r
}

// worstFirst orders the reasons RED, then WARN, then notes (F20), keeping the
// check order within each class and every indented remedy line ("  remedy: …")
// directly under the reason it belongs to. Reasons and Codes stay parallel.
func (r *Report) worstFirst() {
	type group struct {
		reasons, codes []string
		rank           int
	}
	var groups []group
	for i, reason := range r.Reasons {
		if strings.HasPrefix(reason, "  ") && len(groups) > 0 {
			g := &groups[len(groups)-1]
			g.reasons, g.codes = append(g.reasons, reason), append(g.codes, r.Codes[i])
			continue
		}
		rank := 2
		switch {
		case strings.HasPrefix(reason, "RED:"):
			rank = 0
		case strings.HasPrefix(reason, "WARN:"):
			rank = 1
		}
		groups = append(groups, group{[]string{reason}, []string{r.Codes[i]}, rank})
	}
	var reasons, codes []string
	for rank := 0; rank <= 2; rank++ {
		for _, g := range groups {
			if g.rank == rank {
				reasons, codes = append(reasons, g.reasons...), append(codes, g.codes...)
			}
		}
	}
	r.Reasons, r.Codes = reasons, codes
}

// probeCause is the operating system's own words for a failed write probe,
// without the probe file's name (the reason names the folder).
func probeCause(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// diskFull reports whether err is the operating system's "no space left".
func diskFull(err error) bool {
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	var en syscall.Errno
	if runtime.GOOS == "windows" && errors.As(err, &en) {
		return en == 112 || en == 39 // ERROR_DISK_FULL, ERROR_HANDLE_DISK_FULL
	}
	return false
}

// probeWrite writes and removes a small file in dir, enough (64 KiB, synced)
// that a full disk or a failing drive refuses it; nil when dir takes a write.
func probeWrite(dir string) error {
	f, err := os.CreateTemp(dir, ".mailarchive-status-probe-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(make([]byte, 64<<10)); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// rebuildHow is the remedy sentence for a damaged, missing or lagging search
// index, in the words of the surface reading it (F3): the CLI's command, the
// desktop's Maintenance button, the mailarchive-gui health view's choice.
func rebuildHow(in Input) string {
	switch in.Surface {
	case "desktop":
		return "choose Maintenance → Rebuild search index to rebuild it from your archive"
	case "gui":
		return "choose \"Rebuild the search index\" below to rebuild it from your archive"
	}
	return fmt.Sprintf("rebuild it from the archive with `mailarchive reindex -rebuild -out %q`", in.Out)
}

// runErrorText is the last run's failure as the surface reading it should see
// it. The CLI and -json print what the run recorded (its command and SQLite's
// words). An owner surface (desktop, gui) prints a search-index refusal, which
// the record names by kind, in plain words with that surface's own rebuild
// action, so the health view never tells an owner with no command line to run
// one (g7 friction re-verification of F3). Any other failure is verbatim.
func runErrorText(in Input, lr state.LastRun) string {
	if in.Surface == "" {
		return lr.Error
	}
	switch lr.ErrorKind {
	case state.ErrKindIndexBusy:
		return "the search index is in use by another program — close any other program that has it open, then try again"
	case state.ErrKindIndexUnwritable:
		return "the archive cannot be written to (a read-only folder or file, or a full or failing disk) — make it writable or free some space, then try again"
	case state.ErrKindIndexDamaged:
		return "the search index is damaged — " + rebuildHow(in) + ", then try again"
	case state.ErrKindIndexDamagedNoManifest:
		return fmt.Sprintf("the search index is damaged, and the archive has no manifest to rebuild it from — move search.db out of %s, then try again; a fresh index is built", in.Out)
	}
	return lr.Error
}

// verifyRunning reports whether a verify is running now: the archive lock is
// held AND its holder is a verify. Every verify names itself in the holder
// line (lockfile.AcquireAs "verify"), so a lock held by another verb (an
// export, a reindex), by a holder that names none (status's own probe), or no
// lock at all means a "running" verify record belongs to a verify that never
// finished.
func verifyRunning(in Input) bool {
	return in.LockHeld && in.LockVerb == "verify"
}

// runRunning reports whether the "running" last-run record — which only an
// export or a Graph run writes — can belong to a run in progress now: the
// archive lock is held by an export or a graph run, or by a holder that names
// no verb (status's own probe, or a binary older than c19f429, which wrote
// none — counted as live so a mixed-version install shows no false RED). A
// lock held by a verify, a reindex, an extract or any other verb cannot be
// that run, so the record belongs to a run that never finished (R18: fail
// closed), the same rule verifyRunning applies to a running verify record.
func runRunning(in Input) bool {
	if !in.LockHeld {
		return false
	}
	switch in.LockVerb {
	case "export", "graph", "":
		return true
	}
	return false
}

// indexOpenCause is what the index_unreadable reason says about a failed
// search.db open: SQLite's own root cause when OpenReadonly typed its refusal
// (index.UnreadableError), because the reason names the file and the rebuild
// itself and the full refusal would print both twice; the whole error
// otherwise.
func indexOpenCause(err error) string {
	var ue *index.UnreadableError
	if errors.As(err, &ue) && ue.Cause != nil {
		return ue.Cause.Error()
	}
	return err.Error()
}

// assessVerifyEntry watches the scheduled verify's entry the way the Schedule
// section above watches the backup's (MA-466): recorded on another host, its
// state cannot be checked from here (verify_other_host); on this host a verify
// the scheduler does not have is verify_not_installed, a scheduler that cannot
// be queried is scheduler_unavailable (once, with the backup's), and a recorded
// program that is gone or is not this binary is verify_exe_missing or
// verify_exe_moved, each naming the re-install. A verify that cannot run is a
// WARN, as a stale verify is: RED stays for a backup that cannot run and for
// corruption a verify found.
func assessVerifyEntry(r *Report, in Input) {
	if !in.HasVerifyDescriptor {
		return
	}
	vd := in.VerifyDesc
	if in.ThisHost != "" && vd.Host != "" && in.ThisHost != vd.Host {
		r.warn("verify_other_host", fmt.Sprintf("the verify schedule %q was installed on host %q, not this one (%q); its state cannot be checked from here", vd.Name, vd.Host, in.ThisHost))
		return
	}
	reinstall := verifyReinstall(in)
	switch in.VerifySchedState {
	case schedule.NotInstalled:
		clearCmd := "`mailarchive schedule " + strings.Join(quoteTokens([]string{"-name", vd.Name, "-remove", "--", "verify", "-out", in.Out}, in.GOOS), " ") + "`"
		clearHow := "with"
		if in.VerifyNameUnbound {
			// Not shown to be this archive's entry (G8-AO1): no removal command
			// from an untrusted record; the file is the operator's to delete.
			clearHow, clearCmd = "by deleting", filepath.Join(in.Out, schedule.VerifyDescriptorName)
		}
		r.warn("verify_not_installed", fmt.Sprintf("the verify schedule %q is recorded but not present in this host's scheduler — re-install it with %s, or, if this archive should have no scheduled verify, clear the record %s %s", vd.Name, reinstall, clearHow, clearCmd))
	case schedule.SchedulerUnavailable:
		if !r.has("scheduler_unavailable") {
			r.warn("scheduler_unavailable", "this host's scheduler cannot be queried (no crontab/schtasks?) — the scheduled verify may not run here")
		}
	}
	if !in.VerifyExeExists {
		r.warn("verify_exe_missing", fmt.Sprintf("the scheduled verify's program %s no longer exists — the verify cannot run; re-install it with %s from the current binary", vd.Exe, reinstall))
	} else if !in.VerifyExeIsThis {
		r.warn("verify_exe_moved", fmt.Sprintf("the scheduled verify runs %s, which is not this binary — after an upgrade or move, re-install it with %s", vd.Exe, reinstall))
	}
}

// has reports whether the report already carries code.
func (r *Report) has(code string) bool {
	for _, c := range r.Codes {
		if c == code {
			return true
		}
	}
	return false
}

// assessVerifySchedule judges the scheduled verify against the cadence recorded
// for the verify itself (UJ-A1, owner ruling FB-4), never the backup's. The
// verify's own descriptor decides when it loads: a verify whose last run
// finished more than twice its period ago is stale, and so is one that has
// recorded no run in twice its period since it was installed. A last verify
// still recorded as running (verify writes "running" when it takes the lock and
// finalizes the record on the way out) is judged from when it started: no
// verify has finished since then, and none has started after it, so one started
// more than twice the period ago is stale whether or not it still holds the
// lock. With the lock free its process is gone (the lock dies with it), so it
// never finished: a WARN at once, so a verify that dies on every run is never
// GREEN. A verify schedule installed (or re-installed) after the last verify
// ran has not stopped: within two of its periods of the install a stale last
// verify still raises verify_stale (MA-466), but the WARN says the schedule has
// not run yet, naming its next run and a verify now — never a re-install,
// which would change nothing; later, no verify since the install is said so,
// with the re-install remedy. A last-verify record that cannot be read gives no
// verdict and no date to judge by, so it is a WARN too (failing closed, R18). A
// verify descriptor
// that exists but cannot be read is a WARN naming it and its remedy, and gives
// no cadence. Without a readable one, a descriptor an older release wrote for
// the verify in the backup's place still gives the cadence (MA-155); a verify
// installed by an earlier release records none, so its staleness is not judged
// — a guessed cadence would raise a false WARN.
func assessVerifySchedule(r *Report, in Input, now time.Time) {
	lv := in.LastVerify
	done := in.LastVerifyState == state.LastVerifyPresent && lv.Status == state.VerifyDone && lv.Finished != nil
	if in.HasVerifyDescriptor {
		vd := in.VerifyDesc
		period := IntervalOf(vd.Interval)
		pending := in.LastVerifyState == state.LastVerifyPresent && !done
		// When the last verify ran: its finish, or the start of one that has not
		// finished. The verify schedule may have been installed (or re-installed)
		// after it, and then it has not stopped: it has not run yet.
		var last time.Time
		lastWhat := ""
		switch {
		case done:
			last, lastWhat = *lv.Finished, fmt.Sprintf("the last verify was %s ago", HumanAge(now.Sub(*lv.Finished)))
		case pending:
			last, lastWhat = lv.Started, fmt.Sprintf("the last verify started %s ago and has not finished", HumanAge(now.Sub(lv.Started)))
		}
		switch {
		case !last.IsZero() && now.Sub(last) > 2*period && vd.InstalledAt.After(last) && now.Sub(vd.InstalledAt) <= 2*period:
			// Re-installing would change nothing here; a verify run now clears it.
			r.warn("verify_stale", fmt.Sprintf("%s, more than twice the %s verify schedule period of %q — that schedule was installed %s ago, after the last verify, and has not run since: wait for its next run (%s), or run `mailarchive verify -out %q` now", lastWhat, vd.Interval, vd.Name, HumanAge(now.Sub(vd.InstalledAt)), cadenceOf(vd), in.Out))
		case !last.IsZero() && now.Sub(last) > 2*period && vd.InstalledAt.After(last):
			r.warn("verify_stale", fmt.Sprintf("no verify has run in the %s since the %s verify schedule %q was installed, more than twice its period (%s, before the install) — %s", HumanAge(now.Sub(vd.InstalledAt)), vd.Interval, vd.Name, lastWhat, verifyStaleRemedy(in)))
		case !last.IsZero() && now.Sub(last) > 2*period:
			r.warn("verify_stale", fmt.Sprintf("%s, more than twice the %s verify schedule period of %q — %s", lastWhat, vd.Interval, vd.Name, verifyStaleRemedy(in)))
		case in.LastVerifyState == state.LastVerifyAbsent && !vd.InstalledAt.IsZero() && now.Sub(vd.InstalledAt) > 2*period:
			r.warn("verify_stale", fmt.Sprintf("no verify has run in the %s since the %s verify schedule %q was installed, more than twice its period — %s", HumanAge(now.Sub(vd.InstalledAt)), vd.Interval, vd.Name, verifyStaleRemedy(in)))
		}
		// A running record whose verify is gone (verify_never_finished) is
		// reported once, by the last-verify block in Assess, which also knows
		// which verb holds the lock; this function only judges its staleness.
		if in.LastVerifyState == state.LastVerifyUnreadable {
			cause := "corrupt or truncated"
			if in.LastVerifyErr != nil {
				cause = util.StripControl(in.LastVerifyErr.Error())
			}
			r.warn("lastverify_unreadable", fmt.Sprintf("the last-verify record %s is unreadable (%s), so the scheduled verify %q cannot be judged — the next verify rewrites it, or run `mailarchive verify -out %q` now", filepath.Join(in.Out, state.LastVerifyName), cause, vd.Name, in.Out))
		}
		return
	}
	if in.VerifyDescErr != nil && !errors.Is(in.VerifyDescErr, os.ErrNotExist) {
		r.warn("verify_descriptor_unreadable", in.VerifyDescErr.Error())
	}
	if isVerifyJob(in.Desc.Job) && done && now.Sub(*lv.Finished) > 2*IntervalOf(in.Desc.Interval) {
		r.warn("verify_stale", fmt.Sprintf("the last verify was %s ago, more than twice the %s verify schedule period — schedule it, or run `mailarchive verify -out %q`", HumanAge(now.Sub(*lv.Finished)), in.Desc.Interval, in.Out))
	}
}

// cadenceOf renders a recorded schedule's cadence for a remedy, e.g. "weekly at
// 05:00"; a time the descriptor could not parse ("??:??") is left out.
func cadenceOf(d schedule.Descriptor) string {
	if strings.Contains(d.At, "?") {
		return d.Interval
	}
	return d.Interval + " at " + d.At
}

// verifyReinstall is the pasteable command that puts this archive's scheduled
// verify back: its recorded cadence, time and verify flags, aimed at this
// archive and without -name, so it gets this archive's own entry name (on
// launchd and Task Scheduler a -name another archive's verify shares is not
// detected and would be taken over again, FB-4's residual). Every token is
// shell-quoted for the host's shell.
func verifyReinstall(in Input) string {
	vd := in.VerifyDesc
	toks := []string{"-interval", vd.Interval}
	if !strings.Contains(vd.At, "?") { // ReadDescriptor parks an unreadable time as "??:??"
		toks = append(toks, "-at", vd.At)
	}
	toks = append(toks, "-install", "--", "verify", "-out", in.Out)
	for _, a := range vd.Job {
		if a == "-record" || a == "-json" {
			toks = append(toks, a)
		}
	}
	return "`mailarchive schedule " + strings.Join(quoteTokens(toks, in.GOOS), " ") + "`"
}

// verifyLogHint names where the scheduled verify's log is: the descriptor's, or
// the run log's default place beside the archive.
func verifyLogHint(in Input) string {
	if in.VerifyDesc.Log != "" {
		return in.VerifyDesc.Log
	}
	return filepath.Join(in.Out, in.VerifyDesc.Name+".log")
}

// verifyStaleRemedy is what to do about a stale scheduled verify: put it back
// (verifyReinstall), or run a verify now.
func verifyStaleRemedy(in Input) string {
	// A last verify still recorded as running while a process holds the archive
	// lock is running now (or hung): re-installing changes nothing, and another
	// verify would collide with the lock (K3).
	if in.LockHeld && in.LastVerifyState == state.LastVerifyPresent && in.LastVerify.Status == state.VerifyRunning {
		return fmt.Sprintf("a verify holds the archive lock and is still running: let it finish (its log is %s), or, if it is hung, end its process and run `mailarchive verify -out %q`", verifyLogHint(in), in.Out)
	}
	reinstall := verifyReinstall(in)
	why := "it has stopped running here"
	goos := in.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd":
	default:
		why = "it has stopped running, or another archive's verify installed under the same -name replaced it (on macOS and Windows a shared -name is not detected)"
	}
	return fmt.Sprintf("%s: re-install it with %s (no -name, so it has this archive's own entry), or run `mailarchive verify -out %q`", why, reinstall, in.Out)
}

// sameArchivePath reports whether two -out spellings name the same directory.
// It prefers os.SameFile when both paths stat (collapsing symlink, 8.3/long and
// mapped-drive spellings); when a stat is unavailable it folds case only on the
// case-insensitive volumes (darwin/windows), keeping an exact byte-compare on
// linux so a genuinely different-cased directory there still warns (UJ-A4).
func sameArchivePath(a, b, goos string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(fa, fb)
	}
	if goos == "darwin" || goos == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return false
}

// isVerifyJob reports whether a scheduler job is a `verify` job (its verb, the
// first argument, is "verify").
func isVerifyJob(job []string) bool {
	return len(job) > 0 && job[0] == "verify"
}

// jobWithOut returns a copy of job with its -out value replaced by out (both
// `-out V` and `-out=V` spellings), appending `-out out` when the job carries
// none, so a remedy always targets the archive actually being inspected.
func jobWithOut(job []string, out string) []string {
	cp := append([]string(nil), job...)
	for i := 0; i < len(cp); i++ {
		a := strings.TrimLeft(cp[i], "-")
		if a == "out" && i+1 < len(cp) {
			cp[i+1] = out
			return cp
		}
		if strings.HasPrefix(a, "out=") {
			dashes := cp[i][:len(cp[i])-len(a)]
			cp[i] = dashes + "out=" + out
			return cp
		}
	}
	return append(cp, "-out", out)
}

func looksLikeAuthFailure(msg string) bool {
	m := strings.ToLower(msg)
	for _, needle := range []string{"401", "unauthorized", "invalid_client", "invalid_grant", "aadsts", "authentication", "invalid client secret", "sign in", "sign-in", "token cache", "device sign"} {
		if strings.Contains(m, needle) {
			return true
		}
	}
	return false
}

// HumanAge renders a duration for a human.
func HumanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.0f h", d.Hours())
	default:
		return fmt.Sprintf("%.0f days", d.Hours()/24)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// stalenessQuestion phrases the "why might it have missed a run?" prompt for the
// scheduler the archive's OS uses: cron runs unattended whenever the machine is
// on and crond is up (no login), while launchd and Task Scheduler run only in a
// logged-in session.
func stalenessQuestion(goos, at string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd":
		return fmt.Sprintf("was the machine on (and crond running) at %s?", at)
	default:
		return fmt.Sprintf("is the machine on and logged in at %s?", at)
	}
}

// scheduleRemedy shapes the "keep it current" advice for an archive with no
// recorded schedule from the job that actually made it (friction #7). Older
// records and Graph runs carry no job; it then names a generic phrase rather
// than hard-coding -auto.
func scheduleRemedy(in Input) string {
	if in.LastRunState == state.LastRunPresent && len(in.LastRun.Job) > 0 {
		job := in.LastRun.Job
		// If the archive has moved since the job was recorded, target its actual
		// -out so the pasted command backs up THIS directory (friction #13).
		if jo := jobOutOf(job); jo != "" && in.Out != "" && filepath.Clean(jo) != filepath.Clean(in.Out) {
			job = jobWithOut(job, in.Out)
		}
		return "keep it current by scheduling the same job that made this archive: " + scheduleCommand(job, in.GOOS)
	}
	return "keep it current by scheduling the same job that made this archive (e.g. `mailarchive schedule -out " + fmt.Sprintf("%q", in.Out) + " ... -install`)"
}

// scheduleCommand renders a runnable `schedule … -install` line that repeats a
// recorded job: an export job (no verb) on the flat form, a graph/reindex/verify
// job after `--`. Each token is shell-quoted for goos so a job element carrying
// a space or a shell metacharacter (the record is attacker-writable) is inert
// when pasted (INS2-2); control characters were already stripped at read.
func scheduleCommand(job []string, goos string) string {
	q := quoteTokens(job, goos)
	if len(job) > 0 && (job[0] == "graph" || job[0] == "reindex" || job[0] == "verify") {
		return "`mailarchive schedule -install -- " + strings.Join(q, " ") + "`"
	}
	return "`mailarchive schedule " + strings.Join(q, " ") + " -install`"
}

// quoteTokens shell-quotes every token that needs it, leaving the common case
// (a bare flag or an unproblematic path) untouched.
func quoteTokens(toks []string, goos string) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = shellQuoteToken(t, goos)
	}
	return out
}

// shellQuoteToken quotes s for the given OS's shell only when it contains
// whitespace or a shell metacharacter, so a pasted remedy is both inert and
// (for a benign spaced path) correct. POSIX single-quoting does not interpret
// $ or backticks (unlike Go's %q double-quoted form); Windows uses double
// quotes with doubled embedded quotes.
func shellQuoteToken(s, goos string) string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" && windowsExpands(s) {
		// No quoting is inert in both cmd and PowerShell (the Windows terminal's
		// default): the token is not pasted. `<` makes either shell refuse the
		// line, so the operator fills the value in by hand (G8-AO2).
		return unsafeTokenPlaceholder
	}
	if !needsShellQuoting(s) {
		return s
	}
	if goos == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// unsafeTokenPlaceholder stands in for a Windows token no quoting makes inert.
const unsafeTokenPlaceholder = "<edit-this-value-by-hand>"

// windowsExpands reports whether a Windows shell would expand something in s even
// inside double quotes: cmd's %VAR% and ^ escape, PowerShell's backtick escape,
// $(...), ${...} and $name / $env:name / $? variables. A `$` that ends the token
// or is followed by a path separator (an administrative share, \\nas\backup$) is
// literal in both.
func windowsExpands(s string) bool {
	// U+201C, U+201D and U+201E are double quotes to PowerShell: one inside the
	// token closes the string the remedy wraps it in (G8-AO2).
	if strings.ContainsAny(s, "%^`“”„") {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 == len(s) {
			continue
		}
		switch c := s[i+1]; {
		case c == '\\', c == '/', c == ' ', c == '"', c == '.', c == '-':
		default:
			return true
		}
	}
	return false
}

// needsShellQuoting reports whether s must be quoted to survive a shell as one
// inert word: any whitespace/control byte, or any character a shell treats
// specially. An empty token must also be quoted (it would otherwise vanish).
func needsShellQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r <= ' ' {
			return true
		}
		if strings.ContainsRune("`~#$&*()\\|[]{};'\"<>?! ", r) {
			return true
		}
	}
	return false
}

// jobOutOf returns the -out value carried by a scheduler job's arguments,
// handling both `-out V` and `-out=V` (and their `--out` spellings); "" when
// absent.
func jobOutOf(job []string) string {
	for i := 0; i < len(job); i++ {
		a := strings.TrimLeft(job[i], "-")
		if a == "out" && i+1 < len(job) {
			return job[i+1]
		}
		if strings.HasPrefix(a, "out=") {
			return a[len("out="):]
		}
	}
	return ""
}

// Gather collects the facts about an archive. nameOverride checks that
// schedule name instead of the descriptor's.
func Gather(out, nameOverride string) Input {
	in := Input{Out: out, GOOS: runtime.GOOS, PIDAlive: procs.Alive}
	in.ThisHost, _ = os.Hostname()
	if svc, ok := util.UnderCloudSync(out); ok {
		in.OutUnderSync = svc
	}
	// Probe the archive lock: held means a run is genuinely in progress, and
	// the holder line says which verb holds it.
	lockPath := filepath.Join(out, lockfile.Name)
	if l, err := lockfile.Acquire(lockPath); err == nil {
		l.Release()
	} else if errors.Is(err, lockfile.ErrHeld) {
		in.LockHeld = true
		in.LockVerb = lockfile.HolderVerb(lockPath)
	}
	// The run records are read with the lock probe, as one snapshot of the
	// run (F14): reading them after the index open — which can wait seconds on
	// a busy index — let a run that failed in the meantime print "a run is
	// using this archive" beside its own failure.
	in.LastRun, in.LastRunState, in.LastRunErr = state.ReadLastRun(out)
	in.LastVerify, in.LastVerifyState, in.LastVerifyErr = state.ReadLastVerify(out)

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, serr := os.Stat(mpath); serr == nil {
		in.HasManifestFile = true
		if m, err := state.Load(mpath); err == nil {
			in.HasManifest = true
			in.Messages = m.Len()
			in.Fillable, in.Terminal, in.Unknown = m.Counts()
			in.ShownAsSource = m.FallbackCounts()
			in.WithFixity, _ = m.FixityCounts()
		} else {
			// The file exists but does not load (newer format, corrupt). Keep the
			// loader's error so Assess can RED it instead of saying "no archive".
			in.ManifestLoadErr = err
		}
	}
	in.ReportPath = filepath.Join(out, "attachments-report.tsv")
	if _, err := os.Stat(in.ReportPath); err == nil {
		in.ReportExists = true
	}
	// Tell an absent index from a present-but-unopenable one: keep the open
	// error so Assess can RED a damaged index instead of reporting none (F82).
	idxPath := filepath.Join(out, "search.db")
	if _, serr := os.Stat(idxPath); serr == nil {
		in.HasIndexFile = true
		if ix, err := index.OpenReadonly(idxPath); err == nil {
			in.HasIndex = true
			in.Indexed, _ = ix.Count()
			if oldest, newest, ok := ix.Range(); ok {
				in.HasRange, in.RangeOldest, in.RangeNewest = true, oldest, newest
			}
			ix.Close()
		} else {
			in.IndexErr, in.IndexBusy = err, index.Busy(err)
		}
	}
	in.History, in.HistoryReadErr = state.HistoryStatus(filepath.Join(out, state.HistoryName))

	if fi, err := os.Stat(out); err == nil && fi.IsDir() {
		in.WriteProbeErr = probeWrite(out)
	}

	if _, err := os.Lstat(filepath.Join(out, schedule.DescriptorName)); err == nil {
		in.HasDescriptorFile = true
	}
	d, derr := schedule.ReadDescriptor(out)
	in.DescErr = derr
	if derr == nil {
		in.HasDescriptor = true
		in.Desc = d
		in.DescNameUnbound = schedule.NameBound(out, d) != nil
		name := d.Name
		if nameOverride != "" {
			name = nameOverride
		}
		in.SchedState = schedule.Query(name)
		in.ExeExists, in.ExeIsThis = exeIdentity(d)
	} else if nameOverride != "" {
		in.SchedState = schedule.Query(nameOverride)
	}

	// The scheduled verify's own descriptor (FB-4): its cadence judges
	// verify_stale, and the host scheduler is asked about the name it records.
	if _, err := os.Lstat(filepath.Join(out, schedule.VerifyDescriptorName)); err == nil {
		in.HasVerifyDescriptorFile = true
	}
	vd, vderr := schedule.ReadVerifyDescriptor(out)
	in.VerifyDescErr = vderr
	if vderr == nil {
		in.HasVerifyDescriptor = true
		in.VerifyDesc = vd
		in.VerifyNameUnbound = schedule.NameBound(out, vd) != nil
		in.VerifySchedState = schedule.Query(vd.Name)
		in.VerifyExeExists, in.VerifyExeIsThis = exeIdentity(vd)
	}
	return in
}

// statProgram is the stat of a descriptor-supplied program path: a seam so a
// test can see which paths `status` touches (G8-AI4's encoder).
var statProgram = os.Stat

// exeIdentity reports whether a recorded schedule's program still exists, and
// whether it is this binary: the same file, or the size and modification time
// the install recorded for it.
func exeIdentity(d schedule.Descriptor) (exists, isThis bool) {
	fi, err := statProgram(d.Exe)
	if err != nil {
		return false, false
	}
	self, _ := os.Executable()
	return true, sameFile(self, d.Exe) || (d.ExeSize == fi.Size() && !d.ExeMTime.IsZero() && d.ExeMTime.Equal(fi.ModTime().UTC()))
}

func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// Summary renders the facts and the judgement as the lines `status` prints.
func Summary(in Input, rep Report) []string {
	var lines []string
	lines = append(lines, fmt.Sprintf("Archive:    %s", in.Out))
	if in.HasManifest {
		idx := "no search index"
		switch {
		case in.HasIndex:
			idx = fmt.Sprintf("%d indexed", in.Indexed)
		case in.HasIndexFile && in.IndexBusy:
			idx = "search index busy (see the posture below)"
		case in.HasIndexFile:
			idx = "search index unreadable (see the posture below)"
		}
		lines = append(lines, fmt.Sprintf("Messages:   %d in manifest · %s", in.Messages, idx))
		if in.HasRange {
			lines = append(lines, fmt.Sprintf("Archived range: %s – %s (UTC)", in.RangeOldest.Format("2006-01-02"), in.RangeNewest.Format("2006-01-02")))
		}
		if in.Fillable > 0 || in.Terminal > 0 || in.Unknown > 0 {
			line := fmt.Sprintf("Incomplete: %d still missing content · %d source-empty (never fillable) · %d not yet re-examined", in.Fillable, in.Terminal, in.Unknown)
			if in.ReportExists {
				line += " — " + in.ReportPath
			}
			lines = append(lines, line)
			lines = append(lines, "            (still missing content = not downloaded yet; fills on the next run)")
		} else {
			lines = append(lines, "Incomplete: none")
		}
		// Fixity COVERAGE: how many records carry file digests, from manifest
		// fields alone — this hashes nothing (that is `verify`), so it measures
		// coverage, not integrity. The line says so and points at `verify` to
		// check the bytes; when some records lack a digest it also names the
		// baseline remedy (FC12, friction #4).
		if in.Messages > 0 {
			fx := fmt.Sprintf("Fixity coverage: %d of %d records recorded (run `mailarchive verify -out %q` to check the bytes)", in.WithFixity, in.Messages, in.Out)
			if in.WithFixity < in.Messages {
				fx += fmt.Sprintf(" · run `mailarchive verify -record -out %q` to baseline the rest", in.Out)
			}
			lines = append(lines, fx)
		}
		// Extractability (design K4): how many records have a preserved original
		// (.eml) present on disk — the same FILE-PRESENCE signal `extract` drains
		// and `verify` counts (never a recorded digest, so it agrees with them).
		// Informational like fixity coverage: it never changes the posture and
		// never prints a categorical "re-archive" verdict — for the records with
		// no original it names both remedies (re-run with -raw for a raw-capable
		// source; keep the .pst itself for Outlook, which never carries one).
		// Omitted when the manifest has no records.
		if in.Extractable.Total > 0 {
			lines = append(lines, fmt.Sprintf("Extractable: %d of %d records have a preserved original (.eml) on disk", in.Extractable.WithEML, in.Extractable.Total))
			if in.Extractable.WithEML < in.Extractable.Total {
				lines = append(lines, fmt.Sprintf("            the remaining %d have no preserved original — an mbox/maildir/Microsoft 365 source keeps one only when archived with -raw; Outlook .pst/.ost items never carry one, so keep the .pst itself to migrate", in.Extractable.Total-in.Extractable.WithEML))
			}
		}
	} else if in.HasManifestFile {
		lines = append(lines, "Messages:   (manifest present but unreadable — see the posture below)")
	} else {
		lines = append(lines, "Messages:   (no manifest yet — no run has completed here)")
	}
	// Go-back timeline coverage (design §3.4): the observed run cadence and
	// whether a past-date view is fully available. Absent is normal for a
	// one-shot local import.
	switch {
	case !in.History.Exists:
		lines = append(lines, "History:    none recorded — no go-back timeline (a live capture writes it; a one-shot local import has none)")
	case in.HistoryReadErr != nil:
		lines = append(lines, "History:    present but unreadable — see the posture below")
	case in.History.Clean():
		lines = append(lines, fmt.Sprintf("History:    %d run(s), %d event(s) recorded — go-back available", in.History.Runs, in.History.Events))
	case in.History.TornTail && in.History.BadLines <= 1:
		lines = append(lines, fmt.Sprintf("History:    %d run(s), %d event(s) — torn tail, go-back partial (repairs on the next run)", in.History.Runs, in.History.Events))
	default:
		lines = append(lines, fmt.Sprintf("History:    %d run(s), %d event(s) — %d unreadable line(s), go-back partial", in.History.Runs, in.History.Events, in.History.BadLines))
	}
	switch in.LastRunState {
	case state.LastRunPresent:
		lr := in.LastRun
		// A "running" record reads as running only while it can belong to a
		// run in progress (runRunning, the rule Assess judges it by); otherwise
		// the run that wrote it never finished, as the RED below says.
		how := lr.Status
		if lr.Status == state.RunRunning && !runRunning(in) {
			how = "never finished"
		}
		// Local time, with its zone named: the verify lines below are UTC (F9).
		line := fmt.Sprintf("Last run:   %s → %s", lr.Started.Local().Format("2006-01-02 15:04 MST"), how)
		if lr.Status == state.RunOK {
			line += fmt.Sprintf(" · exported %d · filled %d · took %s", lr.Exported, lr.Filled, lr.Finished.Sub(lr.Started).Round(time.Second))
		} else if lr.Error != "" {
			line += ": " + runErrorText(in, lr)
		}
		lines = append(lines, line)
	case state.LastRunAbsent:
		lines = append(lines, "Last run:   none recorded")
	case state.LastRunUnreadable:
		lines = append(lines, "Last run:   record unreadable")
	}
	if in.LastVerifyState == state.LastVerifyPresent {
		lv := in.LastVerify
		if lv.Status == state.VerifyRunning || lv.Finished == nil {
			how := "running"
			if !verifyRunning(in) {
				how = "never finished"
			}
			lines = append(lines, fmt.Sprintf("Last verify: %s → %s", lv.Started.UTC().Format("2006-01-02 15:04"), how))
			// The finished verdict a running record carries is still the
			// archive's latest integrity evidence (F79).
			if p := lv.Previous; p != nil && p.Finished != nil {
				lines = append(lines, "Previous verify: "+verifyVerdict(*p))
			}
		} else {
			lines = append(lines, "Last verify: "+verifyVerdict(lv))
		}
		lines = append(lines, "            (verify times are UTC)")
	}
	switch {
	case in.HasDescriptor:
		lines = append(lines, fmt.Sprintf("Schedule:   %q %s · %s at %s · runs %s · installed %s (UTC) on %s",
			in.Desc.Name, in.SchedState, in.Desc.Interval, in.Desc.At, in.Desc.Exe, in.Desc.InstalledAt.UTC().Format("2006-01-02"), in.Desc.Host))
	case in.HasDescriptorFile:
		lines = append(lines, "Schedule:   (descriptor present but unreadable — see the posture below)")
	default:
		lines = append(lines, "Schedule:   none recorded")
	}
	// The scheduled verify, from its own descriptor (FB-4); no line when none is
	// recorded (a verify installed by an earlier release records none).
	switch {
	case in.HasVerifyDescriptor:
		vd := in.VerifyDesc
		lines = append(lines, fmt.Sprintf("Verify schedule: %q %s · %s at %s · runs %s · installed %s (UTC) on %s",
			vd.Name, in.VerifySchedState, vd.Interval, vd.At, vd.Exe, vd.InstalledAt.UTC().Format("2006-01-02"), vd.Host))
	case in.HasVerifyDescriptorFile:
		lines = append(lines, "Verify schedule: (descriptor present but unreadable — see the posture below)")
	}
	lines = append(lines, fmt.Sprintf("Posture:    %s", rep.Posture))
	for _, reason := range rep.Reasons {
		lines = append(lines, "  "+reason)
	}
	return lines
}

// verifyVerdict renders a finished verify record as "<when> → attested" or
// "<when> → NOT attested (…)" for Summary.
func verifyVerdict(lv state.LastVerify) string {
	when := lv.Finished.UTC().Format("2006-01-02 15:04")
	if lv.Attested {
		return fmt.Sprintf("%s → attested", when)
	}
	return fmt.Sprintf("%s → NOT attested (modified %d, missing %d, unrecorded %d)", when, lv.Modified, lv.Missing, lv.Unrecorded)
}

// JSONReport is the typed, versioned document `status -json` prints (product
// nas-01): the same facts and judgement as Summary, machine-readable. Version
// is bumped when a field's meaning changes so a consumer can refuse a shape it
// does not understand.
type JSONReport struct {
	Version     int      `json:"version"`
	Posture     string   `json:"posture"`
	Reasons     []string `json:"reasons"`
	ReasonCodes []string `json:"reason_codes"` // parallel to Reasons; a stable code per WARN/RED
	Out         string   `json:"out"`
	Messages    int      `json:"messages"`
	Indexed     int      `json:"indexed"`
	// IndexedKnown is false when search.db exists but could not be read (busy
	// or damaged), so Indexed's 0 is not a count (F8). Additive; version stays.
	IndexedKnown bool             `json:"indexed_known"`
	Fillable     int              `json:"fillable"`
	Terminal     int              `json:"terminal"`
	Unknown      int              `json:"unknown"`
	Fixity       *JSONFixity      `json:"fixity"`      // fixity COVERAGE; null when no manifest
	Extractable  *JSONExtractable `json:"extractable"` // records with a preserved .eml present on disk (file presence, design K4); null when no manifest
	History      *JSONHistory     `json:"history"`     // go-back timeline coverage (design §3.4); always present (exists=false when no log)
	LastRun      *JSONLastRun     `json:"last_run,omitempty"`
	LastVerify   *JSONLastVerify  `json:"last_verify"` // verify's integrity verdict; null when none recorded
	Schedule     *JSONSchedule    `json:"schedule,omitempty"`
	// VerifySchedule is the scheduled verify from its own descriptor (FB-4), in
	// the schedule facet's shape; omitted when none is recorded. A later,
	// backward-compatible addition at version 2, like `extractable`.
	VerifySchedule *JSONSchedule `json:"verify_schedule,omitempty"`
}

// JSONFixity is the fixity-coverage facet: how many records carry a digest
// (measured from the manifest; nothing is hashed — that is `verify`).
type JSONFixity struct {
	Records    int `json:"records"`
	WithFixity int `json:"with_fixity"`
}

// JSONExtractable is the extractability facet: how many records have a preserved
// original (.eml) present on disk (records_with_eml) of the total (records) — the
// file-presence signal `extract` drains and `verify` counts (design K4), never a
// recorded digest, so it agrees with what `extract` and `verify` see.
type JSONExtractable struct {
	Records        int `json:"records"`
	RecordsWithEML int `json:"records_with_eml"`
}

// JSONHistory is the go-back timeline's coverage (design §3.4): whether the log
// exists, its run and event counts, and the two damage signals (unreadable
// lines, a torn tail). A consumer can read `exists && !torn_tail && bad_lines==0`
// as "go-back fully available".
type JSONHistory struct {
	Exists   bool `json:"exists"`
	Runs     int  `json:"runs"`
	Events   int  `json:"events"`
	BadLines int  `json:"bad_lines"`
	TornTail bool `json:"torn_tail"`
}

// JSONLastRun is the last-run facet of JSONReport (omitted when no readable
// record exists). Finished is null while a run is in progress, so a consumer
// can tell "not finished yet" from a real completion time (UJ-A5).
type JSONLastRun struct {
	Status   string     `json:"status"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished"`
	Error    string     `json:"error,omitempty"`
	Exported int        `json:"exported"`
	Filled   int        `json:"filled"`
	// InProgress is true only while a run really holds the archive lock: a
	// "running" status whose run is dead (RED run_never_finished) has
	// in_progress false (F8). Additive; version stays.
	InProgress bool `json:"in_progress"`
}

// JSONLastVerify is verify's integrity verdict, from its separate record; null
// when no verify has run. Finished is null while a verify is in progress.
type JSONLastVerify struct {
	Status     string     `json:"status"`
	Started    time.Time  `json:"started"`
	Finished   *time.Time `json:"finished"`
	Attested   bool       `json:"attested"`
	Records    int        `json:"records"`
	WithFixity int        `json:"with_fixity"`
	Checked    int        `json:"checked"`
	OK         int        `json:"ok"`
	Modified   int        `json:"modified"`
	Missing    int        `json:"missing"`
	Unrecorded int        `json:"unrecorded"`
	Unexpected int        `json:"unexpected"`
	Recorded   int        `json:"recorded"`
	ExitCode   int        `json:"exit_code"`
	// Previous is the last finished verdict a running record carries (F79): a
	// new optional key, omitted otherwise, so JSONVersion is unchanged.
	Previous *JSONLastVerify `json:"previous,omitempty"`
}

// jsonLastVerify maps a verify record (and the finished verdict a running one
// carries) to its JSON facet.
func jsonLastVerify(lv state.LastVerify) *JSONLastVerify {
	j := &JSONLastVerify{
		Status: lv.Status, Started: lv.Started, Finished: lv.Finished,
		Attested: lv.Attested, Records: lv.Records, WithFixity: lv.WithFixity,
		Checked: lv.Checked, OK: lv.OK, Modified: lv.Modified, Missing: lv.Missing,
		Unrecorded: lv.Unrecorded, Unexpected: lv.Unexpected, Recorded: lv.Recorded,
		ExitCode: lv.ExitCode,
	}
	if lv.Previous != nil {
		p := *lv.Previous
		p.Previous = nil
		j.Previous = jsonLastVerify(p)
	}
	return j
}

// JSONSchedule is the schedule facet of JSONReport (omitted when no descriptor
// is recorded).
type JSONSchedule struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Interval string `json:"interval"`
	At       string `json:"at"`
	Exe      string `json:"exe"`
	Host     string `json:"host"`
}

// JSONVersion is the current JSONReport schema version. Bumped to 2 with the
// addition of fixity, last_verify and reason_codes and the change of
// last_run.finished to null-while-running. The `extractable`, `history` and
// `verify_schedule` objects are later, backward-compatible additions at version 2 — a new optional
// key changes no existing field's meaning, so a version-2 consumer that ignores
// unknown keys is unaffected and the version is not bumped.
const JSONVersion = 2

// JSON builds the machine-readable status document from the gathered facts and
// the posture judgement. It never fails and never changes the exit code: like
// Summary it only reports (R18).
func JSON(in Input, rep Report) JSONReport {
	doc := JSONReport{
		Version:      JSONVersion,
		Posture:      rep.Posture,
		Reasons:      rep.Reasons,
		ReasonCodes:  rep.Codes,
		Out:          in.Out,
		Messages:     in.Messages,
		Indexed:      in.Indexed,
		IndexedKnown: !(in.HasIndexFile && !in.HasIndex),
		Fillable:     in.Fillable,
		Terminal:     in.Terminal,
		Unknown:      in.Unknown,
	}
	if doc.Reasons == nil {
		doc.Reasons = []string{}
	}
	if doc.ReasonCodes == nil {
		doc.ReasonCodes = []string{}
	}
	// Go-back timeline coverage (design §3.4): a new optional key, always
	// present, that changes no existing field's meaning — so, like `extractable`,
	// it is a backward-compatible addition and does not bump JSONVersion.
	doc.History = &JSONHistory{
		Exists:   in.History.Exists,
		Runs:     in.History.Runs,
		Events:   in.History.Events,
		BadLines: in.History.BadLines,
		TornTail: in.History.TornTail,
	}
	if in.HasManifest {
		doc.Fixity = &JSONFixity{Records: in.Messages, WithFixity: in.WithFixity}
		// Extractability (design K4): the file-presence count `extract`/`verify`
		// share, carried alongside fixity coverage. null when no manifest.
		doc.Extractable = &JSONExtractable{Records: in.Extractable.Total, RecordsWithEML: in.Extractable.WithEML}
	}
	if in.LastRunState == state.LastRunPresent {
		lr := in.LastRun
		jlr := &JSONLastRun{
			Status:     lr.Status,
			Started:    lr.Started,
			Error:      lr.Error,
			Exported:   lr.Exported,
			Filled:     lr.Filled,
			InProgress: lr.Status == state.RunRunning && runRunning(in),
		}
		// Finished stays null while a run is in progress (UJ-A5).
		if lr.Status != state.RunRunning && !lr.Finished.IsZero() {
			f := lr.Finished
			jlr.Finished = &f
		}
		doc.LastRun = jlr
	}
	if in.LastVerifyState == state.LastVerifyPresent {
		doc.LastVerify = jsonLastVerify(in.LastVerify)
	}
	if in.HasDescriptor {
		doc.Schedule = &JSONSchedule{
			Name:     in.Desc.Name,
			State:    in.SchedState.String(),
			Interval: in.Desc.Interval,
			At:       in.Desc.At,
			Exe:      in.Desc.Exe,
			Host:     in.Desc.Host,
		}
	}
	if in.HasVerifyDescriptor {
		doc.VerifySchedule = &JSONSchedule{
			Name:     in.VerifyDesc.Name,
			State:    in.VerifySchedState.String(),
			Interval: in.VerifyDesc.Interval,
			At:       in.VerifyDesc.At,
			Exe:      in.VerifyDesc.Exe,
			Host:     in.VerifyDesc.Host,
		}
	}
	return doc
}
