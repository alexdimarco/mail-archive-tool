package desktop

import (
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// cardOf is the part of the Overview page that is the card with this id: from the id to the
// next card.
func cardOf(page, id string) string {
	i := strings.Index(page, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	rest := page[i:]
	if j := strings.Index(rest[1:], `<div class="card`); j >= 0 {
		return rest[:j+1]
	}
	return rest
}

// withM365 points the rig's Desktop at this computer's Microsoft 365 configuration (the one
// countCapture writes), so Archive now is offered on a local archive.
func withM365(t *testing.T) func(*Config) {
	t.Helper()
	p, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return func(c *Config) { c.ConfigPath = p }
}

// covers: MA-857, R37
// Each kind's field set (3.3), Settings being the one editor (ruling 1). A local archive with
// nothing installed: How often starts at "No scheduled backup" with Hourly offered, the sync box
// is greyed until a cadence is chosen, keep .eml is read-only, and the Microsoft 365 boxes are
// hidden. A Microsoft 365 archive: the three boxes, labelled as this computer's, and a capture's
// How often (no Hourly), no .eml line and no sync. The Scheduled backup card carries no schedule
// inputs and says "Change in Settings" only where Settings has them; Install / update is gone.
func TestSettingsShowsEachKindsFields(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	page := g.page()
	settings, card := cardOf(page, "sec-settings"), cardOf(page, "sec-backup")
	for _, want := range []string{`id="schedInterval"`, `<option value="" selected>No scheduled backup</option>`, hourlyOption,
		`id="schedPresync" disabled`, "Choose how often first: the sync runs before each backup.", `id="emlReadOnly"`} {
		if !strings.Contains(settings, want) {
			t.Errorf("the local archive's Settings lacks %q", want)
		}
	}
	if !strings.Contains(settings, emlYes) && !strings.Contains(settings, emlNo) {
		t.Error("keep .eml is not shown as the archive's own value")
	}
	for _, bad := range []string{`id="keepRaw"`, `id="incDeleted"`, `id="incJunk"`, `id="archiveNowUses"`} {
		if strings.Contains(settings, bad) {
			t.Errorf("the local archive's Settings renders %s", bad)
		}
	}
	for _, bad := range []string{`id="schedInterval"`, `id="schedTime"`, `id="schedPresync"`, `id="schedInstall"`, "Install / update"} {
		if strings.Contains(card, bad) {
			t.Errorf("the Scheduled backup card renders %s", bad)
		}
	}
	if !strings.Contains(card, "<h2>Scheduled backup</h2>") || !strings.Contains(card, "Change in Settings") {
		t.Error("the card is not the Scheduled backup card pointing at Settings")
	}

	e := newWsEnv(t) // a Microsoft 365 archive with nothing installed
	mp, _ := getPage(t, e.h)
	ms, mc := cardOf(mp, "sec-settings"), cardOf(mp, "sec-backup")
	for _, want := range []string{`id="keepRaw"`, `id="incDeleted"`, `id="incJunk"`, m365BoxesLine, `id="schedInterval"`, "No scheduled backup"} {
		if !strings.Contains(ms, want) {
			t.Errorf("the Microsoft 365 archive's Settings lacks %q", want)
		}
	}
	for _, bad := range []string{hourlyOption, `id="emlReadOnly"`, `id="schedPresync"`, `id="archiveNowUses"`} {
		if strings.Contains(ms, bad) {
			t.Errorf("the Microsoft 365 archive's Settings renders %s", bad)
		}
	}
	if !strings.Contains(mc, "How often and time: <a href=\"#sec-settings\">Change in Settings</a>") || strings.Contains(mc, `id="schedRepair"`) {
		t.Error("the Microsoft 365 card with nothing installed does not point at Settings, or offers a Repair")
	}

	// Where Settings has no schedule fields (the archive's record can't be rebuilt here, on
	// Linux), the card does not say "Change in Settings".
	lin := newA3Rig(t, out, "linux")
	if c := cardOf(lin.page(), "sec-backup"); strings.Contains(c, "Change in Settings") {
		t.Error("the card points at Settings where Settings has no schedule fields")
	}
}

// covers: MA-857, R37
// The "Archive now uses" line shows on every archive where Archive now is offered and the
// boxes are hidden (R3C-16): a local archive on a computer with Microsoft 365 set up (where
// section 4's notice replaces the schedule inputs, Remove only), and a mixed archive's run-job
// with no capture counted, which keeps the Local fields and Hourly. Archive now on the local
// archive asks first that it turns it mixed (C-7). Positive twin first: with Microsoft 365 not
// set up, Archive now is off on the local archive and the line is not shown.
func TestTheArchiveNowLineShowsWhereTheBoxesAreHidden(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if p := g.page(); strings.Contains(p, `id="archiveNowUses"`) || strings.Contains(p, "data-confirm=") {
		t.Fatal("the positive twin: Archive now is off, yet its line or confirm shows")
	}
	countCapture(t, out)
	n := newA3Rig(t, out, "darwin", withM365(t))
	page := n.page()
	settings := cardOf(page, "sec-settings")
	if !shows(settings, archiveNowUsesLine) || strings.Contains(settings, `id="keepRaw"`) || strings.Contains(settings, `id="schedInterval"`) {
		t.Errorf("the local archive where a capture is counted: the line, no boxes, no schedule inputs:\n%s", settings)
	}
	if !shows(page, captureCountedLine) {
		t.Error("section 4's notice is not on the card")
	}
	if !strings.Contains(page, `id="archiveBtn" data-confirm="`+archiveNowConfirm+`"`) {
		t.Error("Archive now on a local archive where Microsoft 365 is set up does not ask first")
	}
	if !strings.Contains(dashboardJS, "if(btn.dataset.confirm&&!confirm(btn.dataset.confirm))return;") {
		t.Error("the page script does not ask Archive now's question before it starts")
	}

	// A mixed archive's run-job, no capture counted.
	isolateHome(t)
	mixed := tbProfileArchive(t)
	m := newA3Rig(t, mixed, "darwin")
	if body := m.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	m.recordLast()
	addStore(t, mixed, "mailbox:a@contoso.org")
	ms := cardOf(m.page(), "sec-settings")
	for _, want := range []string{html.EscapeString(archiveNowUsesLine), `id="emlReadOnly"`, hourlyOption, `value="weekly" selected`} {
		if !strings.Contains(ms, want) {
			t.Errorf("the mixed archive's run-job Settings lacks %q", want)
		}
	}
	for _, bad := range []string{`id="keepRaw"`, "No scheduled backup"} {
		if strings.Contains(ms, bad) {
			t.Errorf("the mixed archive's run-job Settings renders %s", bad)
		}
	}
}

// covers: MA-857, R37
// An installed run-job Settings can't edit here (Linux: the run-job form is drawn only on macOS
// and Windows) shows its cadence and sync read-only, with the wizard's real choices in the remedy
// (R3C-16); the card keeps Repair and Run now. keep .eml reads the job file's own value, and
// "Not known" with neither a job file nor a readable record. Positive twin first: on macOS the
// same run-job is editable in Settings.
func TestAReadOnlyScheduleShowsWhatIsInstalledAndHowToChangeIt(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"daily","time":"04:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	if s := cardOf(g.page(), "sec-settings"); !strings.Contains(s, `value="daily" selected`) || !strings.Contains(s, `id="schedPresync" checked`) || strings.Contains(s, "No scheduled backup") {
		t.Fatalf("the positive twin: the installed run-job is not editable in Settings:\n%s", s)
	}
	j, err := job.Read(g.jobPath)
	if err != nil {
		t.Fatal(err)
	}
	j.KeepRaw = true
	if err := job.Write(g.jobPath, j); err != nil {
		t.Fatal(err)
	}

	lin := newA3Rig(t, out, "linux")
	page := lin.page()
	s := cardOf(page, "sec-settings")
	if strings.Contains(s, `id="schedInterval"`) {
		t.Error("Settings offers schedule inputs where the run-job can't be edited")
	}
	for _, want := range []string{"How often: daily at 04:00. Sync Thunderbird first: on.", wizardRemedy, emlYes} {
		if !shows(s, want) {
			t.Errorf("the read-only Settings lacks %q", want)
		}
	}
	if c := cardOf(page, "sec-backup"); !strings.Contains(c, `id="schedRepair"`) || !strings.Contains(c, `id="runBackupBtn"`) {
		t.Error("the card lost the run-job's Repair or Run now")
	} else if strings.Contains(c, "Change in Settings") {
		t.Error("the card points at Settings, which has no schedule fields here")
	}

	// Neither a job file nor a readable record: "Not known".
	os.Remove(g.jobPath)
	os.Remove(filepath.Join(out, schedule.DescriptorName))
	if err := os.WriteFile(filepath.Join(out, state.LastRunName), []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := cardOf(lin.page(), "sec-settings"); !strings.Contains(s, "original .eml too: <b>"+emlNotKnown+"</b>") {
		t.Errorf("keep .eml is not \"Not known\":\n%s", s)
	}
}

// covers: MA-857, R37
// Save works in two steps (3.6): a new location is saved alone — every other option is greyed and
// the page reloads — and otherwise the boxes go first and the schedule only when it changed; "No
// scheduled backup" sends no schedule request. The checker reads the page script; it is run first
// on the script as built (the positive twin), then on scripts with each part taken out.
func TestSaveSendsTheLocationAloneAndTheScheduleOnlyWhenChanged(t *testing.T) {
	check := func(js string) []string {
		var missing []string
		for _, want := range []string{
			"if(!schedFields||!iv||!iv.value)return false;",                                                 // "No scheduled backup" sends nothing
			"if(d.mustPost==='1'||!d.installedInterval)return true;",                                        // nothing installed, or the entry gone
			"document.querySelectorAll('#sec-settings [data-kind-field]').forEach(f=>{f.disabled=moved;});", // greyed while moved
			"if(!moved)settingBoxes.forEach(",                                                               // a move sends the location alone
			"setTimeout(()=>location.reload(),600);return true;}",                                           // and reloads
			"if(schedChanged()){", // the schedule only when changed
			"'The backup schedule was not changed: '+(r.reason||r.error", // the combined reply quotes the reason
		} {
			if !strings.Contains(js, want) {
				missing = append(missing, want)
			}
		}
		return missing
	}
	if m := check(dashboardJS); len(m) > 0 {
		t.Fatalf("the page script lacks %q", m)
	}
	for _, cut := range []string{"if(!schedFields||!iv||!iv.value)return false;", "if(!moved)settingBoxes.forEach(", "if(schedChanged()){"} {
		if len(check(strings.Replace(dashboardJS, cut, "", 1))) == 0 {
			t.Errorf("the checker does not notice %q taken out", cut)
		}
	}
	// The card's Repair and Remove send no schedule fields: a Repair is an empty body that keeps
	// the installed cadence and sync (C-2, C-5), whatever Settings shows.
	i := strings.Index(dashboardJS, "function cardPost(")
	if i < 0 {
		t.Fatal("cardPost not found in the page script")
	}
	cp := dashboardJS[i:]
	cp = cp[:strings.Index(cp, "\n}\n")]
	if !strings.Contains(cp, "body:JSON.stringify({drawnFor:drawnFor})") || strings.Contains(cp, "sched") && strings.Contains(cp, "Interval") {
		t.Errorf("the card's Repair or Remove sends schedule fields:\n%s", cp)
	}
	// Every option Settings draws is greyed with the location; the note says why.
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	s := cardOf(g.page(), "sec-settings")
	for _, f := range []string{`id="schedInterval" data-kind-field`, `id="schedTime" type="time" value="03:00" data-kind-field`, `id="settingsMoveNote" hidden>` + html.EscapeString(moveFirstLine)} {
		if !strings.Contains(s, f) {
			t.Errorf("Settings lacks %q", f)
		}
	}
	e := newWsEnv(t)
	mp, _ := getPage(t, e.h)
	for _, id := range []string{"keepRaw", "incDeleted", "incJunk"} {
		if !strings.Contains(cardOf(mp, "sec-settings"), `id="`+id+`"`) || !strings.Contains(mp, `id="`+id+`"`) {
			t.Fatalf("fixture: %s missing", id)
		}
		if i := strings.Index(mp, `id="`+id+`"`); !strings.Contains(mp[i:i+80], "data-kind-field") {
			t.Errorf("%s is not greyed with the location", id)
		}
	}
}

// covers: MA-857, R37
// Sync alone changed (3.6 step 5, C-18): only the job file is written and the scheduler entry is
// left alone. Positive twin first: a changed cadence goes to the scheduler.
func TestASyncAloneSaveWritesOnlyTheJobFile(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	if body := g.post(`{"interval":"daily","time":"03:00"}`); !strings.Contains(body, `"ok":true`) || len(g.installs) != 2 {
		t.Fatalf("the positive twin, a cadence change: %s (%d installs)", body, len(g.installs))
	}
	g.recordLast()
	before := len(g.installs)
	if body := g.post(`{"interval":"daily","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("sync alone: %s", body)
	}
	if len(g.installs) != before {
		t.Error("a sync-alone save reloaded the scheduler entry")
	}
	if j, err := job.Read(g.jobPath); err != nil || j.ThunderbirdSync == nil {
		t.Errorf("the sync was not written to the job file: %v", err)
	}
}

// covers: MA-857, MA-867, R37
// A Microsoft 365 archive whose capture is installed and counted keeps its Repair on the card
// (C-2), and its How often in Settings starts at the installed cadence with "No scheduled backup"
// no longer offered. Positive twin first: with nothing installed, no Repair.
func TestACountedCaptureKeepsItsRepairOnTheCard(t *testing.T) {
	isolateHome(t)
	out := archiveAt(t, t.TempDir(), "M365", map[string]string{"mailbox:u@contoso.com": "b"})
	g := newA3Rig(t, out, "darwin")
	countCapture(t, out)
	if c := cardOf(g.page(), "sec-backup"); strings.Contains(c, `id="schedRepair"`) {
		t.Fatal("the positive twin: a Repair with nothing installed")
	}
	writeCaptureDescriptor(t, out, schedule.Daily, "06:30")
	g.fake.tasks[out] = true
	page := g.page()
	if c := cardOf(page, "sec-backup"); !strings.Contains(c, `id="schedRepair"`) || strings.Contains(c, `id="runBackupBtn"`) {
		t.Errorf("the counted capture's card lacks its Repair:\n%s", c)
	}
	s := cardOf(page, "sec-settings")
	if !strings.Contains(s, `value="daily" selected`) || !strings.Contains(s, `value="06:30"`) || strings.Contains(s, "No scheduled backup") {
		t.Errorf("Settings does not start at the installed capture's cadence:\n%s", s)
	}
}

// covers: MA-860, R37
// An auto-paused archive (its entry taken out, its descriptor kept, never sealed) says backups
// stopped and since when, says what Save does, and its Save re-installs with the cadence
// unchanged (C-11, 3.4). Positive twin first: with the entry installed, Save is not forced.
func TestAnAutoPausedArchivesSaveReinstalls(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	if s := cardOf(g.page(), "sec-settings"); !strings.Contains(s, `data-must-post=""`) || strings.Contains(s, "Backups stopped") {
		t.Fatalf("the positive twin: an installed schedule forces a Save:\n%s", s)
	}
	t0 := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 5, FirstUnreachable: t0, LastUnreachable: t0.Add(31 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	delete(g.fake.tasks, out) // auto-pause took the entry out, the descriptor kept
	s := cardOf(g.page(), "sec-settings")
	for _, want := range []string{`data-must-post="1"`, "Backups stopped: this archive&#39;s source has been unreachable since Sep 1, 2026.", autoPausedSaveLine, `value="weekly" selected`} {
		if !strings.Contains(s, want) {
			t.Errorf("the auto-paused Settings lacks %q", want)
		}
	}
	before := len(g.installs)
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) || len(g.installs) != before+1 || !g.fake.tasks[out] {
		t.Errorf("Save with the cadence unchanged did not re-install: %s", body)
	}
}

// covers: MA-860, R37
// A combined reply never says "saved" and "Nothing was changed" together (3.6 step 6, R3C-16):
// every refusal of api/schedule-install carries its reason without "Nothing was changed.", which
// the Settings card quotes after what it saved. Positive twin first: an install that succeeds
// carries no reason.
func TestEveryScheduleRefusalCarriesAReasonToQuote(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	var ok map[string]any
	if err := json.Unmarshal([]byte(g.post(`{"interval":"weekly","time":"03:00"}`)), &ok); err != nil || ok["ok"] != true || ok["reason"] != nil {
		t.Fatalf("the positive twin: %v %v", ok, err)
	}
	g.recordLast()
	lin := newA3Rig(t, out, "linux")
	stale := `{"interval":"daily","time":"02:00","drawnFor":"/somewhere/else"}`
	for _, c := range []struct {
		rig  *a3Rig
		body string
		want string
	}{
		{lin, `{"interval":"weekly","time":"03:00","presync":true}`, "Thunderbird sync works on macOS and Windows only."},
		{g, stale, "This page is out of date; reload it."},
		{g, `{"interval":"fortnightly","time":"03:00"}`, "unknown schedule interval fortnightly"},
	} {
		u := c.rig.snapshot()
		body := c.rig.post(c.body)
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatal(err)
		}
		reason, _ := m["reason"].(string)
		assure.Refused(t, refusedCode(body), body, assure.Names(c.want), assure.NoSideEffect(u.holds))
		if reason != c.want && !strings.HasPrefix(reason, c.want) {
			t.Errorf("%s: reason %q, want %q", c.body, reason, c.want)
		}
		if strings.Contains(strings.ToLower(reason), "nothing was changed") {
			t.Errorf("%s: the reason quoted after \"Settings saved.\" says nothing was changed: %q", c.body, reason)
		}
	}
	for in, want := range map[string]string{
		"This archive is paused; Resume it first. Nothing was changed.":             "This archive is paused; Resume it first.",
		"Thunderbird sync works on macOS and Windows only, so nothing was changed.": "Thunderbird sync works on macOS and Windows only.",
		"The backup could not be set up (disk full), so nothing was scheduled.":     "The backup could not be set up (disk full), so nothing was scheduled.",
		unreadableJobText("/j/x.json", "a newer version of MailArchive wrote it"):   "This archive's backup settings file /j/x.json can't be read (a newer version of MailArchive wrote it). Re-create the backup in the MailArchive wizard, or update MailArchive if a newer version made it.",
	} {
		if got := scheduleReason(in); got != want {
			t.Errorf("scheduleReason(%q) = %q, want %q", in, got, want)
		}
	}
}

// covers: MA-861, R37, X16
// Wherever the sync box renders, presync.SyncNotice is visible text beside it (never only a
// tooltip), and on macOS the Automation note from the one constant the wizard also uses
// (presync.MacAutomationNote); on Windows the Mac note is not shown. Positive twin first: the
// macOS create form.
func TestTheSyncBoxCarriesTheNoticeAndTheMacNote(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	visible := func(page, text string) bool {
		s := cardOf(page, "sec-settings")
		text = html.EscapeString(text)
		return strings.Contains(s, `id="schedPresync"`) && strings.Contains(s, ">"+text+"</p>") && !strings.Contains(s, `title="`+text)
	}
	mac := newA3Rig(t, out, "darwin")
	page := mac.page()
	if !visible(page, presync.SyncNotice) || !visible(page, presync.MacAutomationNote) {
		t.Fatal("the positive twin: the macOS create form lacks the notice or the Mac note")
	}
	win := newA3Rig(t, out, "windows")
	wp := win.page()
	if !visible(wp, presync.SyncNotice) {
		t.Error("the Windows create form lacks the sync notice")
	}
	if strings.Contains(wp, presync.MacAutomationNote) {
		t.Error("the Windows form shows the Mac note")
	}
	// The edit form too.
	if body := mac.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	mac.recordLast()
	if p := mac.page(); !visible(p, presync.SyncNotice) || !visible(p, presync.MacAutomationNote) {
		t.Error("the edit form lacks the notice or the Mac note")
	}
}
