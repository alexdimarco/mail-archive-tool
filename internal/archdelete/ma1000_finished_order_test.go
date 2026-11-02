package archdelete

import (
	"errors"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1000, R36
// ExecDeps.Finished, the command line's word that the delete is over, is said once, after the
// steps and before step 10 writes the finished line to deletions.log, never after it: a signal
// once the log says the delete finished is then never "did not finish" (the residual fixes'
// adversarial pass, finding 5). A delete stopped part way says it before its finished line too,
// and a refusal before the started line says it as Execute returns.
func TestFinishedIsSaidBeforeTheFinishedLineIsWritten(t *testing.T) {
	finishedLine := `"event":"` + evFinished + `"`
	run := func(r *rig, typed func(*DeletePlan) string) (*Result, int, string) {
		t.Helper()
		p := r.plan(t)
		calls, logThen := 0, ""
		r.x.Finished = func() {
			calls++
			b, _ := os.ReadFile(r.x.Log)
			logThen = string(b)
		}
		return Execute(p, typed(p), false, r.x), calls, logThen
	}
	logNow := func(r *rig) string {
		b, _ := os.ReadFile(r.x.Log)
		return string(b)
	}

	// Positive twin: a delete that completes says it once, its steps done, its finished line not
	// yet written; the line is written after.
	r := newRig(t, plainBesidePersonsFiles(t))
	res, calls, then := run(r, func(p *DeletePlan) string { return p.Name })
	assure.Reached(t, res.Status == StatusComplete && calls == 1 && len(r.rows) == 1, "a completed delete saying it is over")
	if !strings.Contains(then, `"event":"`+evStarted+`"`) || strings.Contains(then, finishedLine) || !strings.Contains(logNow(r), finishedLine) {
		t.Fatalf("Finished was not said between the steps and the finished line:\nthen:\n%s\nafter:\n%s", then, logNow(r))
	}

	// Stopped part way (before step 4): said once, before its finished line.
	r = newRig(t, plainBesidePersonsFiles(t))
	r.x.BeforeStep = func(step int) error {
		if step == 4 {
			return errors.New("MailArchive Desktop started while the delete was running")
		}
		return nil
	}
	res, calls, then = run(r, func(p *DeletePlan) string { return p.Name })
	if res.Status != StatusPartial || calls != 1 || strings.Contains(then, finishedLine) || !strings.Contains(logNow(r), finishedLine) {
		t.Fatalf("a delete stopped part way: status %q, Finished said %d times\nthen:\n%s\nafter:\n%s", res.Status, calls, then, logNow(r))
	}

	// Refused before the started line (a name that does not match): said once, nothing logged.
	r = newRig(t, plainBesidePersonsFiles(t))
	res, calls, then = run(r, func(*DeletePlan) string { return "not the name" })
	if res.Status != StatusRefused || calls != 1 || then != "" || logNow(r) != "" {
		t.Fatalf("a refused delete: status %q, Finished said %d times, log %q", res.Status, calls, logNow(r))
	}
}
