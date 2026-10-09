package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// flipHTMLByte modifies one archived .html in place (same length, so only its
// digest differs) and returns its path.
func flipHTMLByte(t *testing.T, out string) string {
	t.Helper()
	victim := assure.Reached(t, findSuffix(t, out, ".html"), "archived html files")[0]
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(victim, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return victim
}

func assessOut(out string) health.Report {
	return health.Assess(health.Gather(out, ""), time.Now())
}

// covers: MA-394, R18, R20, S31
// A verify that never finishes must not erase the last finished verdict (F79).
// After a verify finds the archive NOT intact (RED not_attested), a later
// verify that dies partway through — a panic inside it stands in for a crash,
// leaving exactly the on-disk state a kill or power loss leaves — keeps that
// RED on status and is itself flagged (verify_never_finished), and the
// integrity sidecar stays. A verify that then completes replaces both. Positive
// twin: the intact archive attests and carries neither code.
func TestInterruptedVerifyKeepsTheLastVerdict(t *testing.T) {
	out := tmpDir(t)
	buildFixityArchive(t, out, fixtureMsg{subject: "one"}, fixtureMsg{subject: "two"}, fixtureMsg{subject: "three"})

	if rep, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil || !rep.Attested() {
		t.Fatalf("intact archive not attested: %v %+v", err, rep)
	}
	if rep := assessOut(out); reasonFor(rep, "not_attested") != "" || reasonFor(rep, "verify_never_finished") != "" {
		t.Fatalf("intact archive flagged: %v", rep.Reasons)
	}

	flipHTMLByte(t, out)
	if rep, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil || rep.Attested() {
		t.Fatalf("damaged archive attested: %v %+v", err, rep)
	}
	assure.Reached(t, reasonFor(assessOut(out), "not_attested"), "the not_attested RED after a verify that found damage")

	crashVerify := func() {
		t.Helper()
		crashed := false
		func() {
			defer func() {
				if recover() != nil {
					crashed = true
				}
			}()
			_, _ = Verify(out, VerifyOptions{}, discard(), func(Report) { panic("simulated crash mid-verify") })
		}()
		if !crashed {
			t.Fatal("the simulated mid-verify crash did not happen")
		}
	}
	crashVerify()

	rep := assessOut(out)
	if rep.Posture != "RED" || reasonFor(rep, "not_attested") == "" {
		t.Fatalf("an unfinished verify erased the last verdict: posture %s %v", rep.Posture, rep.Reasons)
	}
	assure.Reached(t, reasonFor(rep, "verify_never_finished"), "the verify_never_finished reason for a verify that died partway")
	if _, err := os.Stat(filepath.Join(out, state.IntegrityAttentionName)); err != nil {
		t.Errorf("the integrity sidecar did not survive the unfinished verify: %v", err)
	}

	// The documented status surfaces carry the kept verdict too: `status
	// -json` reports the running record with a `previous` object holding the
	// last finished verdict (one level, never nested), and the human lines
	// say the verify never finished and show that verdict. A second crash
	// carries the same verdict forward.
	for crash := 1; crash <= 2; crash++ {
		if crash == 2 {
			crashVerify()
		}
		in := health.Gather(out, "")
		srep := health.Assess(in, time.Now())
		lv, prev := statusLastVerify(t, in, srep)
		if lv["status"] != state.VerifyRunning || lv["finished"] != nil {
			t.Fatalf("crash %d: status -json last_verify is %v, want the running record (finished null)", crash, lv)
		}
		if prev == nil || prev["status"] != state.VerifyDone || prev["modified"] != float64(1) || prev["attested"] != false || prev["finished"] == nil {
			t.Fatalf("crash %d: status -json last_verify.previous is %v, want the finished verdict (done, modified 1, not attested)", crash, prev)
		}
		if _, nested := prev["previous"]; nested {
			t.Fatalf("crash %d: last_verify.previous nests another previous: %v", crash, prev)
		}
		last, previous := summaryLine(in, srep, "Last verify:"), summaryLine(in, srep, "Previous verify:")
		if !strings.HasSuffix(last, "→ never finished") {
			t.Errorf("crash %d: Summary %q, want the verify shown as never finished", crash, last)
		}
		if !strings.Contains(previous, "→ NOT attested (modified 1, missing 0, unrecorded 0)") {
			t.Errorf("crash %d: Summary %q, want the previous NOT attested verdict", crash, previous)
		}
	}

	// The JSON builder itself flattens: even a record that arrived nested
	// yields one level of previous.
	in := health.Gather(out, "")
	if in.LastVerify.Previous == nil {
		t.Fatal("the gathered running record carries no previous verdict")
	}
	deeper := *in.LastVerify.Previous
	in.LastVerify.Previous.Previous = &deeper
	if _, prev := statusLastVerify(t, in, health.Assess(in, time.Now())); prev == nil {
		t.Fatal("last_verify.previous missing for a nested record")
	} else if _, nested := prev["previous"]; nested {
		t.Fatalf("status -json nests previous inside previous: %v", prev)
	}

	// A verify that completes is the new verdict: still damaged, no longer
	// "never finished".
	if _, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	rep = assessOut(out)
	if reasonFor(rep, "not_attested") == "" || reasonFor(rep, "verify_never_finished") != "" {
		t.Fatalf("a completed verify did not become the verdict: %v", rep.Reasons)
	}
	in = health.Gather(out, "")
	rep = health.Assess(in, time.Now())
	if lv, prev := statusLastVerify(t, in, rep); lv["status"] != state.VerifyDone || prev != nil {
		t.Fatalf("after a completed verify, status -json last_verify is %v (previous %v), want the done record with no previous", lv, prev)
	}
	if p := summaryLine(in, rep, "Previous verify:"); p != "" || !strings.Contains(summaryLine(in, rep, "Last verify:"), "→ NOT attested (modified 1") {
		t.Fatalf("after a completed verify the Summary shows %q / %q, want only the new NOT attested verdict", summaryLine(in, rep, "Last verify:"), p)
	}
}

// statusLastVerify returns `status -json`'s last_verify object as a consumer
// decodes it, and its optional `previous` object (nil when absent).
func statusLastVerify(t *testing.T, in health.Input, rep health.Report) (map[string]any, map[string]any) {
	t.Helper()
	data, err := json.Marshal(health.JSON(in, rep))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	lv, ok := doc["last_verify"].(map[string]any)
	if !ok {
		t.Fatalf("status -json has no last_verify object:\n%s", data)
	}
	prev, _ := lv["previous"].(map[string]any)
	return lv, prev
}

// summaryLine returns the Summary line starting with prefix, or "".
func summaryLine(in health.Input, rep health.Report, prefix string) string {
	for _, l := range health.Summary(in, rep) {
		if strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}

// covers: MA-395, R18, R20, S31
// A verify that checks every file but then errors (a `-record` baseline whose
// manifest save fails — fault-injected by putting a directory where the
// manifest is renamed to) records the verdict it reached, not zeros (F79): the
// modified file it found keeps status RED not_attested, and the failure itself
// is a WARN (verify_failed) naming the re-run. Positive twin: the same verify
// without the fault saves its baseline and errors nowhere.
func TestErroringVerifyRecordsItsVerdict(t *testing.T) {
	// build returns an archive of two records: the first (by key) predates
	// fixity, so -record has something to baseline; the second carries its
	// digests, and its archived html path is returned.
	build := func() (string, string, string) {
		out := tmpDir(t)
		buildFixityArchive(t, out, fixtureMsg{subject: "one"}, fixtureMsg{subject: "two"})
		mpath := filepath.Join(out, ".mailarchive-manifest.json")
		m, err := state.Load(mpath)
		if err != nil {
			t.Fatal(err)
		}
		all := m.All()
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) != 2 {
			t.Fatalf("fixture has %d records, want 2", len(keys))
		}
		legacy := all[keys[0]]
		legacy.Fixity = nil
		m.Add(keys[0], legacy)
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		return out, mpath, filepath.Join(out, filepath.FromSlash(all[keys[1]].Path))
	}

	okOut, _, _ := build()
	if rep, err := Verify(okOut, VerifyOptions{Record: true}, discard(), nil); err != nil || rep.Recorded == 0 {
		t.Fatalf("positive twin: verify -record err=%v recorded=%d", err, rep.Recorded)
	}
	if rep := assessOut(okOut); reasonFor(rep, "verify_failed") != "" {
		t.Fatalf("a successful verify -record flagged verify_failed: %v", rep.Reasons)
	}

	out, mpath, digested := build()
	// Damage the record that HAS fixity, so a full check finds it modified.
	data, err := os.ReadFile(digested)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(digested, data, 0o644); err != nil {
		t.Fatal(err)
	}
	injected := false
	_, verr := Verify(out, VerifyOptions{Record: true}, discard(), func(Report) {
		if injected {
			return
		}
		injected = true
		// The manifest is already loaded; make its final rename fail.
		if err := os.Rename(mpath, mpath+".aside"); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(mpath, "blocker"), 0o755); err != nil {
			t.Fatal(err)
		}
	})
	_ = os.RemoveAll(mpath)
	_ = os.Rename(mpath+".aside", mpath)
	if verr == nil {
		t.Fatal("the injected manifest-save fault did not make verify -record error")
	}

	rep := assessOut(out)
	if rep.Posture != "RED" || reasonFor(rep, "not_attested") == "" {
		t.Fatalf("an erroring verify erased the modified file it found: posture %s %v", rep.Posture, rep.Reasons)
	}
	assure.Reached(t, reasonFor(rep, "verify_failed"), "the verify_failed reason for a verify that errored")
}
