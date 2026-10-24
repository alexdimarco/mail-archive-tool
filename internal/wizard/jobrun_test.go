package wizard

import (
	"io"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
)

// covers: MA-580
// optsForJob maps a job's recorded answers to the archive run's options, shared
// by the scheduled run (RunHeadless) and Run now (RunJobInProcess). With no
// Thunderbird pre-sync recorded, the BeforeRead hook stays nil — the run is what
// it has always been (the off half of S53); this holds on every platform.
func TestOptsForJobMapsFieldsAndNoHookWhenOff(t *testing.T) {
	j := job.Job{
		Name: "x", Out: "/o", Mode: "full", Auto: true, CopyFirst: true,
		KeepRaw: true, FollowLinks: []string{"/in/a"},
	}
	opts := optsForJob(j, []string{"/in/a", "/in/b"}, io.Discard)
	if opts.Out != "/o" || opts.Mode != export.Full || !opts.Auto || !opts.CopyFirst ||
		!opts.KeepRaw || !opts.Unattended || !opts.Index || !opts.Pages {
		t.Errorf("optsForJob mapped the common fields wrong: %+v", opts)
	}
	if len(opts.Inputs) != 2 || opts.Inputs[0] != "/in/a" {
		t.Errorf("optsForJob inputs = %v, want the passed inputs", opts.Inputs)
	}
	if len(opts.FollowLinks) != 1 || opts.FollowLinks[0] != "/in/a" {
		t.Errorf("optsForJob FollowLinks = %v", opts.FollowLinks)
	}
	if opts.BeforeRead != nil {
		t.Error("a job with no Thunderbird pre-sync must leave BeforeRead nil")
	}
}
