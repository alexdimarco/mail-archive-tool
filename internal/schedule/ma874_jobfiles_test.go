package schedule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// isolateJobDir points the per-user config folder (job.Dir) at a fresh temporary folder and
// returns the job folder, so these tests never read or write the real one.
func isolateJobDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	dir, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// tokenCacheNames are the Microsoft 365 sign-in caches' file names, taken from their writers: the
// Desktop's (graphconfig.DefaultTokenCachePath) and the command line's, which adds the mailbox's
// hash to the same name (cmd/mailarchive defaultTokenCachePath; package main, so its rule is
// repeated here). Each is checked to be a name a schedule could have, so it guards something.
func tokenCacheNames(t *testing.T) []string {
	t.Helper()
	p, err := graphconfig.DefaultTokenCachePath("contoso.onmicrosoft.com", "11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(p)
	names := []string{base, strings.TrimSuffix(base, ".json") + "-" + util.ShortHash("alice@contoso.org") + ".json"}
	for _, n := range names {
		if _, err := SanitizeName(strings.TrimSuffix(n, ".json")); err != nil {
			t.Fatalf("fixture: %s is not a name a schedule could have (%v), so it guards nothing", n, err)
		}
	}
	return names
}

// writeAsJob writes, under the job folder, a file job.Read accepts as a job for out.
func writeAsJob(t *testing.T, dir, base, out string, withVersion bool) string {
	t.Helper()
	v := map[string]any{"name": "x", "out": out, "mode": "incremental"}
	if withVersion {
		v["version"] = job.Version
	}
	b, _ := json.Marshal(v)
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(p, 0o600) // umask-proof: the secure read refuses group/other bits
	return p
}

// covers: MA-874, MA-631, R14
// The kind evidence counts only real job files (MA-874): MailArchive's own other files in the
// job folder — the Desktop's settings, the Microsoft 365 configuration, the known archives, the
// last start failure, the Desktop's and a serve's instance records — are never taken for a run-job,
// even written so that job.Read accepts them; nor is a file named as a job but with no version
// (every job.Write records one), nor one whose name a schedule name can't have. Positive twins
// first: the default-named job file, and a job file under another schedule name, are counted. The
// names are taken from their writers, so the list stays in step with them.
func TestTheJobFolderOwnFilesAreNeverJobFiles(t *testing.T) {
	dir := isolateJobDir(t)
	origCC := CaptureCorroborator
	t.Cleanup(func() { CaptureCorroborator = origCC })
	CaptureCorroborator = nil

	out := t.TempDir()
	def := writeAsJob(t, dir, DefaultNameFor(out)+".json", out, true)
	if ev := BuildKindEvidence(out); !ev.Has(KindRunJob) || ev.JobPath != def {
		t.Fatalf("the positive twin: the default-named job file is not counted (%+v)", ev)
	}
	os.Remove(def)
	named := writeAsJob(t, dir, "nightly.json", out, true)
	if ev := BuildKindEvidence(out); !ev.Has(KindRunJob) || ev.JobPath != named {
		t.Fatalf("the positive twin: a job file under another schedule name is not counted (%+v)", ev)
	}
	os.Remove(named)

	settings, err := job.DesktopSettingsFile()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_, sessionFile := instance.Names("42")
	tokens := tokenCacheNames(t)
	own := append([]string{filepath.Base(settings), filepath.Base(graph), archindex.Name, "last-failure.json",
		instance.FileName, sessionFile, instance.ServeFileName(out), "Desktop-Settings.json"}, tokens...)
	for _, base := range own {
		if IsJobFileName(base) {
			t.Errorf("%s is named as a job file", base)
		}
		p := writeAsJob(t, dir, base, out, true)
		if _, err := job.Read(p); err != nil {
			t.Fatalf("fixture: %s does not parse as a job (%v), so it guards nothing", base, err)
		}
		if ev := BuildKindEvidence(out); ev.Has(KindRunJob) {
			t.Errorf("%s is counted as a run-job of %s (%s)", base, out, ev.JobPath)
		}
		os.Remove(p)
	}

	for _, base := range []string{DefaultNameFor(out) + ".json", "nightly.json"} {
		p := writeAsJob(t, dir, base, out, false)
		if ev := BuildKindEvidence(out); ev.Has(KindRunJob) {
			t.Errorf("%s, with no version, is counted as a run-job", base)
		}
		os.Remove(p)
	}
	for _, base := range []string{".hidden.json", "has space.json", "nightly.json.bak"} {
		if IsJobFileName(base) {
			t.Errorf("%q is named as a job file", base)
		}
	}
}

// covers: MA-874, R37
// RemoveJobFile deletes a job file and nothing else (MA-874): the Desktop's settings file, the
// Microsoft 365 configuration, or a file outside the job folder named like a job file is refused,
// named, and left as it was. Positive twin first: a real job file is deleted, and a missing one
// is not an error.
func TestRemoveJobFileRemovesOnlyJobFiles(t *testing.T) {
	dir := isolateJobDir(t)
	out := t.TempDir()
	jp := writeAsJob(t, dir, DefaultNameFor(out)+".json", out, true)
	if err := RemoveJobFile(jp); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	if _, err := os.Stat(jp); !os.IsNotExist(err) {
		t.Fatalf("the positive twin: the job file is still there (%v)", err)
	}
	if err := RemoveJobFile(jp); err != nil {
		t.Errorf("a missing job file: %v", err)
	}

	outside := writeAsJob(t, t.TempDir(), DefaultNameFor(out)+".json", out, true)
	refused := []string{
		writeAsJob(t, dir, "desktop-settings.json", out, true),
		writeAsJob(t, dir, "graph-config.json", out, true),
		outside,
	}
	for _, base := range tokenCacheNames(t) { // the sign-in caches (MA-874, the part A pass)
		refused = append(refused, writeAsJob(t, dir, base, out, true))
	}
	for _, p := range refused {
		before, _ := os.ReadFile(p)
		err := RemoveJobFile(p)
		msg := ""
		rc := 0
		if err != nil {
			msg, rc = err.Error(), 2
		}
		assure.Refused(t, rc, msg, assure.Names(p, "not a scheduled backup's job file"),
			assure.NoSideEffect(func() bool { after, rerr := os.ReadFile(p); return rerr == nil && string(after) == string(before) }))
	}
}
