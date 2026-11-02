package desktop

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1030, R36
// The Desktop's uninstall words in the 1.5.3 friction review's B11: now that Delete… exists,
// the Security & privacy card, the Uninstall card and its button promise only that
// uninstalling never deletes your mail, never "your saved mail is never deleted"; the plan's
// panel counts deletions.log once (on its own line, not again among the settings and log files)
// and, with no archive listed, says "No archives are listed." instead of "your 0 archive(s)"
// over "(none recorded)". Positive twin first: a plan with archives lists each under what it
// keeps.
func TestTheDesktopSaysUninstallingNeverDeletesYourMail(t *testing.T) {
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	cfgDir := t.TempDir()
	logPath := filepath.Join(cfgDir, "deletions.log")
	var kept []string
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		return &uninstall.Report{ConfigFiles: []string{filepath.Join(configDir, "desktop-settings.json"), logPath},
			DeletionsLog: logPath, KeptArchives: kept}, nil
	}
	d := newDashboard(Config{ConfigPath: filepath.Join(cfgDir, "graph-config.json")})
	planText := func() string {
		t.Helper()
		rec := postUninstall(t, d, `{"confirm":false}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body)
		}
		return uninstallPlanLines(t, rec.Body.Bytes())
	}

	// Positive twin: archives listed.
	kept = []string{"/archives/one", "/archives/two"}
	text := planText()
	if !strings.Contains(text, "It will KEEP your 2 archive(s) — uninstalling never deletes your mail:\n   /archives/one\n   /archives/two") {
		t.Fatalf("positive twin: the kept archives are listed:\n%s", text)
	}
	if !strings.Contains(text, "- settings and log files: 1\n") || strings.Count(text, logPath) != 1 {
		t.Fatalf("deletions.log must be counted once, on its own line:\n%s", text)
	}

	kept = nil
	text = planText()
	if !strings.Contains(text, "It will KEEP your archives — uninstalling never deletes your mail. No archives are listed.") {
		t.Fatalf("with no archive listed the panel must say so:\n%s", text)
	}
	for _, bad := range []string{"0 archive(s)", "(none recorded)", "never deleted"} {
		if strings.Contains(text, bad) {
			t.Fatalf("the panel still says %q:\n%s", bad, text)
		}
	}

	page := getOverview(t, Config{ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
	for _, want := range []string{
		"<li><b>Reversible.</b> You can revoke access any time, and uninstalling keeps your archives — uninstalling never deletes your mail. Only Delete… deletes an archive's files, after you type its folder's name.</li>",
		"<b>Your archives are kept</b> — uninstalling never deletes your mail. Removing the program itself is a last step your system shows you.",
		`title="Remove MailArchive's scheduled backups, settings and sign-in from this computer. Uninstalling never deletes your mail.`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(page, "never deleted") {
		t.Error(`the page still says "never deleted"`)
	}
}
