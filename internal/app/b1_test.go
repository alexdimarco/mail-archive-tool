package app

import (
	"strings"
	"testing"
)

// covers: MA-111, R18, S29
// canonicalJob reconstructs a run's export flags in schedulable form so the
// last-run record can carry the job that made the archive (status shapes its
// "keep it current" remedy from it). A run with no local source — a Graph run
// or a bare run — records no job, so status falls back to a generic phrase.
func TestCanonicalJob(t *testing.T) {
	job := canonicalJob(Options{Out: "/a", Inputs: []string{"/a/x.pst"}, Auto: true, CopyFirst: true, KeepRaw: true}, "incremental")
	got := strings.Join(job, " ")
	for _, want := range []string{"-out /a", "-mode incremental", "-auto", "-input /a/x.pst", "-copy-first", "-raw"} {
		if !strings.Contains(got, want) {
			t.Errorf("canonicalJob missing %q: %q", want, got)
		}
	}
	if job := canonicalJob(Options{Out: "/a"}, "incremental"); job != nil {
		t.Errorf("canonicalJob for a source-less run = %v, want nil", job)
	}
}
