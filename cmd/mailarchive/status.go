package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/procs"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// statusInput is everything `status` learns about an archive before judging
// it; gathered by gatherStatus, judged by assess (pure, so tests can drive it).
type statusInput struct {
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

	PIDAlive func(int) bool
}

// statusReport is the judged result: a posture and the reasons behind any
// WARN/RED, each naming its remedy.
type statusReport struct {
	Posture string // GREEN | WARN | RED
	Reasons []string
}

func (r *statusReport) warn(msg string) {
	if r.Posture != "RED" {
		r.Posture = "WARN"
	}
	r.Reasons = append(r.Reasons, "WARN: "+msg)
}

func (r *statusReport) red(msg string) {
	r.Posture = "RED"
	r.Reasons = append(r.Reasons, "RED: "+msg)
}

// intervalOf maps a cadence to its period.
func intervalOf(iv string) time.Duration {
	switch schedule.Interval(iv) {
	case schedule.Hourly:
		return time.Hour
	case schedule.Weekly:
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// assess applies the posture rules (design-schedule-v2 §3.4, fail-closed on
// missing evidence).
func assess(in statusInput, now time.Time) statusReport {
	r := statusReport{Posture: "GREEN"}

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
		period = intervalOf(in.Desc.Interval)
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
			alive := in.PIDAlive != nil && in.PIDAlive(lr.PID)
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
			r.warn(fmt.Sprintf("the last run was %s ago, more than twice the %s schedule period — is the machine on and logged in at %s?", humanAge(now.Sub(lr.Started)), in.Desc.Interval, in.Desc.At))
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

func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.0f h", d.Hours())
	default:
		return fmt.Sprintf("%.0f days", d.Hours()/24)
	}
}

// gatherStatus collects the facts about an archive.
func gatherStatus(out, nameOverride string) statusInput {
	in := statusInput{Out: out, PIDAlive: procs.Alive}
	in.ThisHost, _ = os.Hostname()

	if m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json")); err == nil {
		if _, serr := os.Stat(filepath.Join(out, ".mailarchive-manifest.json")); serr == nil {
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
		// No descriptor but an explicit name: still ask the scheduler.
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

// runStatus is the legibility surface (X6): the archive's completeness, last
// run and schedule, judged GREEN/WARN/RED with every WARN/RED naming its
// remedy. stdout carries the answer (X8) and the exit is 0 whenever it
// reports; a directory with neither manifest nor descriptor is refused.
func runStatus(args []string) error {
	fs := flag.NewFlagSet("mailarchive status", flag.ContinueOnError)
	fs.Usage = statusUsage(fs)
	out := fs.String("out", "", "archive directory (required)")
	name := fs.String("name", "", "schedule name to check (default: the archive's recorded schedule)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the archive directory to report on)")
	}
	in := gatherStatus(abspath(*out), *name)
	if !in.HasManifest && !in.HasDescriptor {
		return fmt.Errorf("no archive at %s: no manifest and no schedule descriptor (run an export into it first, or check the path)", in.Out)
	}
	rep := assess(in, time.Now())

	fmt.Printf("Archive:    %s\n", in.Out)
	if in.HasManifest {
		idx := "no search index"
		if in.HasIndex {
			idx = fmt.Sprintf("%d indexed", in.Indexed)
		}
		fmt.Printf("Messages:   %d in manifest · %s\n", in.Messages, idx)
		if in.Fillable > 0 || in.Terminal > 0 || in.Unknown > 0 {
			fmt.Printf("Incomplete: %d still fillable · %d source-empty (terminal) · %d not yet re-examined — %s\n", in.Fillable, in.Terminal, in.Unknown, in.ReportPath)
		} else {
			fmt.Printf("Incomplete: none\n")
		}
	} else {
		fmt.Printf("Messages:   (no manifest yet — no run has completed here)\n")
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
		fmt.Println(line)
	case state.LastRunAbsent:
		fmt.Println("Last run:   none recorded")
	case state.LastRunUnreadable:
		fmt.Println("Last run:   record unreadable")
	}
	if in.HasDescriptor {
		fmt.Printf("Schedule:   %q %s · %s at %s · runs %s · installed %s on %s\n",
			in.Desc.Name, in.SchedState, in.Desc.Interval, in.Desc.At, in.Desc.Exe, in.Desc.InstalledAt.Local().Format("2006-01-02"), in.Desc.Host)
	} else {
		fmt.Println("Schedule:   none recorded")
	}
	fmt.Printf("Posture:    %s\n", rep.Posture)
	for _, reason := range rep.Reasons {
		fmt.Printf("  %s\n", reason)
	}
	return nil
}

func statusUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive status - report an archive's completeness, last run and schedule

Usage:
  mailarchive status -out DIR [-name NAME]

Prints a GREEN / WARN / RED posture; every WARN or RED names its remedy. The
exit code is 0 whenever a report is produced (the posture is the answer).

Flags:
`)
		fs.PrintDefaults()
	}
}
