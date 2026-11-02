package archdelete

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// recordValueOf is the value of the record line key ("" when it has none).
func recordValueOf(rec, key string) string {
	for _, l := range strings.Split(rec, "\n") {
		if v, ok := strings.CutPrefix(l, key+": "); ok {
			return v
		}
	}
	return ""
}

// covers: MA-1026, R36
// Finishing a part-way delete says so (the 1.5.3 friction review's D1): when the folder has a
// delete in deletions.log that did not finish, the plan begins "This finishes the delete begun
// DATE." (FinishesLine: the terminal's and the panel's first line), the saved plan's second line
// is "Finishes: the delete begun DATE (run FIRST)", and the record of the run that finishes it
// names the first run and carries what the earlier runs removed beside its own — through a second
// part-way run too, and "not known in full" for a run that stopped with no finished line.
// Positive twin first: an ordinary delete says none of it.
func TestFinishingAPartWayDeleteSaysSoAndCarriesItsTotals(t *testing.T) {
	// Positive twin: an ordinary delete.
	twin := newRig(t, smallArchive(t, t.TempDir()))
	p := twin.plan(t)
	if p.Finishes != nil || p.FinishesLine() != "" || strings.Contains(string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p})), "Finishes:") {
		t.Fatalf("an ordinary delete says it finishes one: %+v", p.Finishes)
	}

	dir := smallArchive(t, t.TempDir())
	writeFile(t, dir+"/README.txt", "the archive's readme") // one more of the archive's files
	r := newRig(t, dir)
	first := time.Date(2026, 11, 1, 22, 5, 0, 0, time.UTC)
	r.x.Now = func() time.Time { return first }
	// Run 1 stops part way: the message page cannot be removed.
	held := true
	faultOn(t, nil, func(rel string) error {
		if held && strings.HasSuffix(rel, ".html") {
			return errors.New("The process cannot access the file")
		}
		return nil
	})
	res1 := r.run(t, false)
	if res1.Status != StatusPartial || !res1.Unfinished || res1.Removed == 0 {
		t.Fatalf("run 1 should stop part way having removed something: %s (removed %d)", res1.Message(), res1.Removed)
	}
	firstRun, _ := logLines(t, r.x.Log)[0]["run"].(string)

	// Run 2 also stops part way (still held).
	later := first.Add(time.Hour)
	r.x.Now = func() time.Time { return later }
	p2 := r.plan(t)
	wantLine := "This finishes the delete begun " + localStamp(first) + "."
	if p2.FinishesLine() != wantLine || p2.Finishes.FirstRun != firstRun {
		t.Fatalf("run 2's plan: %q (first run %q), want %q (%q)", p2.FinishesLine(), p2.Finishes.FirstRun, wantLine, firstRun)
	}
	saved := string(RecordText(RecordInput{Status: StatusPlanned, When: later, Plan: p2}))
	lines := strings.Split(saved, "\n")
	if lines[1] != "Finishes: the delete begun "+localStamp(first)+" (run "+firstRun+")" ||
		recordValueOf(saved, "Files removed earlier") != earlier(res1.Removed, res1.Bytes) {
		t.Fatalf("run 2's saved plan:\n%s", saved)
	}
	res2 := Execute(p2, p2.Name, false, r.x)
	if res2.Status != StatusPartial {
		t.Fatalf("run 2: %s", res2.Message())
	}

	// Run 3 finishes it: its record names the first run and carries runs 1 and 2's totals.
	held = false
	p3 := r.plan(t)
	if p3.Finishes == nil || p3.Finishes.FirstRun != firstRun || p3.FinishesLine() != wantLine {
		t.Fatalf("run 3's plan: %+v", p3.Finishes)
	}
	res3 := Execute(p3, p3.Name, false, r.x)
	rec := string(res3.Record)
	earlierN, earlierB := res1.Removed+res2.Removed, res1.Bytes+res2.Bytes
	if res3.Status != StatusComplete ||
		recordValueOf(rec, "Finishes") != "the delete begun "+localStamp(first)+" (run "+firstRun+")" ||
		recordValueOf(rec, "Files removed earlier") != earlier(earlierN, earlierB) ||
		recordValueOf(rec, "Files removed") != earlier(res3.Removed, res3.Bytes) ||
		recordValueOf(rec, "Files removed in all") != earlier(earlierN+res3.Removed, earlierB+res3.Bytes) {
		t.Fatalf("run 3's record (%s):\n%s", res3.Status, rec)
	}

	// A run that stopped with no finished line (a crash): its count is not known.
	d2 := smallArchive(t, t.TempDir())
	r2 := newRig(t, d2)
	p = r2.plan(t)
	appendRawLog(t, r2.x.Log, map[string]any{"at": first.Format(time.RFC3339Nano), "event": "started", "run": "crashedrun", "folder": d2, "row": d2,
		"identity": map[string]any{"volume": p.Identity.Volume, "file_id": p.Identity.FileID}, "kind": "plain"})
	p = r2.plan(t)
	// Nothing of it known: "not known", never "at least 0" (MA-1037, the 1.5.3 re-walk's N2).
	const notKnown = "not known: a run stopped before it wrote its record"
	if p.Finishes == nil || p.Finishes.FirstRun != "crashedrun" || p.earlierValue() != notKnown {
		t.Fatalf("after a crash: %+v %q", p.Finishes, p.earlierValue())
	}
	if res := Execute(p, p.Name, false, r2.x); res.Status != StatusComplete || recordValueOf(string(res.Record), "Files removed earlier") != notKnown ||
		strings.Contains(string(res.Record), "Files removed in all") {
		t.Fatalf("after a crash, the record:\n%s", res.Record)
	}
}

// covers: MA-1026, R36
// "Not known in full" is carried through a chain (the 1.5.3 friction fixes' adversarial pass,
// finding 6): a run that crashed with no finished line, then a run that stops part way, then the
// run that finishes the delete — the last record still says the earlier count is "not known in
// full" and gives no "Files removed in all", since the crashed run's count was never written. The
// part-way run's started line carries `earlier_unknown` forward. Positive twin first: the same
// chain with no crash gives an exact "Files removed in all".
func TestNotKnownInFullIsCarriedThroughAChain(t *testing.T) {
	first := time.Date(2026, 11, 1, 22, 5, 0, 0, time.UTC)
	held := false
	faultOn(t, nil, func(rel string) error {
		if held && strings.HasSuffix(rel, ".html") {
			return errors.New("The process cannot access the file")
		}
		return nil
	})
	chain := func(crash bool) (*rig, *Result) {
		t.Helper()
		dir := smallArchive(t, t.TempDir())
		writeFile(t, dir+"/README.txt", "the archive's readme")
		r := newRig(t, dir)
		r.x.Now = func() time.Time { return first }
		if crash {
			p := r.plan(t)
			appendRawLog(t, r.x.Log, map[string]any{"at": first.Format(time.RFC3339Nano), "event": "started", "run": "crashedrun", "folder": dir, "row": dir,
				"identity": map[string]any{"volume": p.Identity.Volume, "file_id": p.Identity.FileID}, "kind": "plain"})
		}
		held = true
		if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished {
			t.Fatalf("the part-way run: %s", res.Message())
		}
		held = false
		res := r.run(t, false)
		if res.Status != StatusComplete {
			t.Fatalf("the finishing run: %s", res.Message())
		}
		return r, res
	}

	// Positive twin: no crash, an exact count in all.
	_, res := chain(false)
	if all := recordValueOf(string(res.Record), "Files removed in all"); all == "" || strings.Contains(string(res.Record), "not known in full") {
		t.Fatalf("the positive twin's record:\n%s", res.Record)
	}

	r, res := chain(true)
	rec := string(res.Record)
	if !strings.Contains(recordValueOf(rec, "Files removed earlier"), "not known in full") || strings.Contains(rec, "Files removed in all") ||
		!strings.Contains(recordValueOf(rec, "Finishes"), "(run crashedrun)") {
		t.Fatalf("after a crash and a part-way run, the record:\n%s", rec)
	}
	// The part-way run's started line carried it forward.
	var carried bool
	for _, l := range logLines(t, r.x.Log) {
		if l["event"] == "started" && l["run"] != "crashedrun" && l["first_run"] == "crashedrun" {
			carried = l["earlier_unknown"] == true
			break
		}
	}
	if !carried {
		t.Fatalf("the part-way run's started line does not carry earlier_unknown: %v", logLines(t, r.x.Log))
	}
}

// earlier is a count as the record words it.
func earlier(n int, b int64) string { return strings.TrimSpace(itoa(n) + " (" + size(b) + ")") }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}
