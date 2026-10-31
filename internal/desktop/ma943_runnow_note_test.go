package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
)

// addMailToSource appends one new message to the Run now job's first mail folder's Inbox.
func addMailToSource(t *testing.T, jobPath, id string) {
	t.Helper()
	j, err := job.Read(jobPath)
	if err != nil || len(j.Inputs) == 0 {
		t.Fatalf("the job's inputs: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(j.Inputs[0], "Inbox"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	msg := "From - Tue Mar  4 09:00:00 2025\nFrom: c@example.com\nTo: b@example.com\nSubject: new\nDate: Tue, 04 Mar 2025 09:00:00 +0000\nMessage-ID: <" + id + "@ex>\n\nnew body\n\n"
	if _, err := f.WriteString(msg); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-943, MA-906
// The Run now note when a backup ends (owner report from the Windows lab of 2026-10-31:
// the note was a wall of text — the wizard's export summary with "Newly downloaded this
// run: 0" beside "Exported: 20351", "Your data stays on this computer", the output folder
// and "to search the whole archive at once, use the command-line tool: mailarchive serve
// -out …"). Now it leads with one plain line, the backup's — "Backup finished: 1 new
// message archived." — then the Thunderbird sync's line, then only what to act on, each
// on its own line (the note keeps its line breaks: class "note steps"). The backup's own
// line leads in every ending: a failure's "Backup failed: …" and a cancel while reading
// come before the sync's line too; a sync itself cancelled is told by the cancel words
// alone, as before. Positive twin first: the Run now syncs and backs up, the record ok.
func TestRunNowNoteLeadsWithTheBackup(t *testing.T) {
	g := newRunNowRig(t, "darwin", true)
	addMailToSource(t, g.jp, "n1")
	g.start()
	g.at(presync.PhaseWaiting)
	g.s.release <- struct{}{}
	m := g.done()
	if m["error"] != "" || g.lastRunStatus() != "ok" {
		t.Fatalf("the positive twin: the Run now did not finish: %v", m)
	}
	want := "Backup finished: 1 new message archived.\nThunderbird synced: its mail stopped arriving after 6 min. It was left open out of sight; quit it from the Dock when you are done."
	if n := str(m["note"]); n != want {
		t.Errorf("the end note:\n got %q\nwant %q", n, want)
	}
	for _, gone := range []string{"Export complete.", "Newly downloaded this run", "Exported:", "mailarchive serve", "Output folder", "Your data stays on this computer"} {
		if strings.Contains(str(m["note"]), gone) {
			t.Errorf("the end note still carries %q", gone)
		}
	}
	page, _ := getPage(t, DashboardHandler(g.d.cfg))
	if !strings.Contains(page, `<div class="note steps" id="runBackupNote" hidden></div>`) {
		t.Error("Run now's note does not keep its line breaks")
	}

	// The backup's line leads in every ending; a sync itself cancelled keeps its words alone.
	for _, c := range []struct{ sync, name, result, err, want string }{
		{"Thunderbird synced: x.", "ok", "", "the disk is full", "Backup failed: the disk is full\nThunderbird synced: x."},
		{"Thunderbird synced: x.", "ok", cancelledText, "", cancelledText + "\nThunderbird synced: x."},
		{"Thunderbird synced: x.", "ok", "Backup finished: no new messages.\nWhich ones: /r", "", "Backup finished: no new messages.\nThunderbird synced: x.\nWhich ones: /r"},
		{"", "", "Backup finished: no new messages.", "", "Backup finished: no new messages."},
		{"Cancelled sync line", presync.OutcomeCancelled.Name, "Cancelled. The archive is unchanged.", "", "Cancelled. The archive is unchanged."},
	} {
		if got := runNowEndNote(c.sync, c.name, c.result, c.err); got != c.want {
			t.Errorf("runNowEndNote(%q, %q, %q, %q) = %q, want %q", c.sync, c.name, c.result, c.err, got, c.want)
		}
	}
}

// covers: MA-944, MA-906
// The owner's Windows lab of 2026-10-31, run 3: after a third Run now (ended with Stop
// waiting, 135 archived) the owner pasted the second run's note ("Thunderbird was already
// open, so it was not started or closed … Exported: 141"). Reproduced on the real Desktop
// handlers with the scripted Thunderbird: two Run nows on one page — the first finding
// Thunderbird already open, the second ended by Stop waiting with new mail — and after
// the second, /api/activity's note, sync line and result are the second's only, and the
// page's script writes the server's note over the note at each end. Not reproduced: the
// note is not kept from the run before (the pasted text was most likely the clipboard).
func TestASecondRunNowShowsItsOwnNote(t *testing.T) {
	g := newRunNowRig(t, "windows", true)
	g.s.alreadyOpen = true
	g.start()
	first := g.done()
	g.s.alreadyOpen = false
	if n := str(first["note"]); !strings.Contains(n, "Thunderbird was already open") || !strings.HasPrefix(n, "Backup finished: no new messages.") {
		t.Fatalf("the positive twin: the first Run now's note %q", n)
	}

	addMailToSource(t, g.jp, "n2")
	g.s.waitedFor = 3 * time.Minute
	g.start()
	g.at(presync.PhaseWaiting)
	if m := g.activity(); m["note"] != nil || m["syncLine"] != nil || str(m["result"]) != "" {
		t.Errorf("while the second Run now runs, the first one's end is still shown: %v", m)
	}
	if r := callAction(t, g.d, g.d.stopWaitAction, map[string]any{"run": str(g.activity()["run"])}); r["ok"] != true {
		t.Fatalf("Stop waiting: %v", r)
	}
	second := g.done()
	n := str(second["note"])
	if !strings.HasPrefix(n, "Backup finished: 1 new message archived.\nThunderbird sync: you stopped the wait after 3 min") {
		t.Errorf("the second Run now's note is not its own: %q", n)
	}
	for _, old := range []string{"already open", "not started or closed", "no new messages"} {
		if strings.Contains(n+str(second["syncLine"])+str(second["result"]), old) {
			t.Errorf("the second Run now's end still carries the first one's %q: %v", old, second)
		}
	}
	if !strings.Contains(dashboardJS, "if(n){n.hidden=false;n.textContent=d.note||d.otherNote||") ||
		!strings.Contains(dashboardJS, "const n=$('runBackupNote'),done=busy(rbBtn,'Running…',n,'Running the backup now…');") {
		t.Error("the page's script does not replace Run now's note at the start and at the end")
	}
}
