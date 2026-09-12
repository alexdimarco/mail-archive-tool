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
	WithFixity                  int // records carrying at least one file digest (N of Messages)
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

	HasIndex bool
	Indexed  int

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

	HasDescriptor bool
	Desc          schedule.Descriptor
	DescErr       error
	SchedState    schedule.State
	ExeExists     bool
	ExeIsThis     bool
	ThisHost      string

	// LockHeld is the authoritative liveness signal: the archive lock dies
	// with its process, so a "running" record whose lock is free belongs to a
	// run that never finished (pid numbers are reused after a reboot).
	LockHeld bool

	PIDAlive func(int) bool
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
	if in.HasIndex && in.HasManifest && in.Indexed < in.Messages {
		r.warn("index_behind", fmt.Sprintf("the search index holds %d of %d messages — run `mailarchive reindex -out %q`", in.Indexed, in.Messages, in.Out))
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
			r.red("run_failed", fmt.Sprintf("the last run (%s) FAILED: %s", lr.Started.Local().Format("2006-01-02 15:04"), lr.Error))
			if looksLikeAuthFailure(lr.Error) {
				r.note("auth_remedy", "  remedy: authentication failed — the app client secret may have expired; rotate it in Entra and rewrite the secret file")
			}
		case state.RunRunning:
			alive := in.LockHeld
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
	}

	// Last verify: the archive's integrity verdict, from verify's own record.
	// modified/missing is real corruption (RED); unrecorded-only is a coverage
	// gap (WARN); a verify older than twice its own schedule cadence is stale
	// (WARN) — only when the recorded schedule IS a verify schedule, so its
	// cadence is known (UJ-A1).
	if in.LastVerifyState == state.LastVerifyPresent && in.LastVerify.Status == state.VerifyDone {
		lv := in.LastVerify
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
		if isVerifyJob(in.Desc.Job) && lv.Finished != nil && now.Sub(*lv.Finished) > 2*IntervalOf(in.Desc.Interval) {
			r.warn("verify_stale", fmt.Sprintf("the last verify was %s ago, more than twice the %s verify schedule period — schedule it, or run `mailarchive verify -out %q`", HumanAge(now.Sub(*lv.Finished)), in.Desc.Interval, in.Out))
		}
	}

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
			r.warn("moved_archive", fmt.Sprintf("the installed schedule %q backs up %s, not this archive (%s) — re-run %s, then remove the stale one with `mailarchive schedule -name %q -remove`", in.Desc.Name, jo, in.Out, reinstall, in.Desc.Name))
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
	return r
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
	for _, needle := range []string{"401", "unauthorized", "invalid_client", "aadsts", "authentication", "invalid client secret"} {
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
	if !needsShellQuoting(s) {
		return s
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
	// Probe the archive lock: held means a run is genuinely in progress.
	if l, err := lockfile.Acquire(filepath.Join(out, lockfile.Name)); err == nil {
		l.Release()
	} else if errors.Is(err, lockfile.ErrHeld) {
		in.LockHeld = true
	}

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, serr := os.Stat(mpath); serr == nil {
		in.HasManifestFile = true
		if m, err := state.Load(mpath); err == nil {
			in.HasManifest = true
			in.Messages = m.Len()
			in.Fillable, in.Terminal, in.Unknown = m.Counts()
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
	if ix, err := index.OpenReadonly(filepath.Join(out, "search.db")); err == nil {
		in.HasIndex = true
		in.Indexed, _ = ix.Count()
		if oldest, newest, ok := ix.Range(); ok {
			in.HasRange, in.RangeOldest, in.RangeNewest = true, oldest, newest
		}
		ix.Close()
	}
	in.History, in.HistoryReadErr = state.HistoryStatus(filepath.Join(out, state.HistoryName))
	in.LastRun, in.LastRunState, in.LastRunErr = state.ReadLastRun(out)
	in.LastVerify, in.LastVerifyState, in.LastVerifyErr = state.ReadLastVerify(out)

	d, derr := schedule.ReadDescriptor(out)
	in.DescErr = derr
	if derr == nil {
		in.HasDescriptor = true
		in.Desc = d
		name := d.Name
		if nameOverride != "" {
			name = nameOverride
		}
		in.SchedState = schedule.Query(name)
		if fi, err := os.Stat(d.Exe); err == nil {
			in.ExeExists = true
			self, _ := os.Executable()
			in.ExeIsThis = sameFile(self, d.Exe) || (d.ExeSize == fi.Size() && !d.ExeMTime.IsZero() && d.ExeMTime.Equal(fi.ModTime().UTC()))
		}
	} else if nameOverride != "" {
		in.SchedState = schedule.Query(nameOverride)
	}
	return in
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
		if in.HasIndex {
			idx = fmt.Sprintf("%d indexed", in.Indexed)
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
		line := fmt.Sprintf("Last run:   %s → %s", lr.Started.Local().Format("2006-01-02 15:04"), lr.Status)
		if lr.Status == state.RunOK {
			line += fmt.Sprintf(" · exported %d · filled %d · took %s", lr.Exported, lr.Filled, lr.Finished.Sub(lr.Started).Round(time.Second))
		} else if lr.Error != "" {
			line += ": " + lr.Error
		}
		lines = append(lines, line)
	case state.LastRunAbsent:
		lines = append(lines, "Last run:   none recorded")
	case state.LastRunUnreadable:
		lines = append(lines, "Last run:   record unreadable")
	}
	if in.LastVerifyState == state.LastVerifyPresent {
		lv := in.LastVerify
		switch {
		case lv.Status == state.VerifyRunning || lv.Finished == nil:
			lines = append(lines, fmt.Sprintf("Last verify: %s → running", lv.Started.UTC().Format("2006-01-02 15:04")))
		case lv.Attested:
			lines = append(lines, fmt.Sprintf("Last verify: %s → attested", lv.Finished.UTC().Format("2006-01-02 15:04")))
		default:
			lines = append(lines, fmt.Sprintf("Last verify: %s → NOT attested (modified %d, missing %d, unrecorded %d)",
				lv.Finished.UTC().Format("2006-01-02 15:04"), lv.Modified, lv.Missing, lv.Unrecorded))
		}
	}
	if in.HasDescriptor {
		lines = append(lines, fmt.Sprintf("Schedule:   %q %s · %s at %s · runs %s · installed %s (UTC) on %s",
			in.Desc.Name, in.SchedState, in.Desc.Interval, in.Desc.At, in.Desc.Exe, in.Desc.InstalledAt.UTC().Format("2006-01-02"), in.Desc.Host))
	} else {
		lines = append(lines, "Schedule:   none recorded")
	}
	lines = append(lines, fmt.Sprintf("Posture:    %s", rep.Posture))
	for _, reason := range rep.Reasons {
		lines = append(lines, "  "+reason)
	}
	return lines
}

// JSONReport is the typed, versioned document `status -json` prints (product
// nas-01): the same facts and judgement as Summary, machine-readable. Version
// is bumped when a field's meaning changes so a consumer can refuse a shape it
// does not understand.
type JSONReport struct {
	Version     int              `json:"version"`
	Posture     string           `json:"posture"`
	Reasons     []string         `json:"reasons"`
	ReasonCodes []string         `json:"reason_codes"` // parallel to Reasons; a stable code per WARN/RED
	Out         string           `json:"out"`
	Messages    int              `json:"messages"`
	Indexed     int              `json:"indexed"`
	Fillable    int              `json:"fillable"`
	Terminal    int              `json:"terminal"`
	Unknown     int              `json:"unknown"`
	Fixity      *JSONFixity      `json:"fixity"`      // fixity COVERAGE; null when no manifest
	Extractable *JSONExtractable `json:"extractable"` // records with a preserved .eml present on disk (file presence, design K4); null when no manifest
	History     *JSONHistory     `json:"history"`     // go-back timeline coverage (design §3.4); always present (exists=false when no log)
	LastRun     *JSONLastRun     `json:"last_run,omitempty"`
	LastVerify  *JSONLastVerify  `json:"last_verify"` // verify's integrity verdict; null when none recorded
	Schedule    *JSONSchedule    `json:"schedule,omitempty"`
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
// last_run.finished to null-while-running. The `extractable` and `history`
// objects are later, backward-compatible additions at version 2 — a new optional
// key changes no existing field's meaning, so a version-2 consumer that ignores
// unknown keys is unaffected and the version is not bumped.
const JSONVersion = 2

// JSON builds the machine-readable status document from the gathered facts and
// the posture judgement. It never fails and never changes the exit code: like
// Summary it only reports (R18).
func JSON(in Input, rep Report) JSONReport {
	doc := JSONReport{
		Version:     JSONVersion,
		Posture:     rep.Posture,
		Reasons:     rep.Reasons,
		ReasonCodes: rep.Codes,
		Out:         in.Out,
		Messages:    in.Messages,
		Indexed:     in.Indexed,
		Fillable:    in.Fillable,
		Terminal:    in.Terminal,
		Unknown:     in.Unknown,
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
			Status:   lr.Status,
			Started:  lr.Started,
			Error:    lr.Error,
			Exported: lr.Exported,
			Filled:   lr.Filled,
		}
		// Finished stays null while a run is in progress (UJ-A5).
		if lr.Status != state.RunRunning && !lr.Finished.IsZero() {
			f := lr.Finished
			jlr.Finished = &f
		}
		doc.LastRun = jlr
	}
	if in.LastVerifyState == state.LastVerifyPresent {
		lv := in.LastVerify
		doc.LastVerify = &JSONLastVerify{
			Status: lv.Status, Started: lv.Started, Finished: lv.Finished,
			Attested: lv.Attested, Records: lv.Records, WithFixity: lv.WithFixity,
			Checked: lv.Checked, OK: lv.OK, Modified: lv.Modified, Missing: lv.Missing,
			Unrecorded: lv.Unrecorded, Unexpected: lv.Unexpected, Recorded: lv.Recorded,
			ExitCode: lv.ExitCode,
		}
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
	return doc
}
