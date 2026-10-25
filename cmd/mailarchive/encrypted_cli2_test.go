package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-739, R37
// The CLI uninstall preview lists an encrypted archive's leftover working copies, each
// by path, only when there are any: a plan with two leftovers names both under the
// "leftover working copies" line; a plan with none has no such line at all.
func TestUninstallPlanListsLeftoverWorkingCopiesOnlyWhenAny(t *testing.T) {
	leftovers := []string{
		filepath.Join("scratch", "0123456789abcdef0123456789abcdef", "snap", ".mailarchive-snapshot-1"),
		filepath.Join("scratch", "fedcba9876543210fedcba9876543210", "_outlook-pst", "mail.pst"),
	}
	var with bytes.Buffer
	printUninstallPlan(&with, &uninstall.Report{Scratch: leftovers})
	plan := assure.Reached(t, with.String(), "the uninstall plan with leftovers")
	head := "  - leftover working copies in MailArchive's working folder: 2\n"
	at := strings.Index(plan, head)
	if at < 0 {
		t.Fatalf("the plan does not list the leftover working copies:\n%s", plan)
	}
	// Each leftover is listed by path under that line, not just counted.
	for _, p := range leftovers {
		if !strings.Contains(plan[at+len(head):], "      "+p+"\n") {
			t.Fatalf("the plan does not name the leftover %s under its line:\n%s", p, plan)
		}
	}

	var without bytes.Buffer
	printUninstallPlan(&without, &uninstall.Report{})
	if plan := assure.Reached(t, without.String(), "the uninstall plan without leftovers"); strings.Contains(plan, "leftover working copies") {
		t.Fatalf("a plan with no leftovers still mentions them:\n%s", plan)
	}
}

// covers: MA-741, R37
// Pausing through the built binary keeps the owner's note beside a plaintext archive,
// but beside an encrypted archive writes an EMPTY pause marker and says the note was
// not stored. The positive twin (plaintext keeps its note, no such line) runs first.
func TestPauseOfAnEncryptedArchiveStoresNoNoteAndSaysSo(t *testing.T) {
	env := append(os.Environ(), "HOME="+t.TempDir(), "XDG_CONFIG_HOME="+t.TempDir(), "AppData="+t.TempDir(), "LOCALAPPDATA="+t.TempDir())
	pause := func(out string) string {
		t.Helper()
		cmd := exec.Command(testBin, "pause", "-out", out, "-reason", "parked for the move")
		cmd.Env = env
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("`pause` failed: %v\n%s", err, b)
		}
		return assure.Reached(t, string(b), "the pause output")
	}
	const notStored = "The note was not stored"

	plain := cliArchive(t, false)
	if msg := pause(plain); strings.Contains(msg, notStored) {
		t.Fatalf("the positive twin: a plaintext archive's pause says its note was not stored:\n%s", msg)
	}
	if got := seal.Reason(plain); got != "parked for the move" {
		t.Fatalf("the positive twin: a plaintext archive's pause keeps its note, got %q", got)
	}

	sealed := cliArchive(t, true)
	if msg := pause(sealed); !strings.Contains(msg, notStored) {
		t.Fatalf("an encrypted archive's pause does not say the note was not stored:\n%s", msg)
	}
	raw, err := os.ReadFile(filepath.Join(sealed, seal.Name))
	if err != nil {
		t.Fatalf("an encrypted archive's pause wrote no pause marker: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("an encrypted archive's pause marker is not empty: %q", raw)
	}
	if !seal.IsSealed(sealed) {
		t.Fatal("an encrypted archive's pause did not seal it")
	}
}
