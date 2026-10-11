package app

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-420, MA-427, R29, R12, S52
// A size claimed by a manifest handed over is not what the plan believes (E4-AO3): the plan's
// estimate is the bytes the files weigh on disk, so two records that claim 4 EiB each neither
// overflow the sum (it went to -9.2e18 with no warning) nor make the run stop with "the destination
// is full" before a byte is read; each of them is a listed failure whose file does not match the
// size the archive recorded, the four good messages are exported, and no run-fatal R-g is raised
// for a message that is not the disk's doing.
func TestArchiveForgedSizeClaimsNeitherOverflowTheEstimateNorStopTheRun(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	hello, second := a.key("hello"), a.key("second")
	a.setRec("hello", func(r *state.Record) { r.Fixity.EML.Size = 1 << 62 })
	a.setRec("second", func(r *state.Record) { r.Fixity.EML.Size = 1 << 62 })
	s := a.open()
	p := a.plan(e, s, allKeys(s))
	if p.EstimateBytes < 0 || p.EstimateBytes > 8<<20 {
		t.Errorf("the plan's estimate is %d bytes for six messages of a few KB: the two forged sizes were summed (an int64 overflow gives a negative number)", p.EstimateBytes)
	}
	for _, m := range p.Messages {
		if m.SizeHint > 1<<30 {
			t.Errorf("%s carries the size hint %d: the archive's recorded size was taken for what the file weighs", m.Subject, m.SizeHint)
		}
	}
	res := a.run(e, p, s)
	if res.Stop != nil {
		t.Fatalf("the run stopped %s (%s) before a byte was read: a forged size is not the disk's doing", res.Stop.RID, res.Label)
	}
	if res.Counts.Exported != 4 || res.Counts.Failed != 2 || res.Counts.NotAttempted != 0 {
		t.Errorf("counts %+v: want the four good messages exported and the two forged records listed", res.Counts)
	}
	man := manifestOf(t, res.Root)
	for _, key := range []string{hello, second} {
		f := failureListed(man, key)
		if f == nil || f["class"] != "terminal" || !strings.Contains(f["reason"].(string), "doesn't match the checksum the archive recorded") {
			t.Errorf("the forged record %q is listed as %v, want a terminal failure that says the file does not match", key, f)
		}
	}
}

// covers: MA-420, MA-427, R29, R12, S52
// A file whose own size is above the per-message cap (a sparse .eml with no recorded checksum) is
// listed as larger than this version of MailArchive exports and the run goes on: cap before
// budget, as for a message of Microsoft 365 (MA-427). It is not counted in the plan's estimate,
// the preview says so, and it never stops the run with "the destination is full" (E4-AO3).
func TestArchiveSparseOriginalAboveTheCapIsListedAndTheRunGoesOn(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	hello := a.key("hello")
	a.setRec("hello", func(r *state.Record) { r.Fixity.EML = nil })
	_, eml, _ := a.files("hello")
	if err := os.Truncate(eml, 3<<40); err != nil {
		t.Skipf("no sparse files here: %v", err)
	}
	s := a.open()
	p := a.plan(e, s, allKeys(s))
	if p.EstimateBytes < 0 || p.EstimateBytes > 8<<20 {
		t.Errorf("the plan's estimate is %d bytes: a message the run will refuse for its size was counted", p.EstimateBytes)
	}
	warned := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "larger than this version of MailArchive exports") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("the preview does not say a message is above what is exported: %v", p.Warnings)
	}
	res := a.run(e, p, s)
	if res.Stop != nil {
		t.Fatalf("the run stopped %s (%s): a message above the cap is listed, never the disk's doing", res.Stop.RID, res.Label)
	}
	if res.Counts.Exported != 5 || res.Counts.Failed != 1 {
		t.Errorf("counts %+v: want five exported and the sparse one listed", res.Counts)
	}
	if f := failureListed(manifestOf(t, res.Root), hello); f == nil || f["class"] != "terminal" || !strings.Contains(f["reason"].(string), "larger than this version of MailArchive exports") {
		t.Errorf("the sparse original is listed as %v", f)
	}
}
