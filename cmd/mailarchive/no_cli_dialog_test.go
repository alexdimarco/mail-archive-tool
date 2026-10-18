package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeDialogRun runs the built program with a recording fake of every dialog/
// notification program on PATH ahead of the real ones (zenity/qarma/matedialog
// on Linux, osascript on macOS — all PATH-resolved by ncruces/zenity), with
// per-account state isolated. It returns one line per dialog invocation (the
// program's argv), so a test can assert a command-line verb invokes none.
func fakeDialogRun(t *testing.T, args ...string) (invocations []string, stderr string) {
	t.Helper()
	dir := t.TempDir()
	rec := filepath.Join(t.TempDir(), "dialog.log")
	for _, name := range []string{"zenity", "qarma", "matedialog", "osascript"} {
		script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + rec + "\nexit 0\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testBin, args...)
	env := isolatedEnv(t)
	kept := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "PATH=") {
			kept = append(kept, e)
		}
	}
	cmd.Env = append(kept, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var errb strings.Builder
	cmd.Stderr = &errb
	_ = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("%v did not exit within the timeout; a command-line verb must not block on a dialog. stderr:\n%s", args, errb.String())
	}
	data, _ := os.ReadFile(rec)
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(ln) != "" {
			invocations = append(invocations, ln)
		}
	}
	return invocations, errb.String()
}

// covers: MA-624, S59
// No dialog from a command-line verb: with a recording fake dialog program on
// PATH, none of the command-line verbs invokes it — the native-dialog code lives
// only in the wizard and desktop verbs (R33). run-job's one dialog exec is the
// failure notification: a failing run raises exactly one, and on Linux its argv
// begins "--notification" (the zenity form the row names); macOS notifies through
// osascript and Windows natively (L-C2). The per-account state is isolated and
// the block-prone serve/setup are probed through -h, since neither imports the
// dialog library and both would otherwise bind or prompt.
func TestMA624_NoDialogFromACommandLineVerb(t *testing.T) {
	out := t.TempDir()
	cliVerbs := [][]string{
		{"-out", out}, // the default export verb (no input → refuses)
		{"status", "-out", out},
		{"verify", "-out", out},
		{"search", "-out", out, "query"},
		{"reindex", "-out", out},
		{"schedule", "-out", out},
		{"graph", "-out", out},
		{"extract", "-h"},
		{"serve", "-h"},
		{"setup", "-h"},
	}
	for _, args := range cliVerbs {
		inv, _ := fakeDialogRun(t, args...)
		if len(inv) != 0 {
			t.Errorf("command-line verb %v invoked a dialog program %d time(s): %v", args, len(inv), inv)
		}
	}

	// run-job's only dialog exec is the failure notification.
	jobDir := t.TempDir()
	jobPath := filepath.Join(jobDir, "job.json")
	// An input that does not exist, so the headless run fails and notifies.
	body := `{"version":1,"name":"x","out":"` + filepath.ToSlash(filepath.Join(jobDir, "out")) +
		`","mode":"incremental","inputs":["` + filepath.ToSlash(filepath.Join(jobDir, "nope.pst")) + `"]}`
	if err := os.WriteFile(jobPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, stderr := fakeDialogRun(t, "run-job", jobPath)
	if len(inv) != 1 {
		t.Fatalf("a failing run-job invoked the dialog program %d time(s), want exactly one (the failure notification). stderr:\n%s\ninvocations: %v", len(inv), stderr, inv)
	}
	if runtime.GOOS == "linux" && !strings.HasPrefix(strings.TrimSpace(inv[0]), "--notification") {
		t.Errorf("run-job's notification argv = %q, want it to begin with --notification", inv[0])
	}
}
