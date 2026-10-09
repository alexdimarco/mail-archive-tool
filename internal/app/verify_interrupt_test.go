package app

import (
	"os"
	"path/filepath"
	"sort"
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

	rep := assessOut(out)
	if rep.Posture != "RED" || reasonFor(rep, "not_attested") == "" {
		t.Fatalf("an unfinished verify erased the last verdict: posture %s %v", rep.Posture, rep.Reasons)
	}
	assure.Reached(t, reasonFor(rep, "verify_never_finished"), "the verify_never_finished reason for a verify that died partway")
	if _, err := os.Stat(filepath.Join(out, state.IntegrityAttentionName)); err != nil {
		t.Errorf("the integrity sidecar did not survive the unfinished verify: %v", err)
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
