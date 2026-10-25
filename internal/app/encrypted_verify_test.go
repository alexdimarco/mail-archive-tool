package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/assure/leak"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// verifyAllowed is every key the last-verify record of an encrypted archive may
// carry (A18): times, the status and the outcome — no counts, no paths.
var verifyAllowed = map[string]bool{"version": true, "status": true, "started": true, "finished": true, "outcome": true, "encrypted": true, "previous": true}

// requireMinimisedVerify fails unless out's last-verify record holds only the
// allow-listed keys, at the top and in the verdict it carries, with outcome want.
func requireMinimisedVerify(t *testing.T, out, status, want string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(out, state.LastVerifyName))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, d := range []map[string]any{doc, asMap(doc["previous"])} {
		for k := range d {
			if !verifyAllowed[k] {
				t.Fatalf("the encrypted last-verify record carries %q:\n%s", k, raw)
			}
		}
	}
	if doc["status"] != status || doc["encrypted"] != true || (want != "" && doc["outcome"] != want) {
		t.Fatalf("record %s, want status %s, outcome %q", raw, status, want)
	}
	if strings.Contains(string(raw), out) {
		t.Fatalf("the record names the archive:\n%s", raw)
	}
	return doc
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

// aUnitOf is the archive-relative path of one unit file under out.
func aUnitOf(t *testing.T, out string) string {
	t.Helper()
	var found string
	filepath.WalkDir(filepath.Join(out, "units"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && found == "" {
			rel, _ := filepath.Rel(out, p)
			found = filepath.ToSlash(rel)
		}
		return nil
	})
	if found == "" {
		t.Fatal("no unit file")
	}
	return found
}

// withoutVerifyRecord is a tree snapshot less what a verify writes: its record, the
// integrity notice and the lock file.
func withoutVerifyRecord(t *testing.T, out string) map[string][]byte {
	t.Helper()
	snap := snapshotTree(t, out)
	for _, name := range []string{state.LastVerifyName, state.IntegrityAttentionName, lockfile.Name} {
		delete(snap, name)
	}
	return snap
}

// covers: MA-764, MA-765, R37
// Keyless tier 1 through Verify (phase2b §4): an encrypted archive is checked
// without its key — no ErrEncryptedNeedsKey — and a whole one is intact, exit 0. The
// check writes only its last-verify record, in the allow-listed shape (times,
// status, outcome; A18), and the tree stays in the allowed grammar; status judges
// the outcome without the key (GREEN for intact) and `status -json` reports the
// sealed history as null.
func TestVerifyChecksAnEncryptedArchiveWithoutItsKey(t *testing.T) {
	out := sealedArchive(t)
	before := withoutVerifyRecord(t, out)
	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("a keyless check of an encrypted archive: %v", err)
	}
	if !rep.Encrypted || rep.Outcome != state.OutcomeIntact || rep.ExitCode() != 0 || rep.Checked == 0 || rep.OK != rep.Checked {
		t.Fatalf("report %+v", rep)
	}
	if !sameTree(before, withoutVerifyRecord(t, out)) {
		t.Fatal("a keyless check changed the archive")
	}
	requireMinimisedVerify(t, out, state.VerifyDone, state.OutcomeIntact)
	if _, err := os.Stat(filepath.Join(out, state.IntegrityAttentionName)); err == nil {
		t.Fatal("an intact check wrote the integrity notice")
	}
	requireTree(t, "after a keyless check", out, leak.AtRest)
	summary := strings.Join(VerifySummary(rep), "\n")
	assure.Reached(t, summary, "the keyless summary")
	if !strings.Contains(summary, "intact: every file this archive's record lists is present and unchanged") {
		t.Fatalf("summary:\n%s", summary)
	}

	in := health.Gather(out, "")
	hr := health.Assess(in, time.Now())
	for _, c := range hr.Codes {
		if strings.HasPrefix(c, "verify_") || c == "not_attested" {
			t.Fatalf("an intact check is judged %s: %v", c, hr.Reasons)
		}
	}
	if !strings.Contains(strings.Join(health.Summary(in, hr), "\n"), "→ intact (a keyless check of its files)") {
		t.Fatalf("status does not show the check:\n%s", strings.Join(health.Summary(in, hr), "\n"))
	}
	doc := health.JSON(in, hr)
	if doc.History != nil || doc.LastVerify == nil || doc.LastVerify.Outcome != state.OutcomeIntact || !doc.LastVerify.Attested {
		t.Fatalf("status -json: history %+v, last verify %+v", doc.History, doc.LastVerify)
	}
}

// covers: MA-764, MA-765, R37
// Damage found without the key: a unit file removed is damaged, exit 2, the file
// named; the integrity notice says so in fixed words with no path; status is RED
// (verify_damaged) without the key. The "running" record carries the damaged
// verdict in the same allow-listed shape (F79). Put back, the next check is intact,
// removes the notice and clears the RED. A missing file list is not_attested — a
// WARN — and leaves an earlier notice standing.
func TestAKeylessCheckFindsDamageAndStatusSaysSo(t *testing.T) {
	out := sealedArchive(t)
	unit := aUnitOf(t, out)
	saved, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(unit)))
	os.Remove(filepath.Join(out, filepath.FromSlash(unit)))

	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != state.OutcomeDamaged || rep.ExitCode() != 2 || rep.Missing != 1 || len(rep.Problems) != 1 || rep.Problems[0].Path != unit {
		t.Fatalf("report %+v", rep)
	}
	requireMinimisedVerify(t, out, state.VerifyDone, state.OutcomeDamaged)
	notice, err := os.ReadFile(filepath.Join(out, state.IntegrityAttentionName))
	if err != nil {
		t.Fatal("a damaged check wrote no integrity notice")
	}
	if !strings.Contains(string(notice), state.OutcomeText(state.OutcomeDamaged)) || strings.Contains(string(notice), out) || strings.Contains(string(notice), unit) {
		t.Fatalf("the notice:\n%s", notice)
	}
	if hr := assessOut(out); hr.Posture != "RED" || !strings.Contains(reasonFor(hr, "verify_damaged"), "restore the whole archive folder") {
		t.Fatalf("status after damage: %s %v", hr.Posture, hr.Reasons)
	}

	os.WriteFile(filepath.Join(out, filepath.FromSlash(unit)), saved, 0o600)
	var mid map[string]any
	rep, err = Verify(out, VerifyOptions{}, discard(), func(Report) { mid = requireMinimisedVerify(t, out, state.VerifyRunning, "") })
	if err != nil || rep.Outcome != state.OutcomeIntact {
		t.Fatalf("after putting the file back: %+v %v", rep, err)
	}
	if p := asMap(mid["previous"]); p == nil || p["outcome"] != state.OutcomeDamaged {
		t.Fatalf("the running record does not carry the last verdict: %v", mid)
	}
	if _, err := os.Stat(filepath.Join(out, state.IntegrityAttentionName)); err == nil {
		t.Fatal("an intact check left the integrity notice")
	}
	if hr := assessOut(out); reasonFor(hr, "verify_damaged") != "" {
		t.Fatalf("an intact check left the RED: %v", hr.Reasons)
	}

	os.Remove(filepath.Join(out, filepath.FromSlash(unit)))
	Verify(out, VerifyOptions{}, discard(), nil)
	os.WriteFile(filepath.Join(out, filepath.FromSlash(unit)), saved, 0o600)
	os.Remove(filepath.Join(out, container.InventoryName))
	rep, err = Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil || rep.Outcome != state.OutcomeNotAttested || rep.ExitCode() != 2 {
		t.Fatalf("no file list: %+v %v", rep, err)
	}
	hr := assessOut(out)
	if hr.Posture != "WARN" || !strings.Contains(reasonFor(hr, "verify_not_attested"), "run a backup, then check again") {
		t.Fatalf("status with no file list: %s %v", hr.Posture, hr.Reasons)
	}
	if _, err := os.Stat(filepath.Join(out, state.IntegrityAttentionName)); err != nil {
		t.Fatal("a not_attested check removed an earlier integrity notice")
	}
}

// covers: MA-764, R37
// The keyless check runs under the archive lock (phase2b §4): while another run holds
// it, verify is refused as busy, naming it, and writes nothing — no last-verify
// record, nothing changed (the adversarial pass, FID-02). The positive twin: with the
// lock released the same archive checks intact.
func TestKeylessVerifyTakesTheArchiveLock(t *testing.T) {
	out := sealedArchive(t)
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, out)
	_, verr := Verify(out, VerifyOptions{}, discard(), nil)
	assure.Refused(t, 1, errText(verr), assure.Code(1), assure.Names("in use"),
		assure.NoSideEffect(func() bool {
			_, statErr := os.Stat(filepath.Join(out, state.LastVerifyName))
			return errors.Is(statErr, fs.ErrNotExist) && sameTree(before, snapshotTree(t, out))
		}))
	held.Release()
	if rep, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil || rep.Outcome != state.OutcomeIntact {
		t.Fatalf("with the lock released: %+v %v", rep, err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// covers: MA-767, R37
// A link at the last-verify record's name is never read (the adversarial pass,
// OUT-08): the check's running record carries no verdict from the file it points at,
// that file is left as it was, and the record the check writes is its own file in the
// archive. Likewise a link naming the archive itself is checked as the archive (as a
// linked -out is for a plaintext verify), while links inside it stay refused (CRIT-2).
func TestKeylessVerifyFollowsNoLinkToItsRecord(t *testing.T) {
	out := sealedArchive(t)
	outside := filepath.Join(t.TempDir(), "outside.json")
	body := `{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","outcome":"damaged","encrypted":true}`
	os.WriteFile(outside, []byte(body), 0o600)
	if err := os.Symlink(outside, filepath.Join(out, state.LastVerifyName)); err != nil {
		t.Skip("no symlinks here:", err)
	}
	var mid map[string]any
	rep, err := Verify(out, VerifyOptions{}, discard(), func(Report) { mid = requireMinimisedVerify(t, out, state.VerifyRunning, "") })
	if err != nil || rep.Outcome != state.OutcomeIntact {
		t.Fatalf("%+v %v", rep, err)
	}
	if mid["previous"] != nil {
		t.Fatalf("the running record carried a verdict read through the link: %v", mid)
	}
	if b, _ := os.ReadFile(outside); string(b) != body {
		t.Fatal("the file outside the archive changed")
	}
	if fi, err := os.Lstat(filepath.Join(out, state.LastVerifyName)); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("the record is not the archive's own file: %v", err)
	}

	link := filepath.Join(t.TempDir(), "archive")
	if err := os.Symlink(out, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if rep, err := Verify(link, VerifyOptions{}, discard(), nil); err != nil || rep.Outcome != state.OutcomeIntact {
		t.Fatalf("a link naming the archive: %+v %v", rep, err)
	}
}

// covers: MA-764, R37
// Every outcome renders in verify's words (FID-10): the headline — "intact:", "NOT
// attested:", "NOT checked:" or "NOT intact:" — with the rule's reason and, when not
// intact, the outcome's fixed sentence; the counts, OK net of the problems, a bounded
// sample with the rest counted as truncated; leftovers said to be removed only when
// the next backup removes them; parts of the file list unread; and the run-log line
// with the outcome and its verdict.
func TestAKeylessReportSaysEachOutcome(t *testing.T) {
	headline := map[string]string{
		state.OutcomeIntact: "intact: ", state.OutcomeNotAttested: "NOT attested: ", state.OutcomeNewerFormat: "NOT checked: ",
		state.OutcomeDamaged: "NOT intact: ", state.OutcomeRestoredOlder: "NOT intact: ", state.OutcomeUnfinishedSave: "NOT intact: ",
	}
	for outcome, head := range headline {
		c := container.Check{Outcome: outcome, Reason: "the rule's reason", Checked: 10, Missing: 3, Modified: 1, Unnamed: 5, Leftovers: 4, PartsUnread: 2,
			Problems: []container.FileProblem{{Path: "units/ab/ab01", Kind: "missing"}}}
		r := keylessReport("/a", c)
		if r.OK != 6 || r.Truncated["missing"] != 2 || r.Truncated["modified"] != 1 {
			t.Fatalf("%s: ok %d, truncated %v", outcome, r.OK, r.Truncated)
		}
		text := strings.Join(VerifySummary(r), "\n")
		for _, want := range []string{head + "the rule's reason", "files checked=10 · ok=6 · missing=3 · modified=1", "missing: units/ab/ab01",
			"… 2 more missing", "… 1 more modified", "5 file(s) in the archive are not in its file list — they do not change the outcome",
			"4 superseded file(s) of its file list remain — the next backup removes them", "2 part(s) of the archive's file list could not be read"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: the summary does not say %q:\n%s", outcome, want, text)
			}
		}
		if (outcome != state.OutcomeIntact) != strings.Contains(text, state.OutcomeText(outcome)) {
			t.Fatalf("%s: the outcome's sentence is shown only when not intact:\n%s", outcome, text)
		}
		line := VerifyResultLine(r)
		verdict := map[bool]string{true: "attested ", false: "NOT-ATTESTED "}[outcome == state.OutcomeIntact]
		if !strings.HasPrefix(line, verdict) || !strings.Contains(line, "outcome="+outcome) || !strings.Contains(line, "unnamed=5 leftovers=4") {
			t.Fatalf("%s: run-log line %q", outcome, line)
		}
		c.LeftoversStay = true
		text = strings.Join(VerifySummary(keylessReport("/a", c)), "\n")
		if strings.Contains(text, "the next backup removes") || !strings.Contains(text, "9 file(s) in the archive are not in the part of its file list that could be read") {
			t.Fatalf("%s: leftovers that stay:\n%s", outcome, text)
		}
	}
	// A rule's own next step replaces the outcome's fixed sentence (the owner's
	// wording for a damaged record copy, 2026-10-25).
	c := container.Check{Outcome: state.OutcomeNotAttested, Reason: "the rule's reason", Advice: "What to do: the rule's own step."}
	r := keylessReport("/a", c)
	text := strings.Join(VerifySummary(r), "\n")
	if !strings.Contains(text, "NOT attested: the rule's reason\n  What to do: the rule's own step.") || strings.Contains(text, state.OutcomeText(c.Outcome)) {
		t.Fatalf("the rule's own step is not shown in place of the outcome's sentence:\n%s", text)
	}
	if b, err := json.Marshal(r); err != nil || !strings.Contains(string(b), `"advice":"What to do: the rule's own step."`) {
		t.Fatalf("verify -json does not carry the step: %s %v", b, err)
	}
}

// covers: MA-764, R37
// What keyless verify still refuses: -record (every backup records its files'
// checksums itself), naming the archive and the remedy, with nothing written; and a
// folder inside an encrypted archive, as every keyless operation refuses one. The
// positive twin: the same archive checks without -record (above).
func TestKeylessVerifyRefusesRecordAndANestedFolder(t *testing.T) {
	out := sealedArchive(t)
	before := snapshotTree(t, out)
	_, err := Verify(out, VerifyOptions{Record: true}, discard(), nil)
	if !errors.Is(err, ErrVerifyRecordEncrypted) {
		t.Fatalf("want ErrVerifyRecordEncrypted, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(out, "without -record"),
		assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, out)) }))

	nested := filepath.Join(out, "units")
	_, err = Verify(nested, VerifyOptions{}, discard(), nil)
	if !errors.Is(err, ErrEncryptedNeedsKey) {
		t.Fatalf("a folder inside an encrypted archive: want ErrEncryptedNeedsKey, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(out, "password or recovery key"),
		assure.NoSideEffect(func() bool { return sameTree(before, snapshotTree(t, out)) }))
}
