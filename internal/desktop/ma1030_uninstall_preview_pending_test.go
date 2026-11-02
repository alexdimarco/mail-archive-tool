package desktop

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1030, R36
// The Desktop's Uninstall preview with a delete not finished shows the plan, as the command
// line's dry run does (amended 2026-11-02, the uninstall-docs group's adversarial pass, finding
// 2): the reply is the plan, ok, with refuseUntil, "Uninstall will refuse until the delete of
// ROW is finished …", never the real run's "removed nothing". Remove itself is still refused,
// nothing removed, in the real run's words. Both name the finishing command with this Desktop's
// -config when it was started with one (finding 8), and with none when it was not. Positive twin
// first: with no delete waiting, the plan has no refuseUntil.
func TestTheUninstallPreviewWithADeleteNotFinishedShowsThePlan(t *testing.T) {
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	gone := filepath.Join(t.TempDir(), "Old mail")
	pending := true
	realRuns := 0
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		logPath := filepath.Join(configDir, "deletions.log")
		rep := &uninstall.Report{ConfigFiles: []string{filepath.Join(configDir, "desktop-settings.json"), logPath}, DeletionsLog: logPath}
		if !dryRun {
			realRuns++
		}
		if !pending {
			return rep, nil
		}
		if !dryRun {
			return nil, &uninstall.DeleteNotFinishedError{Log: logPath, Folders: []string{gone}}
		}
		return rep, &uninstall.DeleteNotFinishedError{Log: logPath, Folders: []string{gone}}
	}
	reply := func(t *testing.T, d *dashboard, body string) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(postUninstall(t, d, body).Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	cfgPath := filepath.Join(t.TempDir(), "graph-config.json")
	d := newDashboard(Config{ConfigPath: cfgPath})

	// Positive twin: nothing waiting.
	pending = false
	if m := reply(t, d, `{"confirm":false}`); m["ok"] != true || m["refuseUntil"] != nil || m["deletionsLog"] == nil {
		t.Fatalf("positive twin: the plan with no delete waiting: %v", m)
	}

	pending = true
	withConfig := "mailarchive delete-archive -config " + encmark.CommandPath(cfgPath) + " -out " + encmark.CommandPath(gone)
	m := reply(t, d, `{"confirm":false}`)
	until, _ := m["refuseUntil"].(string)
	code := 0
	if m["ok"] == true && until != "" {
		code = 1
	}
	assure.Refused(t, code, until, assure.Code(1),
		assure.Names("Uninstall will refuse until the delete of "+gone+" is finished", withConfig),
		assure.Forbid("removed nothing", "FOLDER"),
		assure.NoSideEffect(func() bool {
			files, _ := m["configFiles"].([]any)
			return len(files) == 2 && m["deletionsLog"] != nil && m["done"] == false && realRuns == 0
		}))

	// Remove: refused in the real run's words, nothing run.
	m = reply(t, d, `{"confirm":true,"shown":{"configFiles":[]}}`)
	msg, _ := m["error"].(string)
	code = 0
	if m["ok"] == false {
		code = 1
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("removed nothing", withConfig),
		assure.NoSideEffect(func() bool { return realRuns == 0 && d.running.blockedWhy() == "" }))

	// A Desktop started without -config: the usual command, no -config.
	def, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	m = reply(t, newDashboard(Config{ConfigPath: def}), `{"confirm":false}`)
	until, _ = m["refuseUntil"].(string)
	if want := "mailarchive delete-archive -out " + encmark.CommandPath(gone); !strings.Contains(until, want) || strings.Contains(until, "-config") {
		t.Errorf("a Desktop started without -config names %q and no -config: %q", want, until)
	}
}

// covers: MA-1030, R36, S41
// The page shows that plan, then why Remove waits, and disables Remove; a click on it sends
// nothing; a later plan with nothing waiting hides the note and gives Remove back. Positive twin
// first: a plan with nothing waiting leaves Remove enabled and shows no note. Run on the page
// script under Node.
func TestTheUninstallPanelWaitsForADeleteNotFinished(t *testing.T) {
	if passed := runPageScenario(t, "uninstallWaits"); len(passed) != 6 {
		t.Errorf("the uninstallWaits scenario passed %d of its 6 checks: %v", len(passed), passed)
	}
}
