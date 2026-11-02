package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
)

// uninstallHome is a throwaway home for one uninstall run of the real program: every OS's
// per-user folders inside it and, off Windows, a PATH holding no program, so no real crontab is
// read and no real scheduler entry can be found or removed. It returns the child's environment
// and the settings folder MailArchive uses in that home.
func uninstallHome(t *testing.T) (env []string, cfgDir string) {
	t.Helper()
	home := t.TempDir()
	vars := map[string]string{"HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"XDG_STATE_HOME": filepath.Join(home, ".local", "state"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"),
		"APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local")}
	if runtime.GOOS != "windows" {
		vars["PATH"] = t.TempDir()
	}
	env = os.Environ()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	switch runtime.GOOS {
	case "darwin":
		cfgDir = filepath.Join(home, "Library", "Application Support", "mailarchive")
	case "windows":
		cfgDir = filepath.Join(vars["APPDATA"], "mailarchive")
	default:
		cfgDir = filepath.Join(vars["XDG_CONFIG_HOME"], "mailarchive")
	}
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return env, cfgDir
}

// runUninstallIn runs the real program's uninstall with args in env: its exit code, standard
// output and standard error.
func runUninstallIn(t *testing.T, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(testBin, append([]string{"uninstall"}, args...)...)
	cmd.Env = env
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("uninstall did not run: %v", err)
	}
	return code, out.String(), errb.String()
}

func writeOwnerOnly(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileIs(path, body string) bool {
	b, err := os.ReadFile(path)
	return err == nil && string(b) == body
}

func pathGone(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// covers: MA-1031, R36
// `uninstall -yes` refuses, removing nothing, while deletions.log holds any line, naming the log,
// where to copy it and `uninstall -yes -log-copied` (the 1.5.3 friction review's D2, the main
// session's decision); with -log-copied it goes ahead and removes the log. Without -yes nothing
// changes: `uninstall -dry-run` shows the plan and `uninstall` asks for -yes, as before. Each
// run is the real program in a throwaway home. Positive twin first: a deletion log holding no
// line is removed by -yes alone.
func TestUninstallYesRefusesUntilTheDeletionLogIsCopied(t *testing.T) {
	// Positive twin: an empty log, -yes goes ahead.
	env, cfg := uninstallHome(t)
	settings, log := filepath.Join(cfg, "desktop-settings.json"), filepath.Join(cfg, "deletions.log")
	writeOwnerOnly(t, settings, "{}")
	writeOwnerOnly(t, log, "")
	code, out, errs := runUninstallIn(t, env, "-yes")
	if code != 0 || !pathGone(log) || !pathGone(settings) || !strings.Contains(out, "This run removes it: it lists no deletes.") {
		t.Fatalf("positive twin: -yes removes a deletion log holding no line: exit %d, log gone %v, settings gone %v\n%s%s", code, pathGone(log), pathGone(settings), out, errs)
	}

	// A log holding a line.
	env, cfg = uninstallHome(t)
	settings, log = filepath.Join(cfg, "desktop-settings.json"), filepath.Join(cfg, "deletions.log")
	const line = `{"at":"2026-11-01T10:00:00Z","event":"started","run":"r1","folder":"/gone/Old mail","row":"/gone/Old mail","kind":"plain"}` + "\n" +
		`{"at":"2026-11-01T10:00:02Z","event":"finished","run":"r1","folder":"/gone/Old mail","outcome":"complete"}` + "\n"
	writeOwnerOnly(t, settings, "{}")
	writeOwnerOnly(t, log, line)
	untouched := func() bool { return fileIs(log, line) && fileIs(settings, "{}") }

	// Without -yes nothing changes.
	code, out, errs = runUninstallIn(t, env, "-dry-run")
	if code != 0 || !untouched() || !strings.Contains(out, "  - the deletion log: "+log+"\n") || strings.Contains(errs, "-log-copied") {
		t.Fatalf("uninstall -dry-run shows the plan and changes nothing: exit %d\n%s%s", code, out, errs)
	}
	code, _, errs = runUninstallIn(t, env)
	if code != 1 || !untouched() || !strings.Contains(errs, "Re-run with -yes to proceed") {
		t.Fatalf("uninstall without -yes asks for it and changes nothing: exit %d\n%s", code, errs)
	}

	// -yes: refused, nothing removed.
	code, out, errs = runUninstallIn(t, env, "-yes")
	assure.Refused(t, code, errs, assure.Code(1),
		// The path quoted, so a pasted copy command works (MA-1041, the 1.5.3 re-walk's N10).
		assure.Names("mailarchive: MailArchive's deletion log, "+encmark.CommandPath(log)+", lists the deletes made on this computer", "so nothing was removed",
			"Copy it somewhere you keep records", "then run mailarchive uninstall -yes -log-copied."),
		assure.NoSideEffect(func() bool {
			return untouched() && !strings.Contains(out, "Done —") && strings.Contains(out, "Copy deletions.log first")
		}))

	// -yes -log-copied: goes ahead, the log removed.
	code, out, errs = runUninstallIn(t, env, "-yes", "-log-copied")
	if code != 0 || !pathGone(log) || !pathGone(settings) ||
		!strings.Contains(out, "This run removes it: you said you have copied it (-log-copied).") ||
		!strings.Contains(out, "The deletion log was removed: "+log) {
		t.Fatalf("-yes -log-copied removes the deletion log: exit %d, log gone %v, settings gone %v\n%s%s", code, pathGone(log), pathGone(settings), out, errs)
	}
}
