package wizard

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// stampVersion rewrites the archive's manifest's version stamp: 8 is another computer's tidy-up
// part-way.
func stampVersion(t *testing.T, out string, v string) {
	t.Helper()
	mp := filepath.Join(out, ".mailarchive-manifest.json")
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`"version":\s*\d+`)
	if err := os.WriteFile(mp, []byte(re.ReplaceAllString(string(b), `"version": `+v)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1310, R45, R37
// A scheduled backup refused because another computer is tidying the archive (a manifest at
// version 8) is a skip, not a failed run (the B3 UX walk's U2, the lead's call): it exits 0, writes
// no record into the archive (its last run stays as it was), sends no alert and raises no notice,
// and keeps on this computer when the wait began; `status` is the tidy-up WARN with that date,
// never RED. Once the wait has lasted 3 days, the backup alert's failure streak sends one email and
// one notice, and only one however many backups wait; the first backup that goes through again
// removes the wait and sends the recovery. Positive twin first: an ordinary scheduled backup
// keeps no wait.
func TestATidyUpElsewhereIsASkipNotAFailure(t *testing.T) {
	g := newLocalRunRig(t)
	okJob := g.jobAt(filepath.Join(g.dir, "ok.json"), g.mbox)
	if RunHeadless(okJob) != 0 || g.results() != "" {
		t.Fatalf("positive twin: sent %q", g.results())
	}
	if _, ok := gmailanswer.ReadTidyWait(g.out); ok {
		t.Fatal("positive twin: an ordinary backup kept a tidy-up wait")
	}

	stampVersion(t, g.out, "8")
	before, _ := os.ReadFile(filepath.Join(g.out, state.LastRunName))
	if code := RunHeadless(okJob); code != 0 {
		t.Fatalf("a backup meeting another computer's tidy-up exits %d, want 0 (a skip)", code)
	}
	if after, _ := os.ReadFile(filepath.Join(g.out, state.LastRunName)); string(after) != string(before) {
		t.Fatal("the skipped backup wrote a record into the archive")
	}
	if g.results() != "" || len(g.notices) != 0 {
		t.Fatalf("the first days of the wait alert: sent %q, notices %v", g.results(), g.notices)
	}
	w, ok := gmailanswer.ReadTidyWait(g.out)
	if !ok || time.Since(w.Since) > time.Minute {
		t.Fatalf("the wait's start is not kept: %+v %v", w, ok)
	}
	in := health.Gather(g.out, "")
	rep := health.Assess(in, time.Now())
	joined := strings.Join(rep.Reasons, "\n")
	if rep.Posture == "RED" || !slices.Contains(rep.Codes, "manifest_tidying") || slices.Contains(rep.Codes, "run_failed") ||
		!strings.Contains(joined, state.TidyWaitSinceLine(w.Since)) {
		t.Fatalf("status: %s %v\n%s", rep.Posture, rep.Codes, joined)
	}

	// Three days on: one alert, however many backups wait.
	if err := gmailanswer.ClearTidyWait(g.out); err != nil {
		t.Fatal(err)
	}
	if _, err := gmailanswer.NoteTidyWait(g.out, time.Now().Add(-gmailanswer.TidyWaitAlertAfter-time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if RunHeadless(okJob) != 0 {
			t.Fatal("a waiting backup after 3 days is not a skip")
		}
	}
	if g.results() != "failed" || len(g.notices) != 1 {
		t.Fatalf("after 3 days: sent %q, notices %v; want one alert and one notice", g.results(), g.notices)
	}
	if !strings.Contains(g.sent[0].Error, state.TidyUpWords) || !g.sent[0].Tidying {
		t.Fatalf("the alert does not say why, or says FAILED: %+v", g.sent[0])
	}
	if g.notices[0] != tidyWaitNotice(g.out) || strings.Contains(g.notices[0], "failed") {
		t.Fatalf("the notice says the backup failed: %q", g.notices[0])
	}

	// The tidy-up finished: the next backup goes through, the wait goes, the recovery is sent.
	stampVersion(t, g.out, "6")
	if RunHeadless(okJob) != 0 || g.results() != "failed,recovered" {
		t.Fatalf("the backup after the tidy-up: sent %q", g.results())
	}
	if _, ok := gmailanswer.ReadTidyWait(g.out); ok {
		t.Fatal("the wait is kept after a backup went through")
	}
}
