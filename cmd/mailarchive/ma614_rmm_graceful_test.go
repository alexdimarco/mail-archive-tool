package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
)

// covers: MA-614, R52, S59
// The RMM upgrade never cuts a running MailArchive's work short (the S1 adversarial
// pass, A2; MA-614 amended). Positive twin first: the dashboard rule is unchanged (no
// argument, `desktop` without -capture, `start`), and the former name is still ended
// unconditionally. A mailarchivew dashboard is first asked to close the graceful way,
// from inside its own sign-in session; one that refuses (work or an open wizard) is never
// forced, and the script then exits 1618 before msiexec, so the upgrade is not started
// under it. A `wizard`, `run-job`, `-job` or `-capture` process is never asked at all.
//
// Amended 2026-11-13 (the S1 fix round 2, docs/review-menubar-s1-adversarial-r2.md B1, B2,
// B5, B6, B7; amended, not weakened — every promise above still holds): the ask is the
// installed program's own `quit` verb, run as the copy's user by a one-time task (no
// window messages, no Add-Type or encoded PowerShell, nothing Constrained Language mode
// blocks); every copy is first asked only whether it would close (-check) and copies are
// closed only when none said no (B7); a copy that could not be asked is never called busy
// and never forced: exit 3, before msiexec (B2); only a copy proven to be from before
// slice S1 is forced — by its own record (quit's 6) or because the installed program
// predates the verb (no QuitVerb value, which Product.wxs now writes) and the copy runs
// it — never because it could not be reached (B1); the installed program is run only
// from under Program Files (it runs as another user); the task has a random name, is
// registered without -Force, carries a protected security descriptor (SYSTEM and
// Administrators only may change it), an execution limit, an expiry with
// DeleteExpiredTaskAfter, and is removed in a finally (B5, B6). The script's codes for
// quit's outcomes are the verb's own. The Windows VM lab of fix round 2 ran the real
// close step as SYSTEM against lab copies (idle, busy, under Constrained Language mode).
func TestMA614_TheUpgradeAsksTheRunningCopyAndNeverForcesWork(t *testing.T) {
	root := moduleRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "packaging", "mailarchive-desktop", "Install-RMM.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	inst := assure.Reached(t, string(b), "Install-RMM.ps1")
	if !strings.Contains(inst, `($cmdArgs -match '^start(\s|$)')`) || !regexp.MustCompile(`Get-Process mailarchive-desktop\b[^\n]*Stop-Process -Force`).MatchString(inst) {
		t.Fatalf("twin: the dashboard rule or the former-name stop changed")
	}
	// Only code, not the comments, is checked below.
	code := regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(inst, "")
	at := func(s string) int {
		t.Helper()
		i := strings.Index(code, s)
		if i < 0 {
			t.Errorf("the script lacks %q", s)
		}
		return i
	}

	// The ask: the installed program's quit verb, as the copy's own user, all or none.
	check, gate, closeAt := at(`"quit -check -pid $id"`), at(`if (-not $busy -and -not $cannot) {`), at(`"quit -pid $id -wait 30s"`)
	if !(check < gate && gate < closeAt) {
		t.Errorf("copies are closed before every copy was asked whether it would (check %d, gate %d, close %d)", check, gate, closeAt)
	}
	for _, want := range []string{"-MethodName GetOwnerSid", "<LogonType>InteractiveToken</LogonType>", "<RunLevel>LeastPrivilege</RunLevel>",
		"Join-Path $installDir 'mailarchivew.exe'", "Get-InstallValue 'QuitVerb') -eq '0x1'", "/reg:64",
		// Only from a folder administrators alone can write: it runs as another user.
		`$Exe.ToLowerInvariant().StartsWith($root.ToLowerInvariant())`, `($Exe -notmatch '\.\.')`, "$env:ProgramW6432"} {
		at(want)
	}
	for _, bad := range []string{"PostMessage", "Add-Type", "EncodedCommand", "New-Object", "ComObject", "::", "RmForceShutdown", "taskkill", "powershell.exe"} {
		if strings.Contains(code, bad) {
			t.Errorf("the script uses %q (window messages, compiled or encoded PowerShell, or what Constrained Language mode blocks)", bad)
		}
	}
	// The task: random name, never -Force, a protected descriptor, bounded, self-deleting.
	for _, want := range []string{"'MailArchive upgrade ' + (New-Guid).Guid", "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;{2})", "<ExecutionTimeLimit>PT2M</ExecutionTimeLimit>",
		"<EndBoundary>{4}</EndBoundary>", "<DeleteExpiredTaskAfter>PT0S</DeleteExpiredTaskAfter>", "<StartWhenAvailable>false</StartWhenAvailable>",
		"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>", "} finally {\n        Unregister-ScheduledTask -TaskName $name -TaskPath '\\'"} {
		at(want)
	}
	if regexp.MustCompile(`Register-ScheduledTask[^\n]*-Force`).MatchString(code) {
		t.Errorf("the task is registered with -Force (it would update a task someone made first)")
	}

	// The one forced stop of a mailarchivew is a copy proven older; it comes only after
	// every copy was asked and none said no or could not be asked.
	forced := regexp.MustCompile(`Stop-Process -Id \$proc\.ProcessId -Force`).FindAllStringIndex(code, -1)
	if len(forced) != 1 {
		t.Fatalf("%d forced stops of a mailarchivew, want 1", len(forced))
	}
	if ret := at(`return @{ Code = $code; Lines = $lines }`); ret > forced[0][0] {
		t.Errorf("a copy is forced before a busy or unaskable copy stops the run")
	}
	if n := strings.Count(code, "$force += $proc"); n != 2 ||
		!strings.Contains(code, "$r -eq $QuitOlder) { $force += $proc }") ||
		!strings.Contains(code, "$proc.ExecutablePath.ToLowerInvariant() -eq $Exe.ToLowerInvariant()) { $force += $proc }") {
		t.Errorf("a copy is marked for a forced stop other than by its own record or by running the installed program that predates quit (%d)", n)
	}
	if !strings.Contains(code, "elseif ($r -eq $QuitBusy) { $busy += $id }") || !strings.Contains(code, `else { $cannot += "process ${id}: " + (Get-CouldNotAskReason $r) }`) {
		t.Errorf("a refusal is not busy, or a copy that could not be asked is not reported as such")
	}
	if strings.Contains(code, "else { $busy += ") {
		t.Errorf("any other answer is still lumped in with busy (B2)")
	}
	at("if ($cannot) { $code = $ExitCouldNotAsk }")
	at("$ExitCouldNotAsk = 3")
	exitAt, msiAt := at("if ($closing.Code -ne 0) { exit $closing.Code }"), at("Start-Process msiexec.exe")
	if exitAt > msiAt {
		t.Errorf("a busy or unaskable copy does not stop the upgrade before msiexec")
	}
	at("$code = 1618")
	// The verb's codes, as the program defines them.
	want := fmt.Sprintf("$QuitDone = %d; $QuitBusy = %d; $QuitNotRunning = %d; $QuitOlder = %d; $QuitStillClosing = %d",
		desktop.QuitDone, desktop.QuitBusy, desktop.QuitNotRunning, desktop.QuitOlder, desktop.QuitStillClosing)
	at(want)
	if strings.Contains(code, "'^wizard") || strings.Contains(code, "run-job") {
		t.Errorf("the script now matches a wizard or run-job process")
	}

	wxs, err := os.ReadFile(filepath.Join(root, "packaging", "mailarchive-desktop", "Product.wxs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wxs), `<RegistryValue Name="QuitVerb" Type="integer" Value="1" />`) {
		t.Errorf("Product.wxs does not record that the installed program has the quit verb")
	}
}
