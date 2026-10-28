package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// localRig is a local archive's Desktop with the scheduler seam recording every install
// (no real schedule is ever installed) and a settings file of its own.
type localRig struct {
	t        *testing.T
	out      string
	h        http.Handler
	csrf     string
	settings string
	installs []schedule.Spec
	jobPath  string
}

func newLocalRig(t *testing.T, out, goos string) *localRig {
	t.Helper()
	g := &localRig{t: t, out: out, settings: filepath.Join(t.TempDir(), "s.json")}
	jp, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	g.jobPath = jp
	t.Cleanup(func() { os.Remove(jp) })
	cfg := Config{Out: out, GOOS: goos, Exe: "/opt/mailarchive", SettingsPath: g.settings,
		InstallBackup: func(s schedule.Spec) error { g.installs = append(g.installs, s); return nil }}
	g.h = DashboardHandler(cfg)
	_, g.csrf = getPage(t, g.h)
	return g
}

// post sends a schedule install as the card does and returns the reply's body.
func (g *localRig) post(body string) string {
	g.t.Helper()
	return postJSON(g.h, "/api/schedule-install", g.csrf, body).Body.String()
}

// fileBytes is the file at p, or nil when there is none.
func fileBytes(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return b
}

// refusedCode is the typed refusal code of a Desktop reply: 2 for ok:false, 0 otherwise.
func refusedCode(body string) int {
	if strings.Contains(body, `"ok":false`) {
		return 2
	}
	return 0
}

// said is what a Desktop reply says, as the page shows it: the reply's error and note decoded
// from JSON, so a Windows path in it reads with single backslashes, as the refusal names it.
// A body that is not a JSON reply is returned as it is.
func said(body string) string {
	var r struct{ Error, Note string }
	if err := json.Unmarshal([]byte(body), &r); err != nil || r.Error+r.Note == "" {
		return body
	}
	return strings.TrimSpace(r.Error + " " + r.Note)
}

// archivedStore is the one local store the archive at out was made from.
func archivedStore(t *testing.T, out string) string {
	t.Helper()
	j, ok := reconstructLocalJob(out)
	if !ok || len(j.Inputs) != 1 {
		t.Fatalf("fixture: want one reconstructable input, got %v (%v)", j.Inputs, ok)
	}
	return j.Inputs[0]
}

// covers: MA-858, R37
// An edit changes only what was changed (design-desktop-settings-card 3.5, C-4): on a job the
// wizard made (incremental, a start date, copy first, keep .eml, a followed link) whose
// archive's LAST run was full with no -raw, switching Thunderbird sync on and then off leaves
// the job file byte-equal to the wizard's except thunderbird_sync, and the cadence goes to the
// scheduler entry only. Before, every edit rebuilt the whole file from the last-run record:
// mode became full, the start date, copy first and keep .eml were lost.
func TestEditChangesOnlyThunderbirdSync(t *testing.T) {
	out := tbProfileArchive(t)
	store := archivedStore(t, out)
	rec := fmt.Sprintf(`{"version":1,"status":"ok","job":["-out",%q,"-mode","full","-input",%q]}`, out, store)
	if err := os.WriteFile(filepath.Join(out, state.LastRunName), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	g := newLocalRig(t, out, "darwin")
	wiz := job.Job{Name: schedule.DefaultNameFor(out), Inputs: []string{store}, Out: out, Mode: "incremental",
		Since: "2024-01-01", CopyFirst: true, KeepRaw: true, FollowLinks: []string{store}}
	if err := job.Write(g.jobPath, wiz); err != nil {
		t.Fatal(err)
	}
	original := assure.Reached(t, fileBytes(g.jobPath), "the wizard's job file")
	withSync := wiz
	withSync.ThunderbirdSync = &job.ThunderbirdSync{}
	expectPath := filepath.Join(t.TempDir(), "expect.json")
	if err := job.Write(expectPath, withSync); err != nil {
		t.Fatal(err)
	}
	expectOn := fileBytes(expectPath)

	if body := g.post(`{"interval":"daily","time":"05:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("switching sync on: %s", body)
	}
	if got := fileBytes(g.jobPath); !bytes.Equal(got, expectOn) {
		t.Fatalf("after switching sync on the job file is not the wizard's plus thunderbird_sync:\n got %s\nwant %s", got, expectOn)
	}
	if n := len(g.installs); n != 1 || g.installs[0].Interval != schedule.Daily || g.installs[0].At != "05:00" {
		t.Fatalf("the cadence was not handed to the scheduler: %+v", g.installs)
	}
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("switching sync off: %s", body)
	}
	if got := fileBytes(g.jobPath); !bytes.Equal(got, original) {
		t.Fatalf("after switching sync off the job file is not the wizard's:\n got %s\nwant %s", got, original)
	}
	// The cadence alone: the job file is left exactly as it is.
	if body := g.post(`{"interval":"daily","time":"01:15","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("changing the cadence alone: %s", body)
	}
	if got := fileBytes(g.jobPath); !bytes.Equal(got, original) {
		t.Errorf("a cadence-only edit changed the job file:\n got %s\nwant %s", got, original)
	}
}

// covers: MA-858, R37
// One source per value (3.5): a local install writes the cadence to the scheduler and the
// descriptor only; the settings file's interval and time are the capture's record and stay
// as they were. Before, a local install wrote them (localschedule.go:204), and a capture
// then read them.
func TestLocalInstallLeavesTheSettingsCadenceAlone(t *testing.T) {
	g := newLocalRig(t, tbProfileArchive(t), "darwin")
	if err := SaveSettings(g.settings, Settings{Interval: "weekly", WeeklyTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	if body := g.post(`{"interval":"daily","time":"05:40"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("a local create: %s", body)
	}
	if len(g.installs) != 1 || g.installs[0].Interval != schedule.Daily {
		t.Fatalf("the install was not handed the posted cadence: %+v", g.installs)
	}
	if s := LoadSettings(g.settings); s.Interval != "weekly" || s.WeeklyTime != "03:00" {
		t.Errorf("a local install wrote the settings file's cadence: %q at %q, want weekly at 03:00", s.Interval, s.WeeklyTime)
	}
}

// covers: MA-858, R37
// Sync lives only in the job file (3.5): the settings file has no field for it.
func TestTheSettingsFileHasNoSyncField(t *testing.T) {
	ty := reflect.TypeOf(Settings{})
	checked := 0
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		name := strings.ToLower(f.Name + " " + f.Tag.Get("json"))
		checked++
		for _, bad := range []string{"sync", "presync", "thunderbird"} {
			if strings.Contains(name, bad) {
				t.Errorf("Settings field %s (%s) holds the Thunderbird sync: it must live only in the job file", f.Name, f.Tag.Get("json"))
			}
		}
	}
	assure.Reached(t, checked, "Settings fields checked")
}

// covers: MA-858, R37
// A settings save leaves a box the request omits as it is (3.6 step 3): only the boxes the
// form sends change. Before, an omitted box was saved as off.
func TestSettingsSaveLeavesOmittedBoxesUnchanged(t *testing.T) {
	e := newWsEnv(t)
	a := t.TempDir()
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: a, KeepRaw: true, IncludeDeleted: true, IncludeJunk: true}); err != nil {
		t.Fatal(err)
	}
	// Positive twin: a box that is sent changes.
	if _, body := e.post("/api/settings", map[string]any{"out": a, "includeJunk": false}); body["ok"] != true {
		t.Fatalf("a save with one box: %v", body)
	}
	s := LoadSettings(e.cfg.SettingsPath)
	if s.IncludeJunk {
		t.Error("a box that was sent as off was not saved")
	}
	if !s.KeepRaw || !s.IncludeDeleted {
		t.Errorf("boxes the save omitted changed: keepRaw %v, includeDeleted %v, want both still on", s.KeepRaw, s.IncludeDeleted)
	}
}

// covers: MA-858, R37
// A create rebuilds from the archive's own record and has no start-date refusal (R3C-16:
// the record never carries a start date, so such a refusal could never fire): a local
// create with no job file installs and writes the rebuilt job.
func TestALocalCreateHasNoStartDateRefusal(t *testing.T) {
	g := newLocalRig(t, tbProfileArchive(t), "darwin")
	body := g.post(`{"interval":"weekly","time":"03:00"}`)
	if !strings.Contains(body, `"ok":true`) || strings.Contains(strings.ToLower(body), "start date") {
		t.Fatalf("a local create: %s", body)
	}
	j, err := job.Read(g.jobPath)
	if err != nil {
		t.Fatalf("the create wrote no readable job file: %v", err)
	}
	if j.Out != g.out || j.Since != "" {
		t.Errorf("the created job: out %q since %q, want this archive and no start date", j.Out, j.Since)
	}
}
