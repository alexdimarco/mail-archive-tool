package desktop

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/wizard"
)

// covers: MA-1058, R36
// The scheduled Microsoft 365 capture (HeadlessCapture) refused for a delete this computer never
// began (only the folder's marker knows it; docs/design-archive-delete.md §15.10 slice 6, the
// owner's ruling Q5) counts toward the same auto-pause as the local job: once the refused runs span
// 30 days, the schedule this Desktop installed the capture under (named for the -out it passes) is
// stopped, the auto-pause email goes with the lead's words, the capture log (beside the settings)
// says so, and the count ends; the folder is unchanged. A delete this Desktop's deletion log knows
// never counts. Positive twin first: spanning 29 days the refused run counts and nothing is stopped.
func TestMA1058_TheScheduledCaptureAutoPausesForADeleteBegunElsewhere(t *testing.T) {
	fake := newFakeM365(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settingsPath := filepath.Join(dir, "desktop-settings.json")
	logPath := filepath.Join(dir, archdelete.LogName) // this Desktop's (-config) deletion log
	tok := filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(captureAlerts), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := func(out string) Config {
		return Config{ConfigPath: cfgPath, SettingsPath: settingsPath, TokenCachePath: tok, Out: out,
			BaseURL: fake.URL, TokenURL: fake.URL + "/token", DeviceAuthURL: fake.URL + "/devicecode"}
	}
	if got := cfg("").deletionsLogPath(); got != logPath {
		t.Fatalf("precondition: this Desktop's deletion log is %s, got %s", logPath, got)
	}
	out := t.TempDir()
	if err := HeadlessCapture(cfg(out), io.Discard); err != nil {
		t.Fatal(err)
	}
	ma1048Plant(t, out)
	name := schedule.DefaultNameFor(out)
	installed := map[string]bool{name: true}
	var removed []string
	t.Cleanup(wizard.SetSchedulerForTest(func(n string) schedule.State {
		if installed[n] {
			return schedule.Installed
		}
		return schedule.NotInstalled
	}, func(s schedule.Spec) error { removed = append(removed, s.Name); delete(installed, s.Name); return nil }))
	sent := recordSends(t)
	countPath := backupalert.DeleteRefusedPath(settingsPath, backupalert.KeyFor(out))
	seed := func(daysAgo int) {
		then := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)
		b, _ := json.Marshal(state.SourceStreak{Version: state.SourceStreakVersion, ConsecutiveFails: 1, FirstUnreachable: then, LastUnreachable: then})
		if err := os.WriteFile(countPath, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	count := func() (state.SourceStreak, bool) {
		b, err := os.ReadFile(countPath)
		var s state.SourceStreak
		if err != nil || json.Unmarshal(b, &s) != nil {
			return s, false
		}
		return s, true
	}
	pausedEmail := func() *alert.Event {
		for i := range *sent {
			if (*sent)[i].Result == alert.ResultPaused {
				return &(*sent)[i]
			}
		}
		return nil
	}
	rc := func(err error) int {
		if err != nil {
			return 1
		}
		return 0
	}
	before := ma1044Files(t, out)

	// Positive twin: 29 days, counted, nothing stopped.
	seed(29)
	err := HeadlessCapture(cfg(out), io.Discard)
	assure.Refused(t, rc(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+out+")"),
		assure.NoSideEffect(func() bool { return len(removed) == 0 && ma1044SameFiles(before, ma1044Files(t, out)) }))
	s, ok := count()
	assure.Reached(t, ok && s.ConsecutiveFails == 2 && !s.AutoPauseDue(), "29 days: the refused capture counted, not due")
	if pausedEmail() != nil {
		t.Fatal("29 days: an auto-pause email went")
	}

	// 31 days: the capture's schedule is stopped.
	seed(31)
	var logged bytes.Buffer
	err = HeadlessCapture(cfg(out), &logged)
	assure.Refused(t, rc(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+out+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, out)) }))
	if len(removed) != 1 || removed[0] != name {
		t.Fatalf("31 days: the capture's schedule was not stopped: removed %v", removed)
	}
	ev := assure.Reached(t, pausedEmail(), "the auto-pause email")
	if ev.Error != wizard.AutoPauseDeleteWhy || ev.Archive != out {
		t.Errorf("the auto-pause email's reason:\n got %q\nwant %q", ev.Error, wizard.AutoPauseDeleteWhy)
	}
	assure.Reached(t, strings.Contains(logged.String(), "auto-paused the scheduled backup: "+wizard.AutoPauseDeleteWhy), "the pause told in the capture log")
	if _, ok := count(); ok {
		t.Error("the auto-pause left the count behind")
	}

	// The log knows the delete: never counts.
	installed[name] = true
	removed, *sent = nil, nil
	ma1044Started(t, logPath, out)
	seed(31)
	if err := HeadlessCapture(cfg(out), io.Discard); err == nil {
		t.Fatal("the capture into a half-deleted folder was not refused")
	}
	if _, ok := count(); ok || len(removed) != 0 || pausedEmail() != nil {
		t.Fatalf("a delete this Desktop's log knows counted: removed %v, sent %v", removed, *sent)
	}
}
