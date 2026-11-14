package desktop

import (
	"html"
	"strings"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1294, R45
// The Gmail question on an archive's row after the B3 friction review (F3, F5, F15, F16).
// Positive twin first: a row with no Gmail store off draws no question box. A row whose last
// backup found one draws the question in a box of its own, headed "Gmail: show every folder?",
// with the buttons "Yes, show every folder" and "Not now"; the page's script puts the answer's
// note in the box in place of the question and does not reload the page. After Yes the row's
// line says what starts the next backup: nothing more for a scheduled archive; Run now (after
// Switch to this archive, for a row that is not current) for one backed up only by Run now; How
// often in Settings for one with no backup settings here. A row's problem text (the tidy-up
// words) is drawn at full contrast, never in the greyed "unavailable" style.
func TestTheGmailQuestionHasItsOwnBoxAndSaysWhatStartsTheNextBackup(t *testing.T) {
	e := newDeleteEnv(t)
	run := e.plainArchive("RunNowOnly")
	none := e.plainArchive("NoSettings")
	sched := e.plainArchive("Scheduled")
	if page := e.page(); strings.Contains(page, "data-gmail-box") {
		t.Fatal("positive twin: a question box is drawn with no Gmail store off")
	}
	for _, out := range []string{run, none, sched} {
		gmailWaits(t, out, "gmail-store")
	}
	jp, _ := job.PathFor(schedule.DefaultNameFor(run))
	if err := job.Write(jp, job.Job{Name: "r", Out: run, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	sp, _ := job.PathFor(schedule.DefaultNameFor(sched))
	if err := job.Write(sp, job.Job{Name: "s", Out: sched, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	if err := schedule.WriteDescriptor(sched, schedule.Descriptor{Version: 1, Name: "s", Interval: "daily", At: "03:00"}); err != nil {
		t.Fatal(err)
	}

	page := html.UnescapeString(e.page())
	box := `<div class="gmail-ask" data-gmail-box role="group" aria-label="` + state.GmailQuestionHeading + `"><b class="gmail-ask-head">` + state.GmailQuestionHeading + `</b><p class="gmail-ask-body" data-gmail-question>` + state.GmailQuestion + `</p>`
	if strings.Count(page, box) != 3 {
		t.Fatalf("the question is not in its own headed box on each row (%d)", strings.Count(page, box))
	}
	if !strings.Contains(page, `data-gmail-answer="yes" data-path="`+run+`">`+state.GmailYesButton+`</button>`) ||
		!strings.Contains(page, `data-gmail-answer="not-now" data-path="`+run+`">`+state.GmailNotNowButton+`</button>`) {
		t.Fatal("the box's buttons are not named \"Yes, show every folder\" and \"Not now\"")
	}
	at := strings.Index(dashboardJS, "querySelectorAll('[data-gmail-answer]')")
	if at < 0 {
		t.Fatal("the page has no script for the question's buttons")
	}
	script := dashboardJS[at:]
	script = script[:strings.Index(script, "}));")]
	if strings.Contains(script, "reloadPage") || !strings.Contains(script, "[data-gmail-question]") || !strings.Contains(script, "data-gmail-note") {
		t.Fatalf("the answer's note does not replace the question in its box, or the page still reloads: %s", script)
	}

	for _, out := range []string{run, none, sched} {
		if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "yes", "archive": state.ArchiveID(out)}); m["ok"] != true {
			t.Fatalf("Yes on %s: %v", out, m)
		}
	}
	if r := cardRow(t, e, sched); r.GmailYes != gmailYesWaits {
		t.Fatalf("a scheduled archive's yes line: %q", r.GmailYes)
	}
	if r := cardRow(t, e, run); r.GmailYes != gmailYesWaits+" "+gmailRunNowLead+gmailSwitchFirst+gmailRunNowAct {
		t.Fatalf("a Run now only archive's yes line: %q", r.GmailYes)
	}
	if r := cardRow(t, e, none); r.GmailYes != gmailYesWaits+" "+gmailNoJobLead+gmailSwitchFirst+gmailNoJobAct {
		t.Fatalf("an archive with no backup settings: %q", r.GmailYes)
	}
	// The current archive's line names no Switch.
	if got := gmailYesWaitsFor(run, true); got != gmailYesWaits+" "+gmailRunNowLead+gmailRunNowAct {
		t.Fatalf("the current archive's yes line: %q", got)
	}

	// F16: the problem text is never greyed.
	if !strings.Contains(page, `.row-state:not(:has(.archive-problem)){opacity:.6}`) || !strings.Contains(page, `.archive-tag.archive-problem{opacity:1}`) ||
		!strings.Contains(overviewTmpl.Lookup("archiveRow").Tree.Root.String(), `<span class="archive-tag archive-problem">`) {
		t.Fatal("a row's problem text is drawn in the greyed unavailable style")
	}
}
