package health

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
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

// covers: MA-770, R37
// A folder this computer recorded as encrypted that now holds a plaintext archive
// reads RED in status, as encrypted, in the case's words (it may have been
// tampered with), and its plaintext manifest is not read. The positive twin: the
// same folder, not recorded, is a plaintext archive to status. A record that cannot
// be used is a WARN naming it.
func TestStatusREDsARecordedArchiveThatNoLongerHoldsItself(t *testing.T) {
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600)
	os.Mkdir(filepath.Join(out, "Inbox"), 0o700)
	if in := Gather(out, ""); in.Encrypted || !in.HasManifestFile {
		t.Fatalf("the positive twin, not recorded: %+v", in)
	}
	id := "0123456789abcdef0123456789abcdef"
	encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, out, false, time.Now()) })
	t.Cleanup(func() { encrecord.Update("test", func(r *encrecord.Record) error { r.ForgetID(id); return nil }) })
	in := Gather(out, "")
	r := Assess(in, time.Now())
	if !in.Encrypted || in.HasManifestFile || r.Posture != "RED" || !slices.Contains(r.Codes, "encrypted_record_mismatch") ||
		!strings.Contains(strings.Join(r.Reasons, "\n"), "tampered") {
		t.Fatalf("a recorded folder now holding plaintext: %s %v %v", r.Posture, r.Codes, r.Reasons)
	}

	in = Input{Out: t.TempDir(), RecordProblem: "this computer's record of encrypted archives is damaged and needs repair"}
	if r := Assess(in, time.Now()); r.Posture != "WARN" || !slices.Contains(r.Codes, "encrypted_record_unusable") {
		t.Fatalf("an unusable record: %s %v", r.Posture, r.Codes)
	}
}

// covers: MA-773, R37
// A check stopped on purpose is a note, and the verdict it carries is still judged:
// a damaged one stays RED. A stopped record that claims an outcome of its own is not
// one MailArchive writes and is refused when written.
func TestStatusJudgesAStoppedCheckByTheVerdictItCarries(t *testing.T) {
	finished := time.Now().UTC()
	in := Input{Out: t.TempDir(), LastVerifyState: state.LastVerifyPresent, LastVerify: state.LastVerify{
		Status: state.VerifyStopped, Started: finished, Finished: &finished, Encrypted: true,
		Previous: &state.LastVerify{Status: state.VerifyDone, Finished: &finished, Encrypted: true, Outcome: state.OutcomeDamaged},
	}}
	r := Assess(in, time.Now())
	if r.Posture != "RED" || !slices.Contains(r.Codes, "verify_stopped") || !slices.Contains(r.Codes, "verify_damaged") {
		t.Fatalf("a stopped check over a damaged verdict: %s %v", r.Posture, r.Codes)
	}
	bad := state.LastVerify{Status: state.VerifyStopped, Encrypted: true, Outcome: state.OutcomeIntact}
	if err := state.WriteLastVerify(t.TempDir(), bad); err == nil {
		t.Fatal("a stopped record claiming its own outcome was written")
	}
}

// covers: MA-775, R37
// The status edges of P3-0 found by its adversarial pass: a stopped check over an
// intact verdict is a note only (FID-10); it does not reset a scheduled verify's
// staleness, which runs from the finished check it carries (CON-02); the summary
// says it was stopped and shows that verdict (CON-04); an unproven recorded path is
// a WARN (FID-09); and while the record cannot be used a plaintext archive is not
// called encrypted — its counts are read, beside the WARN (CON-01, REG-02).
func TestStatusAtTheEdgesOfP30(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	stopped := state.LastVerify{Status: state.VerifyStopped, Started: now.Add(-time.Hour), Finished: &now, Encrypted: true,
		Previous: &state.LastVerify{Status: state.VerifyDone, Finished: &old, Encrypted: true, Outcome: state.OutcomeIntact}}
	in := Input{Out: t.TempDir(), LastVerifyState: state.LastVerifyPresent, LastVerify: stopped}
	r := Assess(in, now)
	if r.Posture == "RED" || !slices.Contains(r.Codes, "verify_stopped") {
		t.Fatalf("a stopped check over an intact verdict: %s %v", r.Posture, r.Codes)
	}
	for _, c := range r.Codes {
		if strings.HasPrefix(c, "verify_") && c != "verify_stopped" {
			t.Fatalf("a stopped check over an intact verdict raised %s: %v", c, r.Codes)
		}
	}
	in.HasVerifyDescriptor = true
	in.VerifyDesc.Interval, in.VerifyDesc.Name, in.VerifyDesc.InstalledAt = "daily", "nightly-verify", old.Add(-time.Hour)
	if r := Assess(in, now); !slices.Contains(r.Codes, "verify_stale") {
		t.Fatalf("a stopped check hid a 30-day-old verify: %s %v", r.Posture, r.Codes)
	}
	// A stopped check carrying no finished verdict is no check at all: a schedule
	// installed long ago still has none (the mutation sweep showed it unpinned).
	bare := in
	bare.LastVerify.Previous = nil
	if r := Assess(bare, now); !slices.Contains(r.Codes, "verify_stale") || !strings.Contains(strings.Join(r.Reasons, "\n"), "no verify has run") {
		t.Fatalf("a stopped check with no verdict before it hid a never-run verify: %s %v %v", r.Posture, r.Codes, r.Reasons)
	}
	summary := strings.Join(Summary(in, Assess(in, now)), "\n")
	if !strings.Contains(summary, "→ stopped") || !strings.Contains(summary, "Previous verify:") {
		t.Fatalf("the summary of a stopped check:\n%s", summary)
	}

	if r := Assess(Input{Out: t.TempDir(), EncryptedCase: encmark.Unproven, EncryptedWhy: "repaired"}, now); r.Posture != "WARN" || !slices.Contains(r.Codes, "encrypted_record_unproven") {
		t.Fatalf("an unproven recorded path: %s %v", r.Posture, r.Codes)
	}

	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, encrecord.FileName), []byte("not a record\n"), 0o600)
	t.Cleanup(encrecord.SetDirForTest(dir))
	g := Gather(plain, "")
	if g.Encrypted || !g.HasManifestFile || g.RecordProblem == "" {
		t.Fatalf("a plaintext archive while the record cannot be used: %+v", g)
	}
	if r := Assess(g, now); !slices.Contains(r.Codes, "encrypted_record_unusable") || slices.Contains(r.Codes, "encrypted") {
		t.Fatalf("its status: %s %v", r.Posture, r.Codes)
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

// covers: MA-767, R37
// status -json of an encrypted archive's check comes from the outcome alone (the fix
// re-verification): counts and an exit code a planted record carries are never shown
// (FV-R-02), and the verdict a running record carries gives verify's exit for its
// outcome too (FV-T-04). A last-verify record that cannot be read is a WARN naming it
// and its remedy even with no verify schedule — an earlier RED behind it is never
// silent (FV-R-03).
func TestStatusJSONOfAKeylessCheckIsItsOutcome(t *testing.T) {
	sealed := t.TempDir()
	sealFolder(t, sealed)
	p := filepath.Join(sealed, state.LastVerifyName)
	os.WriteFile(p, []byte(`{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:01:00Z","outcome":"intact","encrypted":true,"exit_code":1,"modified":5,"missing":2,"checked":9}`), 0o600)
	in := Gather(sealed, "")
	j := JSON(in, Assess(in, time.Now())).LastVerify
	if j == nil || !j.Attested || j.ExitCode != 0 || j.Modified+j.Missing+j.Checked != 0 {
		t.Fatalf("a planted record's counts shown: %+v", j)
	}
	at := time.Now().Add(-time.Hour).UTC()
	f := at.Add(time.Minute)
	prev := state.LastVerify{Status: state.VerifyDone, Started: at, Finished: &f, Encrypted: true, Outcome: state.OutcomeDamaged}
	if err := state.WriteLastVerify(sealed, state.LastVerify{Status: state.VerifyRunning, Started: f, Encrypted: true, Previous: &prev}); err != nil {
		t.Fatal(err)
	}
	in = Gather(sealed, "")
	if j := JSON(in, Assess(in, time.Now())).LastVerify; j == nil || j.Previous == nil || j.Previous.ExitCode != 2 || j.ExitCode != 0 {
		t.Fatalf("the carried verdict's exit: %+v", j)
	}

	os.WriteFile(p, []byte(`{"status":`), 0o600)
	in = Gather(sealed, "")
	r := Assess(in, time.Now())
	if in.HasVerifyDescriptor || !slices.Contains(r.Codes, "lastverify_unreadable") || !strings.Contains(strings.Join(r.Reasons, "\n"), "mailarchive verify -out") {
		t.Fatalf("an unreadable record with no verify schedule: %s %v %v", r.Posture, r.Codes, r.Reasons)
	}
}

// covers: MA-765, MA-767, R37
// Status judges an encrypted archive's last keyless check from its outcome alone
// (phase2b §4), without the key: intact is no fault; not_attested and newer_format
// WARN; damaged, restored_older and unfinished_save are RED — each in the outcome's
// fixed words, under the code verify_<outcome>; status -json gives verify's exit for
// the outcome, 0 only for intact. A running check that never finished is still
// judged by the verdict it carries (F79).
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
		in := Gather(sealed, "")
		r := Assess(in, time.Now())
		// status -json: verify's exit for the outcome, never 0 for one that is not
		// intact, the counts the record does not keep left 0 (the adversarial pass,
		// FID-05).
		if j := JSON(in, r).LastVerify; j == nil || j.Outcome != outcome || j.Attested != (outcome == state.OutcomeIntact) ||
			j.ExitCode != map[bool]int{true: 0, false: 2}[outcome == state.OutcomeIntact] || j.Missing+j.Modified != 0 {
			t.Fatalf("%s: status -json last_verify %+v", outcome, j)
		}
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
