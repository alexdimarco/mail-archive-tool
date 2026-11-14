package desktop

import (
	"html"
	"strings"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1309, R45
// The note shown when someone answers Yes says what starts the backup that applies it, the same
// line the row shows after a reload (the B3 UX walk's U1): nothing more for a scheduled archive;
// Run now for one backed up only by Run now; How often in Settings for one with no backup settings
// here; with "click Switch to this archive" first for a row that is not current. Positive twin
// first: Not now's note is unchanged.
func TestTheYesNoteSaysWhatStartsTheBackup(t *testing.T) {
	e := newDeleteEnv(t)
	run := e.plainArchive("RunNowOnly")
	none := e.plainArchive("NoSettings")
	sched := e.plainArchive("Scheduled")
	nope := e.plainArchive("NotNow")
	for _, out := range []string{run, none, sched, nope} {
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
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": nope, "answer": "not-now", "archive": state.ArchiveID(nope)}); m["note"] != gmailNotNowNote {
		t.Fatalf("positive twin: Not now's note: %v", m)
	}
	for out, want := range map[string]string{
		sched: gmailYesWaits,
		run:   gmailYesWaits + " " + gmailRunNowLead + gmailSwitchFirst + gmailRunNowAct,
		none:  gmailYesWaits + " " + gmailNoJobLead + gmailSwitchFirst + gmailNoJobAct,
	} {
		_, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "yes", "archive": state.ArchiveID(out)})
		if m["ok"] != true || m["note"] != want {
			t.Fatalf("Yes on %s: note %q, want %q", out, m["note"], want)
		}
	}
	// The current archive's note names no Switch.
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: none}); err != nil {
		t.Fatal(err)
	}
	gmailWaits(t, none, "second-store")
	_, m := e.post("/api/gmail-answer", map[string]any{"path": none, "answer": "yes", "archive": state.ArchiveID(none)})
	if want := gmailYesWaits + " " + gmailNoJobLead + gmailNoJobAct; m["note"] != want {
		t.Fatalf("Yes on the current archive: %q, want %q", m["note"], want)
	}
}

// covers: MA-1312, R45
// Yes, a step that cannot be undone, is never the filled primary button: both of the question's
// buttons are drawn as secondary ones (the B3 UX walk's U10). After a Not now, Settings shows one
// short line, "Gmail folders: not switched on.", with a button that opens the question, not the
// whole question each time; and Not now given in Settings says the person can say yes there,
// rather than sending them to Settings (U6). Positive twin first: before any answer, the row asks
// with the box open and Settings draws no collapsed line.
func TestSettingsShowsOneLineAfterNotNowAndYesIsNotPrimary(t *testing.T) {
	e := newDeleteEnv(t)
	out := e.plainArchive("Gmail")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	gmailWaits(t, out, "gmail-store")
	page := html.UnescapeString(e.page())
	if strings.Contains(page, "data-gmail-collapsed") || !strings.Contains(page, `data-gmail-box role="group"`) {
		t.Fatal("positive twin: before any answer the question is shut, or not drawn")
	}
	if strings.Contains(page, `class="btn" type="button" data-gmail-answer="yes"`) || !strings.Contains(page, `class="btn secondary" type="button" data-gmail-answer="yes"`) {
		t.Fatal("Yes is drawn as the primary button")
	}
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "not-now", "from": "settings", "archive": state.ArchiveID(out)}); m["ok"] != true || m["note"] != gmailNotNowSettingsNote {
		t.Fatalf("Not now from Settings: %v", m)
	}
	set := settingsGmail(e.page())
	if !strings.Contains(set, `<p class="card-sub" data-gmail-collapsed>`+gmailCollapsedLine+` <button class="btn secondary" type="button" data-gmail-open>`+gmailCollapsedButton+`</button></p>`) {
		t.Fatalf("after Not now Settings does not show the one line: %q", set)
	}
	if !strings.Contains(set, `data-gmail-settings role="group" aria-label="`+state.GmailQuestionHeading+`" hidden>`) {
		t.Fatalf("after Not now Settings draws the whole question open: %q", set)
	}
	at := strings.Index(dashboardJS, "querySelectorAll('[data-gmail-open]')")
	if at < 0 || !strings.Contains(dashboardJS[at:at+400], "box.hidden=false") {
		t.Fatal("the line's button does not open the question")
	}
	if !strings.Contains(dashboardJS, "from:(box&&box.hasAttribute('data-gmail-settings'))?'settings':''") {
		t.Fatal("Settings' answers do not say they come from Settings")
	}
}
