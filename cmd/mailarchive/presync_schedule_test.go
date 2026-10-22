package main

import (
	"flag"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-569
// A scheduled export carries the Thunderbird pre-sync through the flat form and
// into the canonical, absolute job, so the installed schedule's argv runs the
// sync and job.presync is set.
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

// covers: MA-550
// Schedule-time refusals (§4.2.4): -thunderbird-profile without -thunderbird-sync
// is refused, and an hourly schedule with the sync is refused (Q11) while daily
// or a sync-less hourly is allowed.
func TestScheduledThunderbirdSyncRefusals(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()

	code, msg := parseJobRefusal([]string{"-out", out, "-input", store, "-thunderbird-profile", store})
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-thunderbird-profile", "-thunderbird-sync"))

	if err := refusePresyncHourly(schedule.Hourly, job{presync: true}); err == nil {
		t.Error("hourly + -thunderbird-sync should be refused (Q11)")
	}
	if err := refusePresyncHourly(schedule.Daily, job{presync: true}); err != nil {
		t.Errorf("daily + -thunderbird-sync should be allowed: %v", err)
	}
	if err := refusePresyncHourly(schedule.Hourly, job{presync: false}); err != nil {
		t.Errorf("hourly without the sync should be allowed: %v", err)
	}
}
