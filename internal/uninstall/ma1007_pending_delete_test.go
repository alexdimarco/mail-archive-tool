package uninstall

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/schedule"
)

// logLines writes deletions.log in dir with these lines, one JSON object each, owner-only.
func logLines(t *testing.T, dir string, lines ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		j, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, archdelete.LogName)
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// noSchedulesNoSignIn stands in for the scheduler and the sign-in (none here), for every
// uninstall in a test.
func noSchedulesNoSignIn(t *testing.T) {
	t.Helper()
	origList, origRemove, origClear := listSchedules, removeSchedule, clearSignIn
	t.Cleanup(func() { listSchedules, removeSchedule, clearSignIn = origList, origRemove, origClear })
	listSchedules = func() []schedule.InstalledEntry { return nil }
	removeSchedule = func(schedule.Spec) error { t.Fatal("no schedule here to remove"); return nil }
	clearSignIn = func(string, string) error { t.Fatal("no sign-in here to clear"); return nil }
}

// refusalCode is 1 for uninstall's typed refusal of a pending delete, 0 otherwise.
func refusalCode(err error) int {
	var p *DeleteNotFinishedError
	if errors.As(err, &p) {
		return 1
	}
	return 0
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// covers: MA-1007, R36
// Uninstall refuses, removing nothing, while deletions.log holds a delete that did not finish
// (docs/design-archive-delete.md §5.4; del-s5-A1-06): an encrypted archive's delete stopped
// after its folder was removed and before step 8, so its row says "Delete not finished".
// Removing the log would leave it unfinishable — Plan's re-run reads only the log — while this
// computer kept the archive's record and key. The dry run (the plan) and the real run both
// refuse, naming the folder, the log and how to go on. Positive twin first: the same delete
// finished, the plan names the log and the real run removes it.
func TestUninstallRefusesWhileADeleteDidNotFinish(t *testing.T) {
	noSchedulesNoSignIn(t)
	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "desktop-settings.json"), "{}")
	gone := filepath.Join(t.TempDir(), "Old mail")
	started := map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "r1", "folder": gone, "row": gone,
		"kind": "encrypted", "record_id": "0123456789abcdef0123456789abcdef", "record_spelling": gone}
	removed := map[string]any{"at": "2026-11-01T10:00:01Z", "event": "folder removed", "run": "r1", "folder": gone}

	// Positive twin: the delete finished; nothing refuses, and the log goes with the settings.
	log := logLines(t, cfg, started, removed, map[string]any{"at": "2026-11-01T10:00:02Z", "event": "finished", "run": "r1", "folder": gone, "outcome": "complete"})
	rep, err := Uninstall(cfg, nil, "", true)
	if err != nil || rep.DeletionsLog != log {
		t.Fatalf("positive twin: a finished delete's log is planned for removal: %v, %+v", err, rep)
	}
	if _, err := Uninstall(cfg, nil, "", false); err != nil || exists(log) {
		t.Fatalf("positive twin: the real run removes a finished delete's log: %v (log there: %v)", err, exists(log))
	}

	// The command that finishes it names the real folder, never a placeholder (amended
	// 2026-11-02, the 1.5.3 friction review's B11). The plan (a dry run) now returns its Report
	// with the refusal, so the plan can be shown; its Until says "Uninstall will refuse until…",
	// not "removed nothing". The real run returns no Report, as before.
	finish := "mailarchive delete-archive -out " + encmark.CommandPath(gone)
	for _, run := range []struct {
		name string
		dry  bool
	}{{"the plan", true}, {"the real run", false}} {
		writeFile(t, filepath.Join(cfg, "desktop-settings.json"), "{}")
		log = logLines(t, cfg, started, removed)
		before, _ := os.ReadFile(log)
		rep, err := Uninstall(cfg, nil, "", run.dry)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		assure.Refused(t, refusalCode(err), msg, assure.Code(1),
			assure.Names(gone, log, "removed nothing", "Delete… on its row", finish, "Forget this encrypted archive here"),
			assure.Forbid("FOLDER"),
			assure.NoSideEffect(func() bool {
				after, _ := os.ReadFile(log)
				planned := rep == nil
				if run.dry {
					planned = rep != nil && rep.DeletionsLog == log
				}
				return planned && string(after) == string(before) && exists(filepath.Join(cfg, "desktop-settings.json"))
			}))
		if run.dry {
			var dn *DeleteNotFinishedError
			if !errors.As(err, &dn) {
				t.Fatalf("the plan's refusal is not a DeleteNotFinishedError: %v", err)
			}
			assure.Refused(t, 1, dn.Until(), assure.Code(1),
				assure.Names("Uninstall will refuse until the delete of "+gone+" is finished", log, finish, "Delete… on its row", "Forget this encrypted archive here"),
				assure.Forbid("FOLDER", "removed nothing"))
		}
	}
}

// covers: MA-1007, R36
// A delete running now is refused the same way (del-s5-A2-02): its started line has no finished
// line yet. And one that begins after uninstall's first check, before its log would go (another
// MailArchive window, the command line), stops the real run before the settings files are
// removed, so the log keeps its started line and nothing writes it again behind a finished
// uninstall. Positive twin first: no delete in the log, the real run removes the settings and
// the log.
func TestUninstallStopsBeforeTheSettingsWhenADeleteStartsMeanwhile(t *testing.T) {
	noSchedulesNoSignIn(t)
	cfg := t.TempDir()
	settings := filepath.Join(cfg, "desktop-settings.json")
	folder := t.TempDir() // a delete under way: the folder is still there
	startedLine := map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "r2", "folder": folder, "row": folder, "kind": "plain"}

	// Positive twin.
	writeFile(t, settings, "{}")
	log := logLines(t, cfg, map[string]any{"at": "2026-11-01T09:00:00Z", "event": "started", "run": "r0", "folder": folder, "row": folder, "kind": "plain"},
		map[string]any{"at": "2026-11-01T09:00:01Z", "event": "finished", "run": "r0", "folder": folder, "outcome": "refused"})
	if _, err := Uninstall(cfg, nil, "", false); err != nil || exists(log) || exists(settings) {
		t.Fatalf("positive twin: %v (log %v, settings %v)", err, exists(log), exists(settings))
	}

	// Under way now: refused before anything is removed.
	writeFile(t, settings, "{}")
	log = logLines(t, cfg, startedLine)
	_, err := Uninstall(cfg, nil, "", false)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(folder, "running or did not finish"),
		assure.NoSideEffect(func() bool { return exists(log) && exists(settings) }))

	// Begins between the first check and the removal of the settings.
	writeFile(t, settings, "{}")
	log = logLines(t, cfg)
	orig := pendingDeletes
	t.Cleanup(func() { pendingDeletes = orig })
	calls := 0
	pendingDeletes = func(path, goos string) ([]string, error) {
		calls++
		if calls == 2 { // the delete has written its started line by now
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				return nil, err
			}
			j, _ := json.Marshal(startedLine)
			f.Write(append(j, '\n'))
			f.Close()
		}
		return orig(path, goos)
	}
	_, err = Uninstall(cfg, nil, "", false)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(folder, "before the settings files were removed"),
		assure.NoSideEffect(func() bool {
			b, _ := os.ReadFile(log)
			return exists(settings) && strings.Contains(string(b), `"run":"r2"`)
		}))
	assure.Reached(t, calls == 2, "uninstall checked the log again before removing the settings")
}
