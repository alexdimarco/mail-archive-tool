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
