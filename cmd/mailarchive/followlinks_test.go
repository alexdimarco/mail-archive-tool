package main

import (
	"flag"
	"reflect"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// covers: MA-322, R25, R10, S47
// The directory-link opt-in on the command line (owner ruling OR6, following X4 and X5):
// -follow-links names an -input, repeatable and comma-separated like -input; it travels
// into app.Options and into the scheduled job (so a schedule keeps reading the links),
// and an opt-in that names no input is refused naming the flag, because it would
// otherwise be read as no opt-in at all. The twin: the same job without it carries no
// such flag.
func TestFollowLinksOptInIsCarriedThroughTheCLI(t *testing.T) {
	store, other, out := t.TempDir(), t.TempDir(), t.TempDir()

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	o := exportFlags(fs)
	if err := fs.Parse([]string{"-follow-links", "a,b", "-follow-links", "c", "-out", out}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual([]string(o.followLinks), want) {
		t.Errorf("-follow-links parsed as %v; want %v", o.followLinks, want)
	}

	plain, err := parseJob([]string{"-out", out, "-input", store})
	if err != nil {
		t.Fatal(err)
	}
	if jobHasFlag(plain.args, "-follow-links") {
		t.Errorf("a job without the opt-in carries it: %v", plain.args)
	}
	j, err := parseJob([]string{"-out", out, "-input", store, "-follow-links", store})
	if err != nil {
		t.Fatalf("a job with the opt-in: %v", err)
	}
	want := []string{"-follow-links", abspath(store)}
	found := false
	for i := range j.args {
		if i+1 < len(j.args) && j.args[i] == want[0] && j.args[i+1] == want[1] {
			found = true
		}
	}
	if !found {
		t.Errorf("the scheduled job %v does not carry %v: the schedule would stop reading the links", j.args, want)
	}

	code, msg := parseJobRefusal([]string{"-out", out, "-input", store, "-follow-links", other})
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-follow-links", other, "-input"))

	// The flat form `schedule` re-assembles carries it too.
	fs2 := flag.NewFlagSet("t", flag.ContinueOnError)
	o2 := exportFlags(fs2)
	if err := fs2.Parse([]string{"-out", out, "-input", store, "-follow-links", store}); err != nil {
		t.Fatal(err)
	}
	flat := flatJobArgs(o2, nil)
	if !jobHasFlag(flat, "-follow-links") {
		t.Errorf("the flat job %v lost the opt-in", flat)
	}
	if opts := exportOptions(o2, []string{store}, export.Incremental, time.Time{}); !reflect.DeepEqual(opts.FollowLinks, []string{store}) {
		t.Errorf("app.Options.FollowLinks = %v; want the opted input", opts.FollowLinks)
	}
}
