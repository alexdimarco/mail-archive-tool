package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1424, MA-614, R52
// The `quit` verb is a verb of the one program, and its outcome is the exit status (the S1
// fix round 2, B1, B2: the upgrade script reads it). Positive twin first: `quit -h` says
// what it does and lists its exit codes, exit 0. With no MailArchive running for the
// account (a throwaway home) it says so, exit 4 — never 1, which is what an older program
// without the verb exits with ("-out is required"), so the script can tell them apart;
// a process that is not the account's copy is exit 4 too; an unknown flag or an argument
// is a usage error, exit 2, not 1.
func TestMA1424_TheQuitVerbIsRoutedAndItsOutcomeIsTheExitStatus(t *testing.T) {
	code, stderr := runCLI("quit", "-h")
	if code != 0 || !strings.Contains(stderr, "Usage: mailarchive quit") || !strings.Contains(stderr, "3 it is busy") {
		t.Fatalf("twin: quit -h: %d %q", code, stderr)
	}
	code, stderr = runCLI("quit")
	assure.Refused(t, code, stderr, assure.Code(4), assure.Names("not running for your account"))
	code, stderr = runCLI("quit", "-check", "-pid", "999999999")
	assure.Refused(t, code, stderr, assure.Code(4), assure.Names("Process 999999999"))
	code, stderr = runCLI("quit", "-bogus")
	assure.Refused(t, code, stderr, assure.Code(2), assure.Names("-bogus"))
	code, stderr = runCLI("quit", "now")
	assure.Refused(t, code, stderr, assure.Code(2), assure.Names("takes no arguments"))
	if strings.Contains(stderr, "mailarchive: mailarchive") {
		t.Errorf("the refusal doubled its prefix: %q", stderr)
	}
}
