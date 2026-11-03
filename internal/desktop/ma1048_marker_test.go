package desktop

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
)

// The delete marker on the Desktop (docs/design-archive-delete.md §15.3, §15.4; §15.10 slice 2).
// Nothing writes the marker yet (slice 3): each test plants it by hand, as a delete begun on
// another computer or user account leaves it; this Desktop's deletions.log knows nothing of it.

// ma1048Plant writes a regular delete marker into dir.
func ma1048Plant(t *testing.T, dir string) {
	t.Helper()
	body := `{"what":"` + delmark.What + `","version":1,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z","note":"` + delmark.Note + `"}`
	if err := os.WriteFile(filepath.Join(dir, delmark.Name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1048, R36
// Archive now and Run now refuse a current archive holding the delete marker, in the marker-only
// words (DM-11); nothing runs. Positive twin first: the same current archive without it reaches
// each route's own answer.
func TestMA1048_ArchiveNowAndRunNowRefuseAFolderHoldingTheMarker(t *testing.T) {
	e := newDeleteEnv(t, withoutM365)
	useBackupFake(e.wsEnv, &backupFake{tasks: map[string]bool{}})
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	routes := []string{"/api/capture", "/api/run-backup"}
	notRunning := func() bool {
		c := e.d.cap
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.running
	}

	// Positive twin.
	for _, r := range routes {
		if _, m := e.post(r, map[string]any{"drawnFor": cur}); strings.Contains(replyErr(m), "Delete not finished") {
			t.Fatalf("positive twin: %s is refused: %v", r, m)
		}
	}

	ma1048Plant(t, cur)
	for _, r := range routes {
		code, m := e.post(r, map[string]any{"drawnFor": cur})
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(delmark.MsgOnlyMarker),
			assure.Forbid("click Delete again"), assure.NoSideEffect(notRunning))
	}
}

// covers: MA-1048, R36
// Pause, Resume, a Switch, a save that keeps the folder current, Install and Repair refuse a
// folder holding the delete marker, in the marker-only words saying what was not done (§15.4,
// DM-23's list, through ActionRefusal), with the page's reload line; nothing changes: no pause
// seal, the paused folder stays paused, the current archive and the settings are as they were,
// nothing installed. Positive twins first: each works on another archive.
func TestMA1048_TheDesktopsActionsRefuseAFolderHoldingTheMarker(t *testing.T) {
	e := newDeleteEnv(t)
	installs := 0
	e.cfg.InstallBackup = func(schedule.Spec) error { installs++; return nil }
	e.d.cfg = e.cfg
	other := e.plainArchive("Other")
	marked := e.plainArchive("Marked")
	parked := e.plainArchive("Parked")
	if err := seal.Seal(parked, "parked"); err != nil {
		t.Fatal(err)
	}

	// Positive twins.
	if _, m := e.post("/api/pause-archive", map[string]any{"path": other}); m["ok"] != true || !seal.IsSealed(other) {
		t.Fatalf("positive twin: Pause: %v", m)
	}
	if _, m := e.post("/api/resume-archive", map[string]any{"path": other}); m["ok"] != true || seal.IsSealed(other) {
		t.Fatalf("positive twin: Resume: %v", m)
	}
	if _, m := e.post("/api/settings", map[string]any{"Out": other}); m["ok"] != true || e.d.cfg.effectiveOut() != other {
		t.Fatalf("positive twin: Switch: %v", m)
	}
	if _, m := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "03:00", "drawnFor": other}); m["ok"] != true || installs != 1 {
		t.Fatalf("positive twin: Install: %v (installs %d)", m, installs)
	}

	ma1048Plant(t, marked)
	ma1048Plant(t, parked)
	words := func(done string) string { return delmark.OnlyMarkerWords(done) + reloadRowText }

	_, p := e.post("/api/pause-archive", map[string]any{"path": marked})
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names(words("it was not paused")),
		assure.NoSideEffect(func() bool { return p["ok"] == false && !seal.IsSealed(marked) }))
	_, r := e.post("/api/resume-archive", map[string]any{"path": parked})
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names(words("it was not resumed")),
		assure.NoSideEffect(func() bool { return r["ok"] == false && seal.IsSealed(parked) }))
	_, s := e.post("/api/settings", map[string]any{"Out": marked})
	assure.Refused(t, http.StatusOK, replyErr(s), assure.Code(http.StatusOK), assure.Names(words("it was not made the current archive")),
		assure.NoSideEffect(func() bool { return s["ok"] == false && e.d.cfg.effectiveOut() == other }))

	// The marked folder current (another computer's settings name it): a save that keeps it, and
	// Install and Repair.
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: marked}); err != nil {
		t.Fatal(err)
	}
	_, k := e.post("/api/settings", map[string]any{"Out": marked, "KeepRaw": true})
	assure.Refused(t, http.StatusOK, replyErr(k), assure.Code(http.StatusOK), assure.Names(words("these settings were not saved")),
		assure.NoSideEffect(func() bool { return k["ok"] == false && !LoadSettings(e.cfg.SettingsPath).KeepRaw }))
	for name, body := range map[string]map[string]any{
		"Install": {"interval": "weekly", "time": "03:00", "drawnFor": marked},
		"Repair":  {"drawnFor": marked},
	} {
		code, m := e.post("/api/schedule-install", body)
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(delmark.OnlyMarkerWords("its scheduled backup was not installed")),
			assure.NoSideEffect(func() bool { return m["ok"] == false && installs == 1 }))
		if t.Failed() {
			t.Fatalf("%s: %v", name, m)
		}
	}
}

// covers: MA-1048, R36
// A Desktop with no deletion log of its own (no -config and no home folder, so no settings folder
// to hold one) still asks the folder's delete marker (§15.3, DM-21): Run now and Archive now, and
// the Scheduled backup card's Install, refuse a current archive holding it in the marker-only
// words; nothing runs, nothing is installed. Positive twin first: another current archive is not
// refused for a delete in the same Desktop (with no home folder its install fails for that reason
// alone).
func TestMA1048_ADesktopWithNoDeletionLogStillAsksTheMarker(t *testing.T) {
	e := newDeleteEnv(t, withoutM365)
	useBackupFake(e.wsEnv, &backupFake{tasks: map[string]bool{}})
	installs := 0
	e.cfg.InstallBackup = func(schedule.Spec) error { installs++; return nil }
	other := e.plainArchive("Other")
	marked := e.plainArchive("Marked")
	e.cfg.ConfigPath = ""
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(k, "")
	}
	e.d.cfg = e.cfg
	if lp := e.d.cfg.deletionsLogPath(); lp != "" {
		t.Skipf("this platform finds a settings folder with no home folder set: %s", lp)
	}

	// Positive twin.
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: other}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"/api/schedule-install", "/api/capture", "/api/run-backup"} {
		if _, m := e.post(r, map[string]any{"interval": "weekly", "time": "03:00", "drawnFor": other}); strings.Contains(replyErr(m), "Delete not finished") {
			t.Fatalf("positive twin: %s with no deletion log: %v", r, m)
		}
	}
	installs = 0

	ma1048Plant(t, marked)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: marked}); err != nil {
		t.Fatal(err)
	}
	code, m := e.post("/api/schedule-install", map[string]any{"interval": "weekly", "time": "03:00", "drawnFor": marked})
	assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(delmark.OnlyMarkerWords("its scheduled backup was not installed")),
		assure.NoSideEffect(func() bool { return m["ok"] == false && installs == 0 }))
	notRunning := func() bool {
		c := e.d.cap
		c.mu.Lock()
		defer c.mu.Unlock()
		return !c.running
	}
	for _, r := range []string{"/api/capture", "/api/run-backup"} {
		code, m := e.post(r, map[string]any{"drawnFor": marked})
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(delmark.MsgOnlyMarker), assure.NoSideEffect(notRunning))
	}
}

// covers: MA-1049, R36
// The row and Status & health see the marker (DM-15, §15.4): a folder holding it, which this
// Desktop's deletions.log knows nothing of, has its row say "Delete not finished: a delete of this
// archive begun DATE did not finish. This computer's deletion log has no record of it (it was
// begun on another computer or user account). Click Delete… to finish it." in place of Switch and
// Pause, with Delete… offered; Status & health is RED with the marker's line; a dangling link at
// the name — which the page's stat memo would read as "not there" — is seen on the row too.
// Positive twin first: the row and health of the same archive without it.
func TestMA1049_TheRowAndStatusAndHealthSeeTheMarker(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Begun elsewhere")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}
	health := func() (string, string) {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(e.get("/api/health").Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		lines, _ := m["summary"].([]any)
		var s []string
		for _, l := range lines {
			s = append(s, l.(string))
		}
		posture, _ := m["posture"].(string)
		return posture, strings.Join(s, "\n")
	}

	// Positive twin.
	if row := rowFor(t, e.page(), dir); strings.Contains(row, "data-delete-unfinished") {
		t.Fatalf("positive twin: the row: %s", row)
	}
	if _, summary := health(); strings.Contains(summary, "did not finish") {
		t.Fatalf("positive twin: health:\n%s", summary)
	}

	ma1048Plant(t, dir)
	began := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC).Local().Format("2006-01-02 15:04 -07:00")
	want := "Delete not finished: a delete of this archive begun " + began + " did not finish. This computer's deletion log has no record of it (it was begun on another computer or user account). Click Delete… to finish it."
	row := rowFor(t, e.page(), dir)
	if !strings.Contains(row, "data-delete-unfinished") || !strings.Contains(html.UnescapeString(row), want) || !strings.Contains(row, `data-delete-ok="true"`) ||
		strings.Contains(row, "data-pause-archive") || strings.Contains(row, "click Delete again") {
		t.Fatalf("the row of a folder holding the marker: %s", row)
	}
	posture, summary := health()
	if posture != "RED" || !strings.Contains(summary, "a delete of this archive begun "+began+" did not finish, and this computer's deletion log has no record of it (it was begun on another computer or user account) — click Delete… on its row in Your archives to finish it.") {
		t.Fatalf("health of a folder holding the marker: %s\n%s", posture, summary)
	}

	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "nothing"), filepath.Join(dir, delmark.Name)); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("a link needs a privilege the run may not have")
		}
		t.Fatal(err)
	}
	row = rowFor(t, e.page(), dir)
	if !strings.Contains(row, "data-delete-unfinished") || !strings.Contains(row, `data-delete-ok="false"`) || strings.Contains(row, "data-pause-archive") {
		t.Fatalf("the row of a folder holding a dangling link at the marker's name: %s", row)
	}
	if posture, summary := health(); posture != "RED" || !strings.Contains(summary, "a delete of this archive did not finish, and this computer's deletion log has no record of it") {
		t.Fatalf("health of a dangling link at the name: %s\n%s", posture, summary)
	}
}

// covers: MA-1048, R36
// A scheduled Microsoft 365 capture (scheduledCapture, HeadlessCapture) into a folder holding the
// delete marker is refused at its start, before its log is opened in the folder or the sign-in,
// in the marker-only words with the folder, logged beside the Desktop's settings; the folder
// unchanged. Positive twin first: another folder is captured.
func TestMA1048_ScheduledCaptureRefusesAFolderHoldingTheMarker(t *testing.T) {
	fake := newFakeM365(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settingsPath := filepath.Join(dir, "desktop-settings.json")
	settingsLog := filepath.Join(dir, "desktop-capture.log")
	tok := filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := func(out string) Config {
		return Config{ConfigPath: cfgPath, SettingsPath: settingsPath, TokenCachePath: tok, Out: out,
			BaseURL: fake.URL, TokenURL: fake.URL + "/token", DeviceAuthURL: fake.URL + "/devicecode"}
	}
	if lp := cfg("").deletionsLogPath(); lp == "" || !strings.HasSuffix(lp, archdelete.LogName) {
		t.Fatalf("precondition: this Desktop's deletion log: %q", lp)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	rc := func(err error) int {
		if err != nil {
			return 1
		}
		return 0
	}
	archive := func() string {
		out := t.TempDir()
		if err := HeadlessCapture(cfg(out), io.Discard); err != nil {
			t.Fatal(err)
		}
		return out
	}
	half, other := archive(), archive()
	ma1048Plant(t, half)

	// Positive twin.
	if err := scheduledCapture(cfg(other), filepath.Join(other, "mailarchive-capture.log"), settingsLog, io.Discard); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	before := ma1044Files(t, half)
	err := scheduledCapture(cfg(half), filepath.Join(half, "mailarchive-capture.log"), settingsLog, io.Discard)
	assure.Refused(t, rc(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }))
	assure.Reached(t, strings.Contains(read(settingsLog), "FAILED: "+delmark.MsgOnlyMarker), "the refusal logged beside the settings")
	err = HeadlessCapture(cfg(half), io.Discard)
	assure.Refused(t, rc(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }))
}
