package desktop

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

const hourlyOption = `<option value="hourly"`

// covers: MA-859, R37
// Hourly is offered wherever the form edits a run-job — a local archive's create form, and a mixed
// archive's run-job where no capture is counted — and never on a capture's form (OR-2, R3C-15).
// Positive twin first: the local create form offers it.
func TestHourlyIsOfferedOnEveryRunJobFormAndNeverForACapture(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if page := g.page(); !strings.Contains(page, hourlyOption) || !strings.Contains(page, "hourly uses the minutes") {
		t.Fatal("the local create form does not offer Hourly")
	}
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	addStore(t, out, "mailbox:a@contoso.org") // Archive now added Microsoft 365 mail: mixed
	if page := g.page(); !strings.Contains(page, hourlyOption) {
		t.Error("a mixed archive's run-job form does not offer Hourly")
	}

	e := newWsEnv(t) // a Microsoft 365 archive with nothing installed: the capture's form
	page, _ := getPage(t, e.h)
	if !strings.Contains(page, `id="schedInterval"`) {
		t.Fatal("fixture: the capture's form is not drawn")
	}
	if strings.Contains(page, hourlyOption) {
		t.Error("the capture's form offers Hourly")
	}
}

// covers: MA-859, MA-860, R37
// An hourly capture is refused in section 6's words before anything is written — no install, the
// settings file and the descriptor as they were — whether posted on an install or recorded in a
// descriptor a Repair would build from. Positive twin first: a daily capture installs.
func TestAnHourlyCaptureIsRefused(t *testing.T) {
	e := newWsEnv(t)
	fake := &backupFake{tasks: map[string]bool{}}
	useBackupFake(e, fake)
	installs := seamCaptureInstall(e, fake)
	if _, body := e.post("/api/schedule-install", map[string]any{"interval": "daily", "time": "04:00"}); body["ok"] != true || len(*installs) != 1 {
		t.Fatalf("the positive twin: %v", body)
	}
	*installs = nil
	before := fileBytes(e.cfg.SettingsPath)
	desc := filepath.Join(e.out, schedule.DescriptorName)
	descBefore := fileBytes(desc)
	unchanged := func() bool {
		return len(*installs) == 0 && bytes.Equal(before, fileBytes(e.cfg.SettingsPath)) && bytes.Equal(descBefore, fileBytes(desc))
	}
	_, body := e.post("/api/schedule-install", map[string]any{"interval": "hourly", "time": "00:15"})
	msg, _ := body["error"].(string)
	rc := 0
	if body["ok"] == false {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names(hourlyCaptureRefusal), assure.NoSideEffect(unchanged))

	writeCaptureDescriptor(t, e.out, schedule.Hourly, "00:15") // a Repair would build hourly
	descBefore = fileBytes(desc)
	_, body = e.post("/api/schedule-install", map[string]any{})
	msg, _ = body["error"].(string)
	rc = 0
	if body["ok"] == false {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names(hourlyCaptureRefusal), assure.NoSideEffect(unchanged))
}

// covers: MA-859, R37
// An hourly run-job with Thunderbird sync installs (Q11 reversed: the pre-sync is no longer
// refused on an hourly schedule) and reloads as Hourly. With nothing else to warn about the
// reply carries only the sync note (owner ruling 5 of 2026-10-31, MA-936; before it, no
// note); with success emails on it warns of an email every hour,
// and with a scheduled verify on the archive it warns that backups starting while it runs are
// skipped. The Recent backups card says how many runs it keeps, about two days of hourly ones.
func TestAnHourlyRunJobWithSyncInstallsReloadsAndWarns(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	var reply map[string]any
	if err := json.Unmarshal([]byte(g.post(`{"interval":"hourly","time":"00:15","presync":true}`)), &reply); err != nil || reply["ok"] != true {
		t.Fatalf("hourly with sync: %v %v", reply, err)
	}
	// Hourly with the sync carries only the sync note (owner ruling 5 of 2026-10-31, MA-936);
	// no success emails and no verify, so nothing else.
	if reply["note"] != hourlySyncNote {
		t.Errorf("hourly with the sync and nothing else to warn about: the note %q, want only %q", reply["note"], hourlySyncNote)
	}
	last := g.installs[len(g.installs)-1]
	if last.Interval != schedule.Hourly || last.At != "00:15" || last.Args[0] != "-job" {
		t.Fatalf("not an hourly run-job: %+v", last)
	}
	if j, err := job.Read(g.jobPath); err != nil || j.ThunderbirdSync == nil {
		t.Fatalf("the hourly job does not sync Thunderbird: %+v %v", j, err)
	}
	g.recordLast()
	if page := g.page(); !strings.Contains(page, `<option value="hourly" selected>Hourly</option>`) {
		t.Error("the installed hourly schedule does not reload as Hourly")
	}

	if err := UpdateSettings(g.settings, "alerts", func(s *Settings) { s.AlertEnabled, s.AlertOnSuccess = true, true }); err != nil {
		t.Fatal(err)
	}
	verify := schedule.Spec{Name: "Weekly-Verify", Interval: schedule.Weekly, At: "05:00", Exe: "/opt/mailarchive",
		Args: []string{"verify", "-out", out, "-unattended"}, Out: out}
	if err := schedule.WriteVerifyDescriptor(out, schedule.DescribeSpec(verify)); err != nil {
		t.Fatal(err)
	}
	if _, err := schedule.ReadVerifyDescriptor(out); err != nil {
		t.Fatalf("fixture: the verify is not recorded: %v", err)
	}
	reply = nil
	if err := json.Unmarshal([]byte(g.post(`{"interval":"hourly","time":"00:20"}`)), &reply); err != nil || reply["ok"] != true {
		t.Fatalf("the hourly edit: %v %v", reply, err)
	}
	note, _ := reply["note"].(string)
	for _, want := range []string{hourlySuccessEmailsNote, hourlyVerifyNote} {
		if !strings.Contains(note, want) {
			t.Errorf("saving Hourly does not warn %q (note %q)", want, note)
		}
	}
	// A daily save warns of neither.
	reply = nil
	_ = json.Unmarshal([]byte(g.post(`{"interval":"daily","time":"02:00"}`)), &reply)
	if reply["ok"] != true || reply["note"] != nil {
		t.Errorf("a daily save carries %v", reply)
	}

	if err := state.AppendRunHistory(out, state.RunHistoryEntry{Status: state.RunOK, When: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if page := g.page(); !shows(page, "MailArchive keeps the last 50, which is about two days of hourly backups.") {
		t.Error("the Recent backups card does not say how many runs it keeps")
	}
	os.Remove(filepath.Join(out, schedule.VerifyDescriptorName))
}
