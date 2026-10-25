package health

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-730, R37
// Status on an encrypted archive reads only what is keyless: its manifest, report,
// index and history are not read (here each is garbage that would RED or WARN a
// plaintext archive), the report says it is encrypted, and a failed last run is
// still RED. The positive twin: the same garbage without a vault is read and RED.
func TestStatusOnAnEncryptedArchiveReadsOnlyWhatIsKeyless(t *testing.T) {
	garbage := func(t *testing.T) string {
		out := t.TempDir()
		for _, name := range []string{".mailarchive-manifest.json", "search.db", "attachments-report.tsv", state.HistoryName} {
			if err := os.WriteFile(filepath.Join(out, name), []byte("not what it claims\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		started := time.Now().Add(-time.Hour).UTC()
		if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: started, Finished: started.Add(time.Minute)}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	plain := garbage(t)
	pin := Gather(plain, "")
	if r := Assess(pin, time.Now()); r.Posture != "RED" || !slices.Contains(r.Codes, "manifest_unreadable") || !pin.ReportExists || !pin.History.Exists {
		t.Fatalf("the positive twin: a plaintext archive's garbage files are read (manifest RED, report and history seen), got %s %v", r.Posture, r.Codes)
	}

	sealed := garbage(t)
	sealFolder(t, sealed)
	in := Gather(sealed, "")
	if !in.Encrypted || in.EncryptedWhy == "" || in.HasManifestFile || in.HasIndexFile || in.ReportExists || in.History.Exists || in.HistoryReadErr != nil {
		t.Fatalf("status read a sealed archive's files: %+v", in)
	}
	r := Assess(in, time.Now())
	if r.Posture == "RED" || !slices.Contains(r.Codes, "encrypted") {
		t.Fatalf("an encrypted archive must say so and not RED on its sealed files, got %s %v %v", r.Posture, r.Codes, r.Reasons)
	}

	in.LastRun.Status = state.RunFailed
	if r := Assess(in, time.Now()); r.Posture != "RED" || !slices.Contains(r.Codes, "run_failed") {
		t.Fatalf("a failed last run of an encrypted archive must still RED, got %s %v", r.Posture, r.Codes)
	}
}

// covers: MA-763, R37
// Status WARNs an encrypted archive whose last backup finished with its search index
// still missing messages, from the run record alone — the posture WARN, the reason the
// kind's fixed sentence — and says nothing of the index after a backup that left it
// whole (the positive twin) or after a failed one (its own RED).
func TestStatusWarnsAnEncryptedArchiveWhoseIndexIsBehind(t *testing.T) {
	sealed := t.TempDir()
	sealFolder(t, sealed)
	ok := time.Now().Add(-time.Hour).UTC()
	write := func(r state.LastRun) Report {
		t.Helper()
		r.Started, r.Finished, r.Encrypted = ok, ok.Add(time.Minute), true
		if err := state.WriteLastRun(sealed, r); err != nil {
			t.Fatal(err)
		}
		return Assess(Gather(sealed, ""), time.Now())
	}
	if r := write(state.LastRun{Status: state.RunOK}); slices.Contains(r.Codes, "index_behind") {
		t.Fatalf("the positive twin: a whole index is no fault, got %v", r.Codes)
	}
	r := write(state.LastRun{Status: state.RunOK, ErrorKind: state.NoteIndexBehind})
	if r.Posture != "WARN" || !slices.Contains(r.Codes, "index_behind") || !strings.Contains(strings.Join(r.Reasons, "\n"), state.FailureText(state.NoteIndexBehind)) {
		t.Fatalf("a backup that left its index behind must WARN in the kind's words, got %s %v %v", r.Posture, r.Codes, r.Reasons)
	}
	if r := write(state.LastRun{Status: state.RunFailed, ErrorKind: state.NoteIndexBehind}); slices.Contains(r.Codes, "index_behind") || r.Posture != "RED" {
		t.Fatalf("a failed run is its own RED, not an index WARN: %s %v", r.Posture, r.Codes)
	}
}

// sealFolder commits a vault into dir: an encrypted archive.
func sealFolder(t *testing.T, dir string) {
	t.Helper()
	p, err := vault.PrepareCreate(dir, "status test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
}

// covers: MA-748, R37
// Status still judges what needs no key on an encrypted archive (the Phase-2a
// adversarial pass, F15/F16): a run that never finished is RED, an unreadable run
// record is a WARN, a failed run says its kind's words; no missing schedule is a
// fault (an encrypted archive cannot be scheduled yet); and the summary says the
// counts and history are sealed instead of "no run has completed" or "exported 0".
// The positive twin: a plaintext archive's never-finished run is RED too.
func TestStatusJudgesAnEncryptedArchiveByWhatNeedsNoKey(t *testing.T) {
	old := time.Now().Add(-72 * time.Hour).UTC()
	plain := t.TempDir()
	if err := state.WriteLastRun(plain, state.LastRun{Status: state.RunRunning, Started: old}); err != nil {
		t.Fatal(err)
	}
	if r := Assess(Gather(plain, ""), time.Now()); !slices.Contains(r.Codes, "run_never_finished") {
		t.Fatalf("the positive twin: a plaintext never-finished run is RED, got %v", r.Codes)
	}

	sealed := t.TempDir()
	sealFolder(t, sealed)
	if err := state.WriteLastRun(sealed, state.LastRun{Status: state.RunRunning, Started: old, Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	if r := Assess(Gather(sealed, ""), time.Now()); r.Posture != "RED" || !slices.Contains(r.Codes, "run_never_finished") {
		t.Fatalf("an encrypted archive's never-finished run must be RED, got %s %v", r.Posture, r.Codes)
	}
	if err := os.WriteFile(filepath.Join(sealed, state.LastRunName), []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := Assess(Gather(sealed, ""), time.Now()); !slices.Contains(r.Codes, "lastrun_unreadable") || slices.Contains(r.Codes, "no_schedule") {
		t.Fatalf("an encrypted archive's unreadable record must WARN, and no schedule is no fault: %v", r.Codes)
	}

	if err := state.WriteLastRun(sealed, state.LastRun{Status: state.RunFailed, Started: old, Finished: old, ErrorKind: state.FailArchiveBusy, Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	in := Gather(sealed, "")
	r := Assess(in, time.Now())
	if !strings.Contains(strings.Join(r.Reasons, "\n"), state.FailureText(state.FailArchiveBusy)) {
		t.Fatalf("a failed encrypted run must say its kind's words: %v", r.Reasons)
	}
	ok := time.Now().Add(-time.Hour).UTC()
	if err := state.WriteLastRun(sealed, state.LastRun{Status: state.RunOK, Started: ok, Finished: ok.Add(time.Minute), Encrypted: true}); err != nil {
		t.Fatal(err)
	}
	in = Gather(sealed, "")
	summary := strings.Join(Summary(in, Assess(in, time.Now())), "\n")
	for _, wrong := range []string{"no run has completed", "exported 0", "none recorded — no go-back"} {
		if strings.Contains(summary, wrong) {
			t.Fatalf("the summary of an encrypted archive says %q:\n%s", wrong, summary)
		}
	}
	if !strings.Contains(summary, "sealed") {
		t.Fatalf("the summary of an encrypted archive must say its counts are sealed:\n%s", summary)
	}
}

// covers: MA-765, R37
// Status judges an encrypted archive's last keyless check from its outcome alone
// (phase2b §4), without the key: intact is no fault; not_attested and newer_format
// WARN; damaged, restored_older and unfinished_save are RED — each in the outcome's
// fixed words, under the code verify_<outcome>. A running check that never finished
// is still judged by the verdict it carries (F79).
func TestStatusJudgesAKeylessCheckByItsOutcome(t *testing.T) {
	sealed := t.TempDir()
	sealFolder(t, sealed)
	at := time.Now().Add(-time.Hour).UTC()
	done := func(outcome string) state.LastVerify {
		f := at.Add(time.Minute)
		return state.LastVerify{Status: state.VerifyDone, Started: at, Finished: &f, Encrypted: true, Outcome: outcome}
	}
	for outcome, want := range map[string]string{
		state.OutcomeIntact: "", state.OutcomeNotAttested: "WARN", state.OutcomeNewerFormat: "WARN",
		state.OutcomeDamaged: "RED", state.OutcomeRestoredOlder: "RED", state.OutcomeUnfinishedSave: "RED",
	} {
		if err := state.WriteLastVerify(sealed, done(outcome)); err != nil {
			t.Fatal(err)
		}
		r := Assess(Gather(sealed, ""), time.Now())
		code := "verify_" + outcome
		switch {
		case want == "" && slices.ContainsFunc(r.Codes, func(c string) bool { return strings.HasPrefix(c, "verify_") }):
			t.Fatalf("%s: an intact check is no fault, got %v", outcome, r.Codes)
		case want != "" && (r.Posture != want || !slices.Contains(r.Codes, code) || !strings.Contains(strings.Join(r.Reasons, "\n"), state.OutcomeText(outcome))):
			t.Fatalf("%s: want %s %s in the outcome's words, got %s %v %v", outcome, want, code, r.Posture, r.Codes, r.Reasons)
		}
	}
	prev := done(state.OutcomeDamaged)
	if err := state.WriteLastVerify(sealed, state.LastVerify{Status: state.VerifyRunning, Started: at.Add(time.Minute), Encrypted: true, Previous: &prev}); err != nil {
		t.Fatal(err)
	}
	if r := Assess(Gather(sealed, ""), time.Now()); r.Posture != "RED" || !slices.Contains(r.Codes, "verify_damaged") || !slices.Contains(r.Codes, "verify_never_finished") {
		t.Fatalf("a check that never finished keeps the damaged verdict: %s %v", r.Posture, r.Codes)
	}
}
