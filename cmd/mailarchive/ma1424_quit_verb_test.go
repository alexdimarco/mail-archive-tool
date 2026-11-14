package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
)

// covers: MA-1424, MA-614, MA-613, R52
// The `quit` verb is a verb of the one program, and its outcome is the exit status (the S1
// fix round 2, B1, B2: the upgrade script reads it). Positive twin first: `quit -h` says
// what it does and lists its exit codes, exit 0. With no MailArchive running for the
// account (a throwaway home) it says so, exit 2; a process that is not the account's copy
// is exit 2 too; an unknown flag or an argument is refused, exit 1. Every code the verb
// can return is in X1's declared set (docs/ux-contract.md X1; MA-613's map), and the four
// outcomes the script tells apart are four different codes.
func TestMA1424_TheQuitVerbIsRoutedAndItsOutcomeIsTheExitStatus(t *testing.T) {
	code, stderr := runCLI("quit", "-h")
	if code != 0 || !strings.Contains(stderr, "Usage: mailarchive quit") || !strings.Contains(stderr, "3 it is busy") || !strings.Contains(stderr, "-end-older") {
		t.Fatalf("twin: quit -h: %d %q", code, stderr)
	}
	code, stderr = runCLI("quit")
	assure.Refused(t, code, stderr, assure.Code(desktop.QuitNotRunning), assure.Names("not running for your account"))
	code, stderr = runCLI("quit", "-check", "-pid", "999999999")
	assure.Refused(t, code, stderr, assure.Code(desktop.QuitNotRunning), assure.Names("Process 999999999"))
	code, stderr = runCLI("quit", "-bogus")
	assure.Refused(t, code, stderr, assure.Code(desktop.QuitCouldNotAsk), assure.Names("-bogus"))
	code, stderr = runCLI("quit", "now")
	assure.Refused(t, code, stderr, assure.Code(desktop.QuitCouldNotAsk), assure.Names("takes no arguments"))
	if strings.Contains(stderr, "mailarchive: mailarchive") {
		t.Errorf("the refusal doubled its prefix: %q", stderr)
	}
	seen := map[int]string{}
	for name, c := range map[string]int{"done": desktop.QuitDone, "could not ask": desktop.QuitCouldNotAsk,
		"not running": desktop.QuitNotRunning, "busy": desktop.QuitBusy} {
		if !x1Codes[c] {
			t.Errorf("quit's %q code %d is outside X1's declared set (MA-613)", name, c)
		}
		if other, dup := seen[c]; dup {
			t.Errorf("quit's %q and %q share code %d", name, other, c)
		}
		seen[c] = name
	}
}
