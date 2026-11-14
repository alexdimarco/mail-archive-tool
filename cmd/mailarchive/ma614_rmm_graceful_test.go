package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-614, R52, S59
// The RMM upgrade never cuts a running MailArchive's work short (the S1 adversarial
// pass, A2; MA-614 amended). Positive twin first: the dashboard rule is unchanged (no
// argument, `desktop` without -capture, `start`), and the former name is still ended
// unconditionally. A mailarchivew dashboard is first asked to close the graceful way,
// from inside its own sign-in session (an interactive task of its user posts WM_CLOSE to
// the copy's instance window, with a bound; Restart Manager cannot ask a copy whose only
// window is hidden); only one with no instance window (a dashboard from before slice S1)
// is ended by force; one that refuses (work or an open wizard), or a task that cannot
// run, is never forced, and the script then exits 1618 before msiexec, so the upgrade is
// not started under it. A `wizard`, `run-job`, `-job` or `-capture` process is never
// asked at all. Proven on the Windows VM by the fix round's walk, run as SYSTEM (an idle
// copy closed gracefully; a copy with its wizard open refused, kept its wizard and the
// script exited 1618; a pre-S1 dashboard was forced as before).
func TestMA614_TheUpgradeAsksTheRunningCopyAndNeverForcesWork(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "packaging", "mailarchive-desktop", "Install-RMM.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	inst := assure.Reached(t, string(b), "Install-RMM.ps1")
	if !strings.Contains(inst, `($cmdArgs -match '^start(\s|$)')`) || !regexp.MustCompile(`Get-Process mailarchive-desktop\b[^\n]*Stop-Process -Force`).MatchString(inst) {
		t.Fatalf("twin: the dashboard rule or the former-name stop changed")
	}
	code := inst
	// Only code, not the comments, is checked below.
	code = regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(code, "")
	// The graceful ask: from the copy's own session (an interactive task of its user),
	// a WM_CLOSE posted to its instance window only, with a bound; never a forced close.
	for _, want := range []string{`c.ToString() == "MailArchiveInstanceWindow"`, "PostMessage(found, 0x0010,", "-LogonType Interactive",
		"for ($i = 0; $i -lt 30; $i++)", "$closed = Close-InItsSession $proc", "Unregister-ScheduledTask"} {
		if !strings.Contains(code, want) {
			t.Errorf("the graceful close lacks %q", want)
		}
	}
	if strings.Contains(code, "RmForceShutdown") || strings.Contains(code, "taskkill") {
		t.Errorf("the script closes a copy by force")
	}
	// The one forced stop of a mailarchivew is the "no instance window" answer (2).
	forced := regexp.MustCompile(`Stop-Process -Id \$proc\.ProcessId -Force`).FindAllStringIndex(code, -1)
	if len(forced) != 1 || !strings.Contains(code, "elseif ($closed -eq 2) { Stop-Process -Id $proc.ProcessId -Force") {
		t.Errorf("a mailarchivew is forced other than when it has no instance window (%d forced stops)", len(forced))
	}
	if !strings.Contains(code, "else { $busy += $proc.ProcessId }") {
		t.Errorf("a refusal (or a task that could not run) is not recorded as busy")
	}
	deferAt, msiAt := strings.Index(code, "exit 1618"), strings.Index(code, "Start-Process msiexec.exe")
	if deferAt < 0 || msiAt < 0 || deferAt > msiAt {
		t.Errorf("a busy copy does not stop the upgrade before msiexec (exit 1618 at %d, msiexec at %d)", deferAt, msiAt)
	}
	if strings.Contains(code, "'^wizard") || strings.Contains(code, "run-job") {
		t.Errorf("the script now matches a wizard or run-job process")
	}
}
