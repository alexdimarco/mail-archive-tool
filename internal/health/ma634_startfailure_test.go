package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-634, MA-167, MA-168, R18, S59
// §4.6.6: health.Gather is the one reader of the headless start-failure
// breadcrumb, keyed on the owner-pointed out (schedule.DefaultNameFor), never
// the archive-held Desc.Name — so a forged Desc.Name cannot hide a start
// failure. Assess raises RED start_failed first, under MA-168's rule (the
// archive is unreachable, or the failure is newer than the last recorded run).
// Re-homes the reader's round-trip + control-stripping (MA-167) and the show
// rule (MA-168) from internal/wizard, which moved here with the reader.
func TestMA634_StartFailureBreadcrumb(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	out := "/archives/current"
	name := schedule.DefaultNameFor(out)

	// A seam over the breadcrumb directory (failureDir = job.Dir in production).
	dir := t.TempDir()
	orig := failureDir
	failureDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { failureDir = orig })
	path := filepath.Join(dir, startFailureName)
	write := func(f startFailure) {
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// MA-167: the reader round-trips and strips control characters planted in the
	// user-writable file before the reason can reach a dialog.
	write(startFailure{Version: 1, Name: "job\x07", When: now, Reason: "line1\nline2\x00tail"})
	if rec, ok := readStartFailure(dir); !ok {
		t.Fatal("readStartFailure found no record after one was written")
	} else if strings.ContainsAny(rec.Name+rec.Reason, "\x00\x07\n") {
		t.Errorf("control characters survived the read: %q / %q", rec.Name, rec.Reason)
	} else if !strings.Contains(rec.Reason, "line1") || !strings.Contains(rec.Reason, "tail") {
		t.Errorf("stripping removed content, not just control chars: %q", rec.Reason)
	}

	// Gather reads a breadcrumb whose Name is DefaultNameFor(out), and only that
	// one: the key is the owner-pointed out, so another archive's breadcrumb is
	// ignored and a forged Desc.Name is irrelevant.
	write(startFailure{Version: 1, Name: name, When: now, Reason: "drive not mounted"})
	if in := Gather(out, ""); !in.HasStartFailure {
		t.Error("Gather did not read the matching breadcrumb")
	}
	write(startFailure{Version: 1, Name: schedule.DefaultNameFor("/some/other/archive"), When: now, Reason: "x"})
	if in := Gather(out, ""); in.HasStartFailure {
		t.Error("Gather read another archive's breadcrumb")
	}
	os.Remove(path)
	if in := Gather(out, ""); in.HasStartFailure {
		t.Error("Gather reported a start failure with no breadcrumb")
	}

	// Assess's MA-168 rule, via a directly-built Input (the breadcrumb read is
	// covered above). reachable means the archive's own last run can speak.
	has := func(r Report, code string) bool {
		for _, c := range r.Codes {
			if c == code {
				return true
			}
		}
		return false
	}
	in := func(failWhen time.Time, reachable bool, lastRun time.Time) Input {
		i := Input{Out: out, HasStartFailure: true, LastStartFailure: startFailure{Name: name, When: failWhen, Reason: "drive not mounted"}}
		if reachable {
			i.HasManifest = true
			i.LastRunState = state.LastRunPresent
			i.LastRun.Started = lastRun
		}
		return i
	}

	// Unreachable archive → RED start_failed, and it is the first reason shown.
	r := Assess(in(now, false, time.Time{}), now)
	if !has(r, "start_failed") || r.Posture != "RED" {
		t.Fatalf("an unreachable archive with a breadcrumb must raise RED start_failed:\n%v", r.Reasons)
	}
	if len(r.Codes) == 0 || r.Codes[0] != "start_failed" {
		t.Errorf("start_failed must be the first reason:\n%v", r.Reasons)
	}
	if !strings.Contains(r.Reasons[0], "drive not mounted") || !strings.Contains(r.Reasons[0], "records a run here") {
		t.Errorf("start_failed line must name the reason and the remedy:\n%s", r.Reasons[0])
	}

	// Reachable, failure newer than the last run → still shown.
	if r := Assess(in(now, true, now.Add(-time.Hour)), now); !has(r, "start_failed") {
		t.Error("a failure newer than the last run must surface")
	}
	// Reachable, a later successful run supersedes it → not shown.
	if r := Assess(in(now, true, now.Add(time.Hour)), now); has(r, "start_failed") {
		t.Error("a later successful run must supersede the breadcrumb")
	}
	// Unreachable surfaces even when the failure looks older than a stale last run.
	if r := Assess(in(now.Add(-2*time.Hour), false, time.Time{}), now); !has(r, "start_failed") {
		t.Error("an unreachable archive must surface the failure regardless of the stale last run")
	}
	// No breadcrumb → never shown.
	if r := Assess(Input{Out: out}, now); has(r, "start_failed") {
		t.Error("no breadcrumb must not raise start_failed")
	}
}
