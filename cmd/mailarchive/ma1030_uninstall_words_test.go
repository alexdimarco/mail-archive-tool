package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1030, R36
// The command line's uninstall plan in the 1.5.3 friction review's words (B11): deletions.log is
// listed once, on its own line with where to copy it, and not again among the settings files;
// what is kept says "uninstalling never deletes your mail" (never "your mail is never deleted",
// now that Delete… exists); with no archive listed it says "No archives are listed." instead of
// "your 0 archive(s)" over an empty list. Positive twin first: a plan with archives lists each.
func TestUninstallPlanAtTheTerminalSaysWhatUninstallingKeeps(t *testing.T) {
	rep := &uninstall.Report{
		ConfigFiles:  []string{"/cfg/desktop-settings.json", "/cfg/deletions.log"},
		DeletionsLog: "/cfg/deletions.log",
		KeptArchives: []string{"/archives/one", "/archives/two"},
	}
	var buf bytes.Buffer
	printUninstallPlan(&buf, rep, "")
	out := buf.String()
	if !strings.Contains(out, "It will KEEP your 2 archive(s) — uninstalling never deletes your mail:\n      /archives/one\n      /archives/two\n") {
		t.Fatalf("positive twin: the kept archives are listed:\n%s", out)
	}
	if n := strings.Count(out, "/cfg/deletions.log"); n != 1 || !strings.Contains(out, "  - settings/log files: 1\n      /cfg/desktop-settings.json\n") {
		t.Fatalf("deletions.log must be listed once (%d times) and not counted among the settings files:\n%s", n, out)
	}
	if !strings.Contains(out, "  - the deletion log: /cfg/deletions.log\n      "+uninstall.DeletionsLogNote+"\n") || !strings.Contains(uninstall.DeletionsLogNote, "somewhere you keep records") {
		t.Fatalf("the deletion log's line must say where to copy it:\n%s", out)
	}

	rep.KeptArchives = nil
	buf.Reset()
	printUninstallPlan(&buf, rep, "")
	out = buf.String()
	if !strings.HasSuffix(out, "It will KEEP your archives — uninstalling never deletes your mail. No archives are listed.\n") {
		t.Fatalf("with no archive listed the plan must say so:\n%s", out)
	}
	for _, bad := range []string{"0 archive(s)", "never deleted"} {
		if strings.Contains(out, bad) {
			t.Fatalf("the plan still says %q:\n%s", bad, out)
		}
	}
}

// jsonString is s as a JSON string.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// covers: MA-1030, R36
// `uninstall -dry-run` with a delete that did not finish shows the plan, then says "Uninstall
// will refuse until…" with the real folder in the command that finishes it — never the
// placeholder FOLDER, never "removed nothing" — exit 1, nothing changed; `uninstall -yes` says it
// removed nothing, with the same real command (the 1.5.3 friction review's B11). The real program
// in a throwaway home. Positive twin first: the same delete finished, the dry run shows the plan
// and exits 0.
func TestUninstallDryRunShowsThePlanAndWhenItWillRefuse(t *testing.T) {
	env, cfg := uninstallHome(t)
	settings, log := filepath.Join(cfg, "desktop-settings.json"), filepath.Join(cfg, "deletions.log")
	folder := filepath.Join(t.TempDir(), "Old mail") // deleted: not there any more
	started := `{"at":"2026-11-01T10:00:00Z","event":"started","run":"r1","folder":` + jsonString(folder) + `,"row":` + jsonString(folder) + `,"kind":"plain"}` + "\n" +
		`{"at":"2026-11-01T10:00:01Z","event":"folder removed","run":"r1","folder":` + jsonString(folder) + `}` + "\n"
	finished := `{"at":"2026-11-01T10:00:02Z","event":"finished","run":"r1","folder":` + jsonString(folder) + `,"outcome":"complete"}` + "\n"

	// Positive twin: the delete finished.
	writeOwnerOnly(t, settings, "{}")
	writeOwnerOnly(t, log, started+finished)
	code, out, errs := runUninstallIn(t, env, "-dry-run")
	if code != 0 || !strings.Contains(out, "  - the deletion log: "+log+"\n") || strings.Contains(errs, "refuse") {
		t.Fatalf("positive twin: a finished delete's dry run shows the plan: exit %d\n%s%s", code, out, errs)
	}

	// Not finished.
	writeOwnerOnly(t, log, started)
	finish := "mailarchive delete-archive -out " + encmark.CommandPath(folder)
	unchanged := func() bool { return fileIs(log, started) && fileIs(settings, "{}") }
	code, out, errs = runUninstallIn(t, env, "-dry-run")
	assure.Refused(t, code, errs, assure.Code(1),
		assure.Names("Uninstall will refuse until the delete of "+folder+" is finished", log, finish, "Delete… on its row in MailArchive Desktop"),
		assure.Forbid("FOLDER", "removed nothing"),
		assure.NoSideEffect(func() bool {
			return unchanged() && strings.Contains(out, "MailArchive uninstall will REMOVE:") &&
				strings.Contains(out, "  - the deletion log: "+log+"\n") && strings.Contains(out, settings)
		}))

	code, _, errs = runUninstallIn(t, env, "-yes")
	assure.Refused(t, code, errs, assure.Code(1),
		assure.Names("A delete of "+folder+" is running or did not finish, so uninstall removed nothing", finish),
		assure.Forbid("FOLDER"),
		assure.NoSideEffect(unchanged))
}
