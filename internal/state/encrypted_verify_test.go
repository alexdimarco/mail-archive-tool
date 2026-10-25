package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-765, R37
// An encrypted archive's last-verify record holds only the A18 allow-list — times,
// the status and an outcome from the closed list — at the top and in the verdict a
// running record carries, whatever counts the caller set. An outcome outside the
// list is refused and nothing is written; a record that says one is read as
// unreadable, never judged. A plaintext record keeps its shape, with neither new key.
func TestAnEncryptedArchivesVerifyRecordHoldsOnlyTheAllowList(t *testing.T) {
	out := t.TempDir()
	at := time.Now().UTC()
	prev := LastVerify{Status: VerifyDone, Started: at, Finished: &at, Encrypted: true, Outcome: OutcomeDamaged, Checked: 9, Missing: 2, ExitCode: 2}
	v := LastVerify{Status: VerifyRunning, Started: at, Encrypted: true, Records: 7, Checked: 9, OK: 9, Attested: true, Previous: &prev}
	if err := WriteLastVerify(out, v); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, LastVerifyName))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"version": true, "status": true, "started": true, "finished": true, "outcome": true, "encrypted": true, "previous": true}
	p, _ := doc["previous"].(map[string]any)
	if p == nil || p["outcome"] != OutcomeDamaged {
		t.Fatalf("the running record lost the verdict it carries:\n%s", raw)
	}
	for _, d := range []map[string]any{doc, p} {
		for k := range d {
			if !allowed[k] {
				t.Fatalf("the record carries %q:\n%s", k, raw)
			}
		}
	}
	if got, st, err := ReadLastVerify(out); st != LastVerifyPresent || err != nil || !got.Encrypted || got.Previous == nil || got.Previous.Outcome != OutcomeDamaged {
		t.Fatalf("read back %+v %v %v", got, st, err)
	}

	err := WriteLastVerify(out, LastVerify{Status: VerifyDone, Started: at, Finished: &at, Encrypted: true, Outcome: "fine"})
	assure.Refused(t, 1, errText(err), assure.Code(1), assure.Names("fine", "closed list"),
		assure.NoSideEffect(func() bool { b, _ := os.ReadFile(filepath.Join(out, LastVerifyName)); return string(b) == string(raw) }))
	os.WriteFile(filepath.Join(out, LastVerifyName), []byte(strings.Replace(string(raw), `"damaged"`, `"fine"`, 1)), 0o600)
	if _, st, err := ReadLastVerify(out); st != LastVerifyUnreadable || err == nil {
		t.Fatalf("a record with an unknown outcome: %v %v", st, err)
	}

	plain := t.TempDir()
	if err := WriteLastVerify(plain, LastVerify{Status: VerifyDone, Started: at, Finished: &at, Attested: true, Checked: 3, OK: 3}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(plain, LastVerifyName))
	if strings.Contains(string(b), "outcome") || strings.Contains(string(b), "encrypted") || !strings.Contains(string(b), `"checked": 3`) {
		t.Fatalf("the plaintext record changed shape:\n%s", b)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// covers: MA-765, R37
// The integrity notice beside an encrypted archive is the outcome's fixed sentence
// and the time — no path, count or file name: written for a RED outcome, removed by
// intact, left standing by not_attested and newer_format.
func TestAnEncryptedArchivesIntegrityNoticeIsTheOutcomesWords(t *testing.T) {
	out := t.TempDir()
	at := time.Now().UTC()
	notice := filepath.Join(out, IntegrityAttentionName)
	write := func(outcome string) {
		t.Helper()
		if err := WriteLastVerify(out, LastVerify{Status: VerifyDone, Started: at, Finished: &at, Encrypted: true, Outcome: outcome, Missing: 4}); err != nil {
			t.Fatal(err)
		}
	}
	for _, red := range []string{OutcomeDamaged, OutcomeRestoredOlder, OutcomeUnfinishedSave} {
		write(OutcomeIntact)
		if _, err := os.Stat(notice); err == nil {
			t.Fatal("intact left the notice")
		}
		write(red)
		b, err := os.ReadFile(notice)
		if err != nil {
			t.Fatalf("%s wrote no notice", red)
		}
		if !strings.Contains(string(b), OutcomeText(red)) || strings.Contains(string(b), out) || strings.Contains(string(b), "file(s)") {
			t.Fatalf("%s notice:\n%s", red, b)
		}
		for _, amber := range []string{OutcomeNotAttested, OutcomeNewerFormat} {
			write(amber)
			if after, err := os.ReadFile(notice); err != nil || string(after) != string(b) {
				t.Fatalf("%s changed or removed the notice: %v\n%s", amber, err, after)
			}
		}
	}
}
