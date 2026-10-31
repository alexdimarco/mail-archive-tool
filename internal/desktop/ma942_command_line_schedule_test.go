package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// writeCommandLineSchedule records an export schedule for out, as `mailarchive schedule
// ... -input <folder> -out <out>` at a terminal does.
func writeCommandLineSchedule(t *testing.T, out string) {
	t.Helper()
	exp := schedule.Spec{Name: schedule.DefaultNameFor(out), Interval: schedule.Weekly, At: "03:00", Exe: "/opt/mailarchive",
		Args: []string{"-input", filepath.Join(out, "x"), "-out", out, "-mode", "incremental"}, Out: out}
	if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(exp)); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-942, MA-860
// The owner's Windows lab of 2026-10-31: a schedule made at the command line showed "This
// archive's scheduled backup: an export backup — repair it from the command line (see
// Status & health)." and no Run now, without saying why. Now the card names it "a backup
// set up at the command line" and says why Run now isn't shown and how to get it — on a
// local archive this computer can schedule, Remove on this card, then How often in Settings
// and Save settings (Save settings alone is refused for such a schedule, C-7). Each step
// the words name is driven: Save settings is refused with nothing changed; after Remove (the
// scheduler entry and, as schedule.Remove does, the archive's record of it go), Settings
// shows How often, Save settings sets up a backup the Desktop can run, and Run now appears.
// Where that path does not exist (Linux here, a Microsoft 365 archive) the card says to
// change it at the command line and that this card can only remove it. Settings' read-only
// remedy says the same. Positive twin first: a Desktop-made schedule shows Run now.
func TestACommandLineScheduleSaysWhyNoRunNow(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin's create: %s", body)
	}
	g.recordLast()
	if p := g.page(); !strings.Contains(p, `id="runBackupBtn"`) || strings.Contains(p, `id="schedExport"`) {
		t.Fatal("the positive twin: a Desktop-made schedule has no Run now, or is told as a command-line one")
	}
	os.Remove(g.jobPath)
	delete(g.fake.tasks, out)

	writeCommandLineSchedule(t, out)
	g.fake.tasks[out] = true
	page := g.page()
	for _, want := range []string{
		"Run now isn't shown for it: MailArchive Desktop can't run or change a backup set up at the command line. To get Run now, click Remove below (your archive and its mail are kept), then in Settings choose How often and click Save settings. That sets up a backup MailArchive Desktop can run, and Run now then appears here.",
		"This schedule was set up at the command line; change it there. To get one MailArchive Desktop can run, with Run now, click Remove on the Scheduled backup card, then choose How often here and click Save settings.",
	} {
		if !shows(page, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
	// template text, not escaped data
	if !strings.Contains(page, "This archive's scheduled backup: a backup set up at the command line.") {
		t.Error("the card does not name a backup set up at the command line")
	}
	for _, bad := range []string{"an export backup", "repair it from the command line", `id="runBackupBtn"`, `id="schedInterval"`} {
		if strings.Contains(page, bad) {
			t.Errorf("the command-line schedule's page still has %q", bad)
		}
	}

	// Save settings alone is refused, with nothing changed (C-7).
	u := g.snapshot()
	body := g.post(`{"interval":"daily","time":"04:00"}`)
	assure.Refused(t, refusedCode(body), body, assure.Names(commandLineRefusal), assure.NoSideEffect(u.holds))

	// Remove, then How often in Settings and Save settings: Run now appears.
	if r := postAction(g.h, "/api/schedule-remove", g.csrf, "127.0.0.1:8097", "http://127.0.0.1:8097"); !strings.Contains(r.Body.String(), `"ok":true`) || g.fake.tasks[out] {
		t.Fatalf("Remove: %s; the entry left: %v", r.Body.String(), g.fake.tasks[out])
	}
	if err := schedule.RemoveDescriptor(out); err != nil { // what schedule.Remove does beside the entry
		t.Fatal(err)
	}
	if p := g.page(); !strings.Contains(p, `id="schedInterval"`) {
		t.Fatal("after Remove, Settings does not offer How often")
	}
	if body := g.post(`{"interval":"daily","time":"04:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("Save settings after Remove: %s", body)
	}
	g.recordLast()
	if p := g.page(); !strings.Contains(p, `id="runBackupBtn"`) {
		t.Error("after Remove and Save settings, Run now does not appear")
	}

	// No such path here: Linux, and a Microsoft 365 archive.
	lin := tbProfileArchive(t)
	writeCommandLineSchedule(t, lin)
	m365 := emptyArchive(t)
	addStore(t, m365, "mailbox:a@contoso.org")
	writeCommandLineSchedule(t, m365)
	for name, p := range map[string]string{"linux": newA3Rig(t, lin, "linux").page(), "microsoft 365": newA3Rig(t, m365, "darwin").page()} {
		if !shows(p, "It was set up at the command line, so change or repair it there; MailArchive Desktop can only remove it (Remove below).") ||
			strings.Contains(p, "To get Run now") {
			t.Errorf("%s: the command-line schedule's words are wrong", name)
		}
	}
}
