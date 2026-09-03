package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

var testBin string

// TestMain builds the CLI once so the refusal tests exercise the real binary's
// exit codes and stderr (a refusal is a subprocess contract, not a function
// return).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mailarchive-clitest")
	if err != nil {
		panic(err)
	}
	testBin = filepath.Join(dir, "mailarchive")
	if runtime.GOOS == "windows" {
		testBin += ".exe" // exec needs the real produced name on Windows
	}
	build := exec.Command("go", "build", "-o", testBin, ".")
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("building CLI for tests: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func runCLI(args ...string) (int, string) {
	cmd := exec.Command(testBin, args...)
	var errb strings.Builder
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	return code, errb.String()
}

// covers: MA-33, MA-34, R12, S9
// Invalid operator input is refused with a typed non-zero exit and a message
// that names the problem — never a panic/stack trace.
func TestCLIRefusals(t *testing.T) {
	// Missing required -out.
	code, stderr := runCLI("-input", "nope")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-out"))

	// Bad -mode value.
	code, stderr = runCLI("-out", t.TempDir(), "-mode", "sideways")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("mode"))

	// serve against a directory with no index.
	code, stderr = runCLI("serve", "-out", t.TempDir())
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("index"))

	// search against a directory with no index.
	code, stderr = runCLI("search", "-out", t.TempDir(), "anything")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("index"))

	// reindex with no -out.
	code, stderr = runCLI("reindex")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-out"))

	// reindex against a directory with no index.
	code, stderr = runCLI("reindex", "-out", t.TempDir())
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("index"))
}

// covers: MA-59, R16, R12
// The -outlook option requires Windows + classic Outlook; on any other platform
// it refuses with a typed non-zero exit naming the requirement — never a crash.
// The assertion anchors on the phrase "requires Windows" (unique to the refusal
// message): plain "Windows"/"Outlook" would also match the temp-dir path and the
// progress log line, and pass even if the refusal never fired.
func TestOutlookFlagUnsupportedRefusal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows, -outlook drives real Outlook rather than refusing")
	}
	code, stderr := runCLI("-out", t.TempDir(), "-outlook")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("requires Windows", "classic Outlook"))
}

// covers: MA-63, R17, R12
// graph refuses each missing required input (-out, -tenant, -client-id, -mailbox,
// and an unset client secret) with a typed non-zero exit naming the problem.
func TestGraphRefusals(t *testing.T) {
	code, stderr := runCLI("graph")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-out"))

	code, stderr = runCLI("graph", "-out", t.TempDir())
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-tenant"))

	code, stderr = runCLI("graph", "-out", t.TempDir(), "-tenant", "contoso.com")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-client-id"))

	code, stderr = runCLI("graph", "-out", t.TempDir(), "-tenant", "contoso.com", "-client-id", "app")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-mailbox"))

	// Secret env var deliberately points at an unset variable → names it.
	code, stderr = runCLI("graph", "-out", t.TempDir(), "-tenant", "contoso.com",
		"-client-id", "app", "-mailbox", "u@contoso.com", "-client-secret-env", "MAILARCHIVE_DEFINITELY_UNSET_XYZ")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("secret", "MAILARCHIVE_DEFINITELY_UNSET_XYZ"))
}

// covers: MA-50, R14, R12, S9, S14
// schedule refuses a missing -out and a bad -interval with a typed non-zero exit
// naming the problem, and never applies anything on the refusal path.
func TestScheduleRefusals(t *testing.T) {
	// Missing required -out (default interval is valid).
	code, stderr := runCLI("schedule")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-out"))

	// Bad -interval value.
	code, stderr = runCLI("schedule", "-out", t.TempDir(), "-interval", "fortnightly")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("interval"))

	// Both -install and -remove is contradictory.
	code, stderr = runCLI("schedule", "-out", t.TempDir(), "-auto", "-install", "-remove")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("install", "remove"))
}

// covers: MA-39, R12
// X3 (UX contract): the root and every subcommand emit usage naming their flags.
func TestHelpTotality(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"-h"}, []string{"-out", "-input", "-enable-offline"}},
		{[]string{"serve", "-h"}, []string{"-out", "-addr"}},
		{[]string{"search", "-h"}, []string{"-out", "-folder"}},
		{[]string{"reindex", "-h"}, []string{"-out"}},
		{[]string{"schedule", "-h"}, []string{"-out", "-interval", "-install"}},
		{[]string{"graph", "-h"}, []string{"-out", "-tenant", "-mailbox", "-client-secret-file"}},
		{[]string{"status", "-h"}, []string{"-out", "-name"}},
	}
	for _, c := range cases {
		_, stderr := runCLI(c.args...)
		for _, w := range c.want {
			if !strings.Contains(stderr, w) {
				t.Errorf("help for %v does not name %q; got:\n%s", c.args, w, stderr)
			}
		}
	}
}

// covers: MA-85, R5, R12, S25
// A run against an archive another run holds refuses with a typed non-zero exit
// naming the lock and the fact that the archive is in use, and leaves no
// manifest behind. The positive twin: the same archive exports once released.
func TestRefusesLockedArchive(t *testing.T) {
	out := t.TempDir()
	held, err := lockfile.Acquire(filepath.Join(out, lockfile.Name))
	if err != nil {
		t.Fatal(err)
	}
	code, stderr := runCLI("-input", "../../testdata/support.pst", "-out", out)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("in use", lockfile.Name),
		assure.NoSideEffect(func() bool {
			_, err := os.Stat(filepath.Join(out, ".mailarchive-manifest.json"))
			return os.IsNotExist(err)
		}))
	held.Release()

	if code, stderr := runCLI("-input", "../../testdata/support.pst", "-out", out); code != 0 {
		t.Fatalf("export after release failed (%d): %s", code, stderr)
	}
}

// covers: MA-72, R14, R12, S28
// schedule validates the job NOW through the real flag definitions: a
// non-backup verb, an interactive flag, a Graph job without a secret file, and
// a flat-form flag mixed with a -- job are each refused naming the problem;
// valid export and graph jobs preview with their -log and are not applied.
func TestScheduleJobValidation(t *testing.T) {
	out := t.TempDir()

	code, stderr := runCLI("schedule", "--", "serve", "-out", out)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("serve", "cannot be a scheduled backup job"))

	code, stderr = runCLI("schedule", "--", "status", "-out", out)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("status", "cannot be scheduled"))

	code, stderr = runCLI("schedule", "-out", out, "-input", "x.pst", "-enable-offline")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-enable-offline", "interactive"))

	code, stderr = runCLI("schedule", "--", "graph", "-out", out, "-tenant", "t", "-client-id", "c", "-mailbox", "m@x")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-client-secret-file", "no environment"))

	code, stderr = runCLI("schedule", "-out", out, "--", "-out", out, "-auto")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-out", "after --"))

	code, stderr = runCLI("schedule", "-name", "bad name!", "-out", out, "-auto")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-name", "letters"))

	// Valid export job via --: previewed, not applied, logging to <out>/<name>.log.
	cmd := exec.Command(testBin, "schedule", "--", "-out", out, "-auto")
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("valid -- export job refused: %v", err)
	}
	for _, want := range []string{"-auto", "-log", "NOT applied", "mailarchive-"} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("preview lacks %q:\n%s", want, stdout)
		}
	}

	// Valid graph job with a proper secret file.
	secret := filepath.Join(t.TempDir(), "graph.secret")
	if err := os.WriteFile(secret, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(testBin, "schedule", "-interval", "weekly", "--", "graph", "-out", out, "-tenant", "t", "-client-id", "c", "-mailbox", "m@x", "-client-secret-file", secret)
	stdout, err = cmd.Output()
	if err != nil {
		t.Fatalf("valid -- graph job refused: %v", err)
	}
	for _, want := range []string{"graph", "-client-secret-file", "-mailbox m@x", "NOT applied"} {
		if !strings.Contains(string(stdout), want) {
			t.Errorf("graph preview lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(string(stdout), "s3cret") {
		t.Error("the secret itself leaked into the preview")
	}
}
