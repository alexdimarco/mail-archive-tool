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
	"strings"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/procs"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// Input is everything known about an archive before judging it.
type Input struct {
	Out string

	HasManifest                 bool
	Messages                    int
	Fillable, Terminal, Unknown int
	ReportPath                  string

	HasIndex bool
	Indexed  int

	LastRun      state.LastRun
	LastRunState state.LastRunState
	LastRunErr   error

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

// Report is the judgement: a posture and the reasons behind any WARN/RED.
type Report struct {
	Posture string // GREEN | WARN | RED
	Reasons []string
}

func (r *Report) warn(msg string) {
	if r.Posture != "RED" {
		r.Posture = "WARN"
	}
	r.Reasons = append(r.Reasons, "WARN: "+msg)
}

func (r *Report) red(msg string) {
	r.Posture = "RED"
	r.Reasons = append(r.Reasons, "RED: "+msg)
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

	// Completeness.
	if in.Fillable > 0 {
		r.warn(fmt.Sprintf("%d message(s) still missing content — download for offline use in your mail app, then re-run; incremental fills them (see %s)", in.Fillable, in.ReportPath))
	}
	if in.Unknown > 0 {
		r.warn(fmt.Sprintf("%d entr%s predate completeness tracking and are re-examined by incremental runs until none remain", in.Unknown, plural(in.Unknown, "y", "ies")))
	}
	if in.HasIndex && in.HasManifest && in.Indexed < in.Messages {
		r.warn(fmt.Sprintf("the search index holds %d of %d messages — run `mailarchive reindex -out %q`", in.Indexed, in.Messages, in.Out))
	}

	// Last run.
	period := 24 * time.Hour
	if in.HasDescriptor {
		period = IntervalOf(in.Desc.Interval)
	}
	switch in.LastRunState {
	case state.LastRunUnreadable:
		r.warn(fmt.Sprintf("the last-run record %s is unreadable (%v) — the next run rewrites it", filepath.Join(in.Out, state.LastRunName), in.LastRunErr))
	case state.LastRunAbsent:
		if in.HasDescriptor {
			r.warn("a schedule is installed but no run has ever been recorded here — wait for the first run, or run the job by hand to check it works")
		}
	case state.LastRunPresent:
		lr := in.LastRun
		switch lr.Status {
		case state.RunFailed:
			r.red(fmt.Sprintf("the last run (%s) FAILED: %s", lr.Started.Local().Format("2006-01-02 15:04"), lr.Error))
			if looksLikeAuthFailure(lr.Error) {
				r.Reasons = append(r.Reasons, "  remedy: authentication failed — the app client secret may have expired; rotate it in Entra and rewrite the secret file")
			}
		case state.RunRunning:
			alive := in.LockHeld
			switch {
			case alive && now.Sub(lr.Started) <= period:
				r.Reasons = append(r.Reasons, fmt.Sprintf("a run is in progress (pid %d, started %s)", lr.PID, lr.Started.Local().Format("2006-01-02 15:04")))
			case alive:
				r.red(fmt.Sprintf("a run started %s is still running (pid %d) — longer than the schedule period; check the log", lr.Started.Local().Format("2006-01-02 15:04"), lr.PID))
			default:
				r.red(fmt.Sprintf("the run started %s never finished (pid %d is gone: crash, kill, or power loss) — check the log and run again", lr.Started.Local().Format("2006-01-02 15:04"), lr.PID))
			}
		case state.RunCancelled:
			r.warn(fmt.Sprintf("the last run (%s) was cancelled before finishing; progress was kept", lr.Started.Local().Format("2006-01-02 15:04")))
		}
		if in.HasDescriptor && lr.Status != state.RunRunning && now.Sub(lr.Started) > 2*period {
			r.warn(fmt.Sprintf("the last run was %s ago, more than twice the %s schedule period — is the machine on and logged in at %s?", HumanAge(now.Sub(lr.Started)), in.Desc.Interval, in.Desc.At))
		}
	}

	// Schedule.
	switch {
	case in.DescErr != nil && !errors.Is(in.DescErr, os.ErrNotExist):
		r.warn(in.DescErr.Error())
	case !in.HasDescriptor:
		r.warn(fmt.Sprintf("no schedule is recorded for this archive — keep it current with `mailarchive schedule -out %q -auto -install` (or the GUI's \"Keep this archive current\")", in.Out))
	default:
		if in.ThisHost != "" && in.Desc.Host != "" && in.ThisHost != in.Desc.Host {
			r.warn(fmt.Sprintf("the schedule was installed on host %q, not this one (%q); its state cannot be checked from here", in.Desc.Host, in.ThisHost))
		} else {
			switch in.SchedState {
			case schedule.SchedulerUnavailable:
				r.warn("this host's scheduler cannot be queried (no crontab/schtasks?) — the schedule may not run here")
			case schedule.NotInstalled:
				r.warn(fmt.Sprintf("the schedule %q is recorded but not present in this host's scheduler — re-run `mailarchive schedule ... -install`", in.Desc.Name))
			}
			if !in.ExeExists {
				r.red(fmt.Sprintf("the scheduled program %s no longer exists — the job cannot run; re-run `mailarchive schedule ... -install` from the current binary", in.Desc.Exe))
			} else if !in.ExeIsThis {
				r.warn(fmt.Sprintf("the schedule runs %s, which is not this binary — after an upgrade or move, re-run `mailarchive schedule ... -install`", in.Desc.Exe))
			}
		}
	}
	return r
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

// Gather collects the facts about an archive. nameOverride checks that
// schedule name instead of the descriptor's.
func Gather(out, nameOverride string) Input {
	in := Input{Out: out, PIDAlive: procs.Alive}
	in.ThisHost, _ = os.Hostname()
	// Probe the archive lock: held means a run is genuinely in progress.
	if l, err := lockfile.Acquire(filepath.Join(out, lockfile.Name)); err == nil {
		l.Release()
	} else if errors.Is(err, lockfile.ErrHeld) {
		in.LockHeld = true
	}

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if _, serr := os.Stat(mpath); serr == nil {
		if m, err := state.Load(mpath); err == nil {
			in.HasManifest = true
			in.Messages = m.Len()
			in.Fillable, in.Terminal, in.Unknown = m.Counts()
		}
	}
	in.ReportPath = filepath.Join(out, "attachments-report.tsv")
	if ix, err := index.OpenReadonly(filepath.Join(out, "search.db")); err == nil {
		in.HasIndex = true
		in.Indexed, _ = ix.Count()
		ix.Close()
	}
	in.LastRun, in.LastRunState, in.LastRunErr = state.ReadLastRun(out)

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
		if in.Fillable > 0 || in.Terminal > 0 || in.Unknown > 0 {
			lines = append(lines, fmt.Sprintf("Incomplete: %d still fillable · %d source-empty (terminal) · %d not yet re-examined — %s", in.Fillable, in.Terminal, in.Unknown, in.ReportPath))
		} else {
			lines = append(lines, "Incomplete: none")
		}
	} else {
		lines = append(lines, "Messages:   (no manifest yet — no run has completed here)")
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
	if in.HasDescriptor {
		lines = append(lines, fmt.Sprintf("Schedule:   %q %s · %s at %s · runs %s · installed %s on %s",
			in.Desc.Name, in.SchedState, in.Desc.Interval, in.Desc.At, in.Desc.Exe, in.Desc.InstalledAt.Local().Format("2006-01-02"), in.Desc.Host))
	} else {
		lines = append(lines, "Schedule:   none recorded")
	}
	lines = append(lines, fmt.Sprintf("Posture:    %s", rep.Posture))
	for _, reason := range rep.Reasons {
		lines = append(lines, "  "+reason)
	}
	return lines
}
