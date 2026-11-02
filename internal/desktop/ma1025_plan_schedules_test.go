package desktop

import (
	"reflect"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-1025, R36
// The Desktop's plan panel lists only the scheduled backups step 2 would stop, or says none (the
// 1.5.3 friction review's B1): the plan route asks the Desktop's scheduler (Deps.Scheduler) and
// its reply's scheduleLines are the panel's words, which the page script draws in place of the
// derived names it listed before. Positive twin first: an installed backup is listed.
func TestThePlanPanelListsOnlyInstalledBackups(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Installed")
	e.sched.installed[schedule.DefaultNameFor(dir)] = true
	p := e.plan(dir)
	lines, _ := p["scheduleLines"].([]any)
	if p["ok"] != true || len(lines) != 1 || lines[0] != "Scheduled backups it stops: the backup "+schedule.DefaultNameFor(dir)+"." {
		t.Fatalf("the positive twin: %v", p["scheduleLines"])
	}

	none := e.plainArchive("Nothing Installed")
	p = e.plan(none)
	if got := p["scheduleLines"]; p["ok"] != true || !reflect.DeepEqual(got, []any{"Scheduled backups it stops: none."}) {
		t.Fatalf("nothing installed: %v", got)
	}
	if s, _ := p["schedules"].([]any); len(s) != 0 {
		t.Fatalf("nothing installed, yet the reply lists %v", s)
	}
}
