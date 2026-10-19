package schedule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/job"
)

// covers: MA-631, R14, R18, S59
// KindEvidence corroborates kinds from owner-side state ONLY (§4.6.5b): a run-job
// from a securely-read job file whose Out is the same archive; capture from the
// program's desktop-config predicate. A job file for a DIFFERENT archive does not
// corroborate run-job for this one, and an archive with no owner-side state
// corroborates nothing — the evidence never comes from a descriptor's Job.
func TestMA631_KindEvidenceCorroboration(t *testing.T) {
	// job.Dir() uses os.UserConfigDir(); redirect it into a tempdir.
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
	writeJob := func(name, forOut string) string {
		p := filepath.Join(dir, name+".json")
		b, _ := json.Marshal(map[string]any{"version": 1, "out": forOut})
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(p, 0o600) // umask-proof: readSecure refuses group/other bits
		return p
	}

	out := t.TempDir()
	wantPath := writeJob(DefaultNameFor(out), out) // the run-job for our archive
	writeJob("mailarchive-elsewhere", t.TempDir()) // a run-job for a DIFFERENT archive

	origCC := CaptureCorroborator
	t.Cleanup(func() { CaptureCorroborator = origCC })
	CaptureCorroborator = func(o string) bool { return SameArchivePath(o, out, runtime.GOOS) }

	ev := BuildKindEvidence(out)
	if !ev.Has(KindRunJob) || ev.JobPath != wantPath {
		t.Errorf("run-job: Has=%v JobPath=%q, want true and %q", ev.Has(KindRunJob), ev.JobPath, wantPath)
	}
	if !ev.Has(KindCapture) {
		t.Error("capture should be corroborated when the desktop-config predicate holds")
	}

	// A different archive (the predicate matches only `out`) and no job file
	// corroborates nothing.
	if ev2 := BuildKindEvidence(t.TempDir()); ev2.Any() {
		t.Errorf("no owner-side state should corroborate nothing, got %+v", ev2.Corroborated)
	}
}
