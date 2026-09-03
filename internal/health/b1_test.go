package health

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

func lineIndex(lines []string, prefix string) int {
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			return i
		}
	}
	return -1
}

// covers: MA-111, R18, S29
// The no-schedule WARN shapes its "keep it current" remedy from the job that
// actually made the archive (its -input, its -auto or not) instead of always
// suggesting -auto; older records with no recorded job get a generic phrase,
// never an invented -auto. Either way it stays WARN and acknowledges that an
// external scheduler (systemd timer, NAS task) may already keep it current.
func TestStatusRemedyFromJob(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	base := healthyInput(now)
	base.HasDescriptor = false
	base.DescErr = os.ErrNotExist

	// Shaped from a recorded -input job: repeats -input, no invented -auto.
	in := base
	in.LastRun.Job = []string{"-out", "/a", "-mode", "incremental", "-input", "/a/mail.pst"}
	reasons := strings.Join(Assess(in, now).Reasons, "\n")
	if !strings.Contains(reasons, "no schedule") {
		t.Errorf("expected the no-schedule WARN:\n%s", reasons)
	}
	if !strings.Contains(reasons, "-input /a/mail.pst") {
		t.Errorf("remedy did not repeat the recorded job's -input:\n%s", reasons)
	}
	if strings.Contains(reasons, "-auto") {
		t.Errorf("remedy hard-coded -auto for a job that had none:\n%s", reasons)
	}
	if !strings.Contains(reasons, "systemd timer") {
		t.Errorf("WARN did not acknowledge external schedulers:\n%s", reasons)
	}

	// A recorded -auto job keeps -auto.
	in = base
	in.LastRun.Job = []string{"-out", "/a", "-mode", "incremental", "-auto"}
	if r := strings.Join(Assess(in, now).Reasons, "\n"); !strings.Contains(r, "-auto") {
		t.Errorf("remedy dropped the recorded -auto:\n%s", r)
	}

	// Older record with no job: generic phrase, no invented -auto, still WARN.
	in = base
	in.LastRun.Job = nil
	rep := Assess(in, now)
	r := strings.Join(rep.Reasons, "\n")
	if !strings.Contains(r, "same job that made this archive") {
		t.Errorf("no-job fallback missing the generic phrase:\n%s", r)
	}
	if !strings.Contains(r, "systemd timer") {
		t.Errorf("fallback missing the external-scheduler note:\n%s", r)
	}
	if rep.Posture != "WARN" {
		t.Errorf("no-schedule must stay WARN, got %s", rep.Posture)
	}
}

// covers: MA-112, R18, S29
// The staleness WARN is OS-aware: a cron host asks whether the machine was on
// and crond running (cron needs no login); launchd and Task Scheduler hosts
// keep "logged in", which is true only for them.
func TestStalenessWordingOSAware(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	stale := func(goos string) string {
		in := healthyInput(now)
		in.GOOS = goos
		in.LastRun.Started = now.Add(-72 * time.Hour)
		in.LastRun.Finished = now.Add(-71 * time.Hour)
		return strings.Join(Assess(in, now).Reasons, "\n")
	}
	if r := stale("linux"); !strings.Contains(r, "crond running") || strings.Contains(r, "logged in") {
		t.Errorf("linux staleness wording wrong:\n%s", r)
	}
	if r := stale("freebsd"); !strings.Contains(r, "crond running") {
		t.Errorf("bsd staleness wording should mention crond:\n%s", r)
	}
	if r := stale("darwin"); !strings.Contains(r, "logged in") {
		t.Errorf("darwin staleness wording should say logged in:\n%s", r)
	}
	if r := stale("windows"); !strings.Contains(r, "logged in") {
		t.Errorf("windows staleness wording should say logged in:\n%s", r)
	}
}

// covers: MA-113, MA-118, R18, S29
// status's completeness line uses the same labels as the export/GUI summary
// ("still missing content", "source-empty (never fillable)", "not yet
// re-examined") with a plain-language gloss, cites the report file only when it
// exists on disk, and prints the archived date range directly after Messages
// when the index has one.
func TestIncompleteLabelsAndRange(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.Fillable, in.Terminal, in.Unknown = 3, 1, 2
	in.ReportPath = "/a/attachments-report.tsv"

	// Report absent: unified labels + gloss, path NOT cited, old wording gone.
	in.ReportExists = false
	lines := strings.Join(Summary(in, Assess(in, now)), "\n")
	for _, want := range []string{"still missing content", "source-empty (never fillable)", "not yet re-examined", "not downloaded yet; fills on the next run"} {
		if !strings.Contains(lines, want) {
			t.Errorf("summary lacks unified label/gloss %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "attachments-report.tsv") {
		t.Errorf("summary cited a report that does not exist:\n%s", lines)
	}
	if strings.Contains(lines, "still fillable") || strings.Contains(lines, "(terminal)") {
		t.Errorf("summary kept the old completeness wording:\n%s", lines)
	}

	// Report present: the path IS cited.
	in.ReportExists = true
	if l := strings.Join(Summary(in, Assess(in, now)), "\n"); !strings.Contains(l, "/a/attachments-report.tsv") {
		t.Errorf("summary omitted an existing report path:\n%s", l)
	}

	// Archived range prints directly after the Messages line.
	in.HasRange = true
	in.RangeOldest = time.Date(2019, 2, 1, 0, 0, 0, 0, time.UTC)
	in.RangeNewest = time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	l := Summary(in, Assess(in, now))
	joined := strings.Join(l, "\n")
	if !strings.Contains(joined, "Archived range: 2019-02-01 – 2026-07-03 (UTC)") {
		t.Errorf("summary lacks the archived range line:\n%s", joined)
	}
	mi, ri := lineIndex(l, "Messages:"), lineIndex(l, "Archived range:")
	if mi < 0 || ri != mi+1 {
		t.Errorf("archived range not directly after Messages: msg=%d range=%d", mi, ri)
	}
}

// covers: MA-114, R18, S29
// A torn or hand-edited last-run record WARNs that it is "corrupt or truncated"
// and that the next run rewrites it — without echoing the raw decoder error at
// the operator.
func TestTornRecordWording(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.LastRunState = state.LastRunUnreadable
	in.LastRunErr = errors.New("invalid character 'x' looking for beginning of value")
	r := strings.Join(Assess(in, now).Reasons, "\n")
	if !strings.Contains(r, "corrupt or truncated") {
		t.Errorf("torn-record WARN lacks the plain wording:\n%s", r)
	}
	if !strings.Contains(r, "unreadable") {
		t.Errorf("torn-record WARN should still say unreadable:\n%s", r)
	}
	if strings.Contains(r, "invalid character") {
		t.Errorf("torn-record WARN leaked the raw decoder error:\n%s", r)
	}
}

// covers: MA-115, R18, S29
// health.JSON emits a typed, versioned document carrying the posture, reasons,
// counts, and the last_run and schedule facets under their documented keys — a
// machine-readable twin of the text report that never changes the exit code.
func TestJSONReport(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	doc := JSON(in, Assess(in, now))
	if doc.Version != JSONVersion {
		t.Errorf("version = %d, want %d", doc.Version, JSONVersion)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "posture", "reasons", "messages", "indexed", "fillable", "terminal", "unknown", "last_run", "schedule", "out"} {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON document missing key %q:\n%s", k, data)
		}
	}
	lr, ok := m["last_run"].(map[string]any)
	if !ok {
		t.Fatalf("last_run is not an object:\n%s", data)
	}
	for _, k := range []string{"status", "started", "finished", "exported", "filled"} {
		if _, ok := lr[k]; !ok {
			t.Errorf("last_run missing key %q", k)
		}
	}
	sch, ok := m["schedule"].(map[string]any)
	if !ok {
		t.Fatalf("schedule is not an object:\n%s", data)
	}
	for _, k := range []string{"name", "state", "interval", "at", "exe", "host"} {
		if _, ok := sch[k]; !ok {
			t.Errorf("schedule missing key %q", k)
		}
	}
}

// covers: MA-116, R18, S29
// jobOutOf reads the schedule job's -out (both spellings). Assess WARNs when
// the installed schedule backs up a different archive than the one inspected
// (naming both paths and the re-install/remove remedy), and when the archive
// sits inside a cloud-sync folder (naming the service and the remedy).
func TestScheduleTargetAndCloudSync(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	if got := jobOutOf([]string{"-out", "/x", "-mode", "full"}); got != "/x" {
		t.Errorf("jobOutOf(-out /x) = %q", got)
	}
	if got := jobOutOf([]string{"-mode", "full", "-out=/y"}); got != "/y" {
		t.Errorf("jobOutOf(-out=/y) = %q", got)
	}
	if got := jobOutOf([]string{"-auto"}); got != "" {
		t.Errorf("jobOutOf(none) = %q, want empty", got)
	}

	// Schedule targets a different archive → WARN naming both paths + remedy.
	in := healthyInput(now)
	in.Out = "/archives/current"
	in.Desc.Job = []string{"-out", "/archives/old", "-auto"}
	r := strings.Join(Assess(in, now).Reasons, "\n")
	if !strings.Contains(r, "/archives/old") || !strings.Contains(r, "/archives/current") {
		t.Errorf("target-mismatch WARN missing a path:\n%s", r)
	}
	if !strings.Contains(r, "not this archive") || !strings.Contains(r, "-remove") {
		t.Errorf("target-mismatch WARN missing the remedy:\n%s", r)
	}

	// Same target → no such WARN.
	in = healthyInput(now)
	in.Out = "/archives/current"
	in.Desc.Job = []string{"-out", "/archives/current", "-auto"}
	if r := strings.Join(Assess(in, now).Reasons, "\n"); strings.Contains(r, "not this archive") {
		t.Errorf("matching target wrongly warned:\n%s", r)
	}

	// Cloud-sync WARN names the service and the remedy, staying WARN.
	in = healthyInput(now)
	in.OutUnderSync = "OneDrive"
	rep := Assess(in, now)
	r = strings.Join(rep.Reasons, "\n")
	if !strings.Contains(r, "OneDrive") || !strings.Contains(r, "synced folder") {
		t.Errorf("cloud-sync WARN missing the service:\n%s", r)
	}
	if !strings.Contains(r, "non-synced folder") {
		t.Errorf("cloud-sync WARN missing the remedy:\n%s", r)
	}
	if rep.Posture != "WARN" {
		t.Errorf("cloud-sync should be WARN, got %s", rep.Posture)
	}
}
