package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
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
