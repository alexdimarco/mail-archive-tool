package app

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/state"
)

// The friction re-run's fixes for an archive-sourced export (EW-E4, docs/review-extraction-workspace-archive-friction.md).

// historyCard is the card of run on Export activity, as the page reads it after a restart.
func historyCard(t *testing.T, e *CopyExportEnv, run string) CopyRunInfo {
	t.Helper()
	hist, err := e.History(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range hist {
		if c.Run == run {
			return c
		}
	}
	t.Fatalf("the run %s is not in the history", run)
	return CopyRunInfo{}
}

// linkInPlaceOf puts a symlink where a message's original was: the gate refuses it for good (terminal).
func linkInPlaceOf(t *testing.T, a *ceArchive, slug, target string) {
	t.Helper()
	_, eml, _ := a.files(slug)
	_, other, _ := a.files(target)
	os.Remove(eml)
	if err := os.Symlink(other, eml); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// rewrittenByACapture changes a message's original and its recorded fixity together, as a capture does:
// the run lists it as changed in the archive during export (retryable).
func rewrittenByACapture(a *ceArchive, slug string) {
	_, eml, _ := a.files(slug)
	b := append(a.read(eml), []byte("X-Later: a re-capture\r\n")...)
	os.WriteFile(eml, b, 0o644)
	a.setRec(slug, func(r *state.Record) { r.Fixity.EML = digestOf(b) })
}

// covers: MA-437, MA-420, R29, S52
// F3: "Export failed messages again" is offered when a retryable failure is left, and only then. A partial
// archive export whose failures are all terminal (a link where the original should be, a file with several
// hard links) offers Open folder and Check this export and no retry, and a click that would be refused says
// what happened (the failures cannot be tried again), why (they failed for a reason that trying again does not
// change) and what to do (the reasons are under "Which ones?"; restore the file, then export the folder anew).
// The positive twin first: one retryable failure keeps the offer, and the retry is exactly that message.
func TestArchiveExportOffersRetryOnlyWhileARetryableFailureIsLeft(t *testing.T) {
	run := func(t *testing.T, damage func(a *ceArchive), slugs ...string) (*CopyExportEnv, *ceArchive, *CopyResult) {
		a := buildCEArchive(t)
		e := newCEEnv(t)
		s := a.open()
		var keys []string
		for _, slug := range slugs {
			keys = append(keys, a.key(slug))
		}
		p := a.plan(e, s, keys)
		damage(a) // after the preview, as a capture or bit-rot does
		res := a.run(e, p, s)
		if res.Status != CopyStatusPartial || res.Counts.Failed == 0 {
			t.Fatalf("the run: %+v, want partial with failures", res)
		}
		return e, a, res
	}

	t.Run("a retryable failure keeps the offer", func(t *testing.T) {
		e, a, res := run(t, func(a *ceArchive) { rewrittenByACapture(a, "hello") }, "hello", "second")
		if c := historyCard(t, e, res.RunID); !hasAction(c.Actions, ActExportFailed) {
			t.Fatalf("a retryable failure: the card offers %v, want Export failed messages again", c.Actions)
		}
		in, err := e.RetryInput(res.RunID, "failed", "")
		if err != nil || len(in.Messages) != 1 || in.Messages[0].SourceID != a.key("hello") {
			t.Fatalf("the retry of the retryable failure: %v, %v", in, err)
		}
	})

	t.Run("only terminal failures: no offer, and the click says why", func(t *testing.T) {
		e, a, res := run(t, func(a *ceArchive) { linkInPlaceOf(t, a, "hello", "second") }, "hello", "second")
		if f := failureListed(manifestOf(t, res.Root), a.key("hello")); f == nil || f["class"] != "terminal" {
			t.Fatalf("hello's failure: %v, want terminal", f)
		}
		c := historyCard(t, e, res.RunID)
		if hasAction(c.Actions, ActExportFailed) || !hasAction(c.Actions, ActOpenFolder) || !hasAction(c.Actions, ActCheckThis) {
			t.Errorf("the card of a run whose failures are all terminal offers %v: want Open folder and Check this export, no retry", c.Actions)
		}
		before := treeDigest(t, filepath.Dir(res.Root))
		_, err := e.RetryInput(res.RunID, "failed", "")
		refusedWith(t, err, "R-h", []string{"1 message that failed", "can't be tried again", "Which ones?", "restore"})
		if treeDigest(t, filepath.Dir(res.Root)) != before {
			t.Error("the refused retry wrote into the destination")
		}
	})

	t.Run("a terminal and a retryable failure: the offer stays, for the retryable one only", func(t *testing.T) {
		e, a, res := run(t, func(a *ceArchive) {
			linkInPlaceOf(t, a, "hello", "client-mail")
			rewrittenByACapture(a, "second")
		}, "hello", "second", "client-mail")
		if c := historyCard(t, e, res.RunID); !hasAction(c.Actions, ActExportFailed) {
			t.Fatalf("the card offers %v, want Export failed messages again", c.Actions)
		}
		in, err := e.RetryInput(res.RunID, "failed", "")
		if err != nil || len(in.Messages) != 1 || in.Messages[0].SourceID != a.key("second") {
			t.Fatalf("the retry: %v, %v, want only the retryable message", in, err)
		}
	})
}
