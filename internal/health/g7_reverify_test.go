package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-386, R12, R18, S33
// The owner's health view speaks the owner's words about the last run's failure
// too (g7 friction re-verification of F3). A run refused at the search index is
// recorded with the CLI's text (`mailarchive reindex -rebuild -out …`, SQLite's
// "file is not a database (26)"), and the RED run_failed reason and the "Last
// run: … failed:" line printed that verbatim on MailArchive Desktop and
// mailarchive-gui, beside the surface-aware index lines and the Rebuild choice.
// The record now carries the refusal's kind, and on an owner surface both lines
// are rendered from it, with the surface's own rebuild action and no command or
// SQLite text. The CLI and -json keep the recorded text; a failed run with no
// kind (the source gone, a bad password) is printed verbatim everywhere.
func TestOwnerSurfacesRenderTheLastRunsIndexRefusal(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	failed := func(kind, text string) Input {
		in := healthyInput(now)
		in.LastRun = state.LastRun{Status: state.RunFailed, Started: now.Add(-2 * time.Hour), Finished: now.Add(-2 * time.Hour), PID: 1, Error: text, ErrorKind: kind}
		return in
	}
	lines := func(in Input) (reason, last string) {
		rep := Assess(in, now)
		reason = assure.Reached(t, reasonOf(rep, "run_failed"), "the run_failed reason")
		for _, l := range Summary(in, rep) {
			if strings.HasPrefix(l, "Last run:") {
				last = l
			}
		}
		return reason, assure.Reached(t, last, "the Last run line")
	}
	cliText := "search index /a/search.db cannot be opened (index pragmas: file is not a database (26)): rebuild it from the archive with `mailarchive reindex -rebuild -out \"/a\"`, then run again"

	// Positive twin: the CLI keeps the recorded text, command and cause.
	reason, last := lines(failed(state.ErrKindIndexDamaged, cliText))
	for _, s := range []string{reason, last} {
		if !strings.Contains(s, "mailarchive reindex -rebuild") || !strings.Contains(s, "file is not a database (26)") {
			t.Fatalf("the CLI lost the recorded text: %q", s)
		}
	}
	// Positive twin: a failure with no kind is verbatim on an owner surface.
	in := failed("", "input /mnt/mail does not exist (check the path)")
	in.Surface = "gui"
	reason, last = lines(in)
	if !strings.Contains(reason, "input /mnt/mail does not exist") || !strings.Contains(last, "input /mnt/mail does not exist") {
		t.Fatalf("a failure with no kind was rewritten: %q / %q", reason, last)
	}

	cases := []struct {
		kind, text string
		surface    string
		want       []string
	}{
		{state.ErrKindIndexDamaged, cliText, "desktop", []string{"damaged", "Maintenance → Rebuild search index"}},
		{state.ErrKindIndexDamaged, cliText, "gui", []string{"damaged", "Rebuild the search index"}},
		{state.ErrKindIndexBusy, "search index /a/search.db is busy (index pragmas: database is locked (5) (SQLITE_BUSY)): another program kept it locked — close any other program that has it open, then run again", "gui", []string{"in use by another program"}},
		{state.ErrKindIndexUnwritable, "search index /a/search.db cannot be written (index write check: attempt to write a readonly database (8)): search.db is read-only — make it writable, then run again", "desktop", []string{"cannot be written to"}},
		{state.ErrKindIndexDamagedNoManifest, "search index /a/search.db cannot be opened (file is not a database (26)), and there is no manifest (/a/.mailarchive-manifest.json) to rebuild it from: move it out of /a, then run again", "gui", []string{"no manifest", "move"}},
	}
	for _, c := range cases {
		in := failed(c.kind, c.text)
		in.Surface = c.surface
		reason, last := lines(in)
		for _, s := range []string{reason, last} {
			for _, w := range c.want {
				if !strings.Contains(s, w) {
					t.Errorf("%s/%s: %q lacks %q", c.kind, c.surface, s, w)
				}
			}
			for _, bad := range []string{"mailarchive reindex", "(26)", "SQLITE", "(5)", "(8)", "pragmas"} {
				if strings.Contains(s, bad) {
					t.Errorf("%s/%s: owner text carries %q: %q", c.kind, c.surface, bad, s)
				}
			}
		}
	}
}
