package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
)

// captureRig is a Microsoft 365 archive on a computer where Microsoft 365 is set up (its capture
// counted), with a stand-in scheduler.
func captureRig(t *testing.T) *a3Rig {
	t.Helper()
	isolateHome(t)
	cfgPath, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	out := archiveAt(t, t.TempDir(), "M365", map[string]string{"mailbox:u@contoso.com": "b"})
	g := newA3Rig(t, out, "darwin", func(c *Config) { c.ConfigPath = cfgPath })
	countCapture(t, out)
	return g
}

// covers: MA-876, R37
// A capture Repair never guesses a cadence it can't read (the part A pass): with an installed
// entry whose recorded cadence can't be read — the earlier location's record on a drive that is
// unplugged (Save and keep the backup), or this folder's record gone — the Repair is refused in
// plain words, naming where the record is, before anything is written: no install, no removal,
// the settings file and the archive's record unchanged. Positive twin first: with the record
// readable, the Repair keeps its cadence.
func TestACaptureRepairNeverResetsACadenceItCantRead(t *testing.T) {
	g := captureRig(t)
	writeCaptureDescriptor(t, g.out, schedule.Daily, "06:30")
	g.fake.tasks[g.out] = true
	if body := g.post(`{}`); !strings.Contains(body, `"ok":true`) || len(g.installs) != 1 || g.installs[0].Interval != schedule.Daily || g.installs[0].At != "06:30" {
		t.Fatalf("the positive twin: %s %+v", body, g.installs)
	}

	// The earlier location's drive is unplugged: its entry is installed, its record unreadable, and
	// this folder holds no capture record either (with one, it is the record that moved with the
	// folder, and the Repair uses it: TestACaptureRepairAfterAMoveUsesTheRecordThatMoved).
	if err := schedule.RemoveDescriptor(g.out); err != nil {
		t.Fatal(err)
	}
	away := filepath.Join(t.TempDir(), "unplugged", "Mail")
	if err := SaveSettings(g.settings, Settings{Out: g.out, BackupFor: away, Interval: "daily", WeeklyTime: "06:00"}); err != nil {
		t.Fatal(err)
	}
	g.fake.tasks = map[string]bool{away: true}
	g.fake.removed = nil
	u := g.snapshot()
	body := g.post(`{}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("can't read how often the installed backup runs", away, "Nothing was changed."),
		assure.NoSideEffect(func() bool { return u.holds() && len(g.fake.removed) == 0 }))

	// This folder's own entry, its record gone.
	if err := SaveSettings(g.settings, Settings{Out: g.out}); err != nil {
		t.Fatal(err)
	}
	g.fake.tasks = map[string]bool{g.out: true}
	u = g.snapshot()
	body = g.post(`{}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("can't read how often the installed backup runs", g.out),
		assure.NoSideEffect(func() bool { return u.holds() && len(g.fake.removed) == 0 }))
}

// covers: MA-876, R37
// A capture Repair after the archive folder moved (Save and keep the backup) keeps its cadence (the
// part A re-verification, 2026-10-28): the entry is still named for the earlier folder, which is
// gone, and the record that moved with the folder is a capture's, as Settings shows it. The Repair
// reads that record, installs under the current folder's name at its cadence, and removes the
// earlier folder's entry. Positive twin first, the refusal it replaces: with no record here either,
// the Repair is refused (TestACaptureRepairNeverResetsACadenceItCantRead) — here it goes through.
func TestACaptureRepairAfterAMoveUsesTheRecordThatMoved(t *testing.T) {
	g := captureRig(t)
	away := filepath.Join(t.TempDir(), "moved-away", "Mail")
	if err := SaveSettings(g.settings, Settings{Out: g.out, BackupFor: away}); err != nil {
		t.Fatal(err)
	}
	g.fake.tasks = map[string]bool{away: true}
	u := g.snapshot()
	body := g.post(`{}`)
	assure.Refused(t, refusedCode(body), body,
		assure.Names("can't read how often the installed backup runs", away),
		assure.NoSideEffect(func() bool { return u.holds() && len(g.fake.removed) == 0 }))

	writeCaptureDescriptor(t, g.out, schedule.Daily, "05:00")
	body = g.post(`{}`)
	if !strings.Contains(body, `"ok":true`) || len(g.installs) != 1 {
		t.Fatalf("the Repair after a move was refused: %s", body)
	}
	if in := g.installs[0]; !samePath(in.Out, g.out) || in.Interval != schedule.Daily || in.At != "05:00" {
		t.Errorf("the Repair installed %+v; want the current folder at daily 05:00", in)
	}
	if len(g.fake.removed) != 1 || g.fake.removed[0] != away || g.fake.tasks[away] {
		t.Errorf("the earlier folder's entry was not removed: removed %v, tasks %v", g.fake.removed, g.fake.tasks)
	}
}

// covers: MA-876, R37
// Settings never preselects a cadence it can't read (the part A pass): a capture installed with
// its recorded cadence unreadable draws How often as "As installed (can't be read right now)",
// selected, with no other choice selected and no installed interval for the script to compare —
// a value the script's schedChanged never posts, so a Save of the boxes alone leaves the schedule
// as it is (and a crafted post with no cadence is refused by the Repair's rule above). Positive
// twin first: with the record readable, How often starts at the installed cadence.
func TestSettingsDrawsAnUnreadableCaptureCadenceAsUnknown(t *testing.T) {
	g := captureRig(t)
	writeCaptureDescriptor(t, g.out, schedule.Weekly, "05:00")
	g.fake.tasks[g.out] = true
	if s := cardOf(g.page(), "sec-settings"); !strings.Contains(s, `value="weekly" selected`) || strings.Contains(s, "As installed") {
		t.Fatalf("the positive twin: Settings does not start at the installed cadence:\n%s", s)
	}

	if err := schedule.RemoveDescriptor(g.out); err != nil {
		t.Fatal(err)
	}
	s := cardOf(g.page(), "sec-settings")
	if !strings.Contains(s, `<option value="" selected>As installed (can`) {
		t.Fatalf("an unreadable cadence is not drawn as unknown:\n%s", s)
	}
	if sel := s[strings.Index(s, `id="schedInterval"`):]; strings.Count(sel[:strings.Index(sel, "</select>")], " selected") != 1 || !strings.Contains(s, `data-installed-interval=""`) {
		t.Errorf("another cadence is preselected, or an installed interval is claimed:\n%s", s)
	}
	if !strings.Contains(dashboardJS, "if(!schedFields||!iv||!iv.value)return false;") {
		t.Error("the script's schedChanged no longer leaves an empty How often unposted")
	}
}
