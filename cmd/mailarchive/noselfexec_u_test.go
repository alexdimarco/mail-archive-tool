package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-612, R33, S59
// No headless verb starts a copy of the program: recording fakes named like the
// program and its former names, on PATH, are never invoked when run-job runs;
// invoking a fake by hand records it (the positive twin, proving the recorder
// works). POSIX only — the fakes are /bin/sh scripts; the Windows case is a lab item.
func TestMA612_U_NoFakeCopyInvoked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the recording fakes are /bin/sh scripts; Windows is a lab item (L-W)")
	}
	dir := t.TempDir()
	records := map[string]string{}
	for _, name := range []string{"mailarchive", "mailarchivew", "mailarchive-gui", "mailarchive-desktop"} {
		rec := filepath.Join(dir, name+".invoked")
		records[name] = rec
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho invoked >> "+rec+"\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A job whose -out is absent, so run-job fails fast without a real archive;
	// the point is only what it starts, not that it succeeds.
	jobPath := filepath.Join(dir, "job.json")
	if err := os.WriteFile(jobPath, []byte(`{"version":1,"out":"`+filepath.Join(dir, "nope")+`","mode":"incremental"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(testBin, "run-job", jobPath)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_ = cmd.Run() // a refused run-job exits non-zero; we only inspect what it started

	for name, rec := range records {
		if _, err := os.Stat(rec); err == nil {
			t.Errorf("run-job invoked a copy of the program named %q — it must never start itself (R33, MA-612)", name)
		}
	}

	// Positive twin: a hand-invoked fake IS recorded, so the absence above is a
	// real "never invoked", not a broken recorder.
	if err := exec.Command(filepath.Join(dir, "mailarchive")).Run(); err != nil {
		t.Fatalf("running the fake by hand: %v", err)
	}
	if _, err := os.Stat(records["mailarchive"]); err != nil {
		t.Errorf("the recorder did not record a hand-invoked fake: %v", err)
	}
}
