package schedule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
)

// covers: MA-631, MA-632, R14, R18, S59
// The §4.6.5c guard ENFORCES at installOn: a non-verify install of a kind that
// owner-side state does not corroborate is refused before any job file, wrapper or
// scheduler entry is written (assure.Refused, the healthy twin first); a same-kind
// install proceeds (R14, one entry). A forged descriptor Job cannot suppress it —
// the evidence is the owner's run-job job file, not the Job, so a capture install
// whose Job claims capture is still refused while a run-job is corroborated
// (MA-632's suppression case).
func TestMA631_InstallGuardRefusesCrossKind(t *testing.T) {
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	var writes []string
	crontabList = func() (string, error) { return "", nil }
	crontabInstall = func(c string) error { writes = append(writes, c); return nil }

	// Redirect job.Dir into a tempdir and seed a run-job job file for the archive,
	// so owner-side state corroborates run-job.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	jp := filepath.Join(dir, DefaultNameFor(out)+".json")
	jb, _ := json.Marshal(map[string]any{"version": 1, "out": out})
	if err := os.WriteFile(jp, jb, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(jp, 0o600)

	// Healthy twin first: the run-job install (its own kind) proceeds — one entry.
	runjob := Spec{Name: DefaultNameFor(out), Interval: Daily, At: "02:00", Exe: "/usr/bin/mailarchive", Args: []string{"-job", jp}, Out: out}
	if err := installOn(runjob, "linux"); err != nil {
		t.Fatalf("healthy twin: the run-job install (its corroborated kind) was refused: %v", err)
	}
	if len(writes) != 1 {
		t.Fatalf("healthy twin: want one scheduler write, got %d", len(writes))
	}
	if got := recordedKind(out); got != KindRunJob {
		t.Fatalf("healthy twin: recorded kind %q, want run-job", got)
	}

	// Cross-kind + forged-Job suppression: a capture install (its Job claims
	// capture) under the same archive is refused while a run-job is corroborated;
	// nothing further is written and the run-job descriptor is unchanged.
	writes = nil
	capture := Spec{Name: DefaultNameFor(out), Interval: Daily, At: "02:00", Exe: "/usr/bin/mailarchive", Args: []string{"-capture", "-out", out}, Out: out}
	code, msg := refusal(installOn(capture, "linux"))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("run-job", out))
	if len(writes) != 0 {
		t.Errorf("a refused cross-kind install still wrote to the scheduler: %v", writes)
	}
	if got := recordedKind(out); got != KindRunJob {
		t.Errorf("the refused install changed the recorded kind to %q (want run-job unchanged)", got)
	}
}
