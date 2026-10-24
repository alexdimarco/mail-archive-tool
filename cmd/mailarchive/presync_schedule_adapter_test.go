//go:build darwin || windows

package main

import (
	"flag"
	"testing"
)

// covers: MA-569
// A scheduled export carries the Thunderbird pre-sync through the flat form and
// into the canonical, absolute job, so the installed schedule's argv runs the
// sync and job.presync is set. Build-tagged to the platforms WITH a pre-sync
// adapter (macOS, Windows): only there is -thunderbird-sync accepted at parse
// time. On a platform without an adapter (Linux) parseJob refuses it — the twin
// TestScheduledJobRefusesThunderbirdSyncOffAdapter covers that.
func TestScheduledJobCarriesThunderbirdSync(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	o := exportFlags(fs)
	if err := fs.Parse([]string{"-out", out, "-input", store, "-thunderbird-sync", "-thunderbird-profile", store}); err != nil {
		t.Fatal(err)
	}
	flat := flatJobArgs(o, nil)
	if !jobHasFlag(flat, "-thunderbird-sync") || !jobHasFlag(flat, "-thunderbird-profile") {
		t.Errorf("the flat job %v lost the pre-sync flags", flat)
	}

	j, err := parseJob(flat)
	if err != nil {
		t.Fatalf("parseJob: %v", err)
	}
	if !j.presync {
		t.Error("job.presync is false; want true")
	}
	if !jobHasFlag(j.args, "-thunderbird-sync") || !jobHasFlag(j.args, "-thunderbird-profile") {
		t.Errorf("canonical job args %v lost the pre-sync flags", j.args)
	}
}
