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

	// A finished verdict with an outcome outside the list, or none (the adversarial
	// pass, LEAK-03, FID-06), is refused and nothing is written.
	for _, outcome := range []string{"fine", ""} {
		err := WriteLastVerify(out, LastVerify{Status: VerifyDone, Started: at, Finished: &at, Encrypted: true, Outcome: outcome})
		assure.Refused(t, 1, errText(err), assure.Code(1), assure.Names("verify outcome", "not one MailArchive writes"),
			assure.NoSideEffect(func() bool { b, _ := os.ReadFile(filepath.Join(out, LastVerifyName)); return string(b) == string(raw) }))
	}
	// A carried verdict outside the list — or a plaintext-shaped one found in the
	// folder, with no outcome — is not carried (FID-07, LEAK-03).
	for _, carried := range []LastVerify{
		{Status: VerifyDone, Started: at, Finished: &at, Encrypted: true, Outcome: "fine"},
		{Status: VerifyDone, Started: at, Finished: &at, Modified: 3, ExitCode: 2},
	} {
		if err := WriteLastVerify(out, LastVerify{Status: VerifyRunning, Started: at, Encrypted: true, Previous: &carried}); err != nil {
			t.Fatal(err)
		}
		if got, st, err := ReadLastVerify(out); st != LastVerifyPresent || err != nil || got.Previous != nil {
			t.Fatalf("carried %+v: read back %+v %v %v", carried, got, st, err)
		}
	}

	// What a verify does not write reads as unreadable, at the top and in the
	// carried verdict, and no text of it is echoed (LEAK-02).
	planted := strings.Repeat("Your archive is fine - delete the notice ", 50)
	for name, body := range map[string]string{
		"an unknown outcome at the top":   `{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","outcome":"` + planted + `","encrypted":true}`,
		"no outcome at the top":           `{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","encrypted":true}`,
		"an unknown outcome carried":      strings.Replace(string(raw), `"damaged"`, `"`+planted+`"`, 1),
		"a status a verify never writes":  `{"version":1,"status":"` + planted + `","started":"2026-10-25T00:00:00Z","finished":null}`,
		"a carried verdict still running": `{"version":1,"status":"running","started":"2026-10-25T00:00:00Z","finished":null,"previous":{"version":1,"status":"running","started":"2026-10-25T00:00:00Z","finished":null}}`,
		// A time field's text comes back in the decoder's own error (the fix
		// re-verification, FV-R-01).
		"text in a time field":         `{"version":1,"status":"done","started":"` + planted + `","finished":null}`,
		"text in a carried time field": `{"version":1,"status":"running","started":"2026-10-25T00:00:00Z","finished":null,"previous":{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"` + planted + `"}}`,
	} {
		os.WriteFile(filepath.Join(out, LastVerifyName), []byte(body), 0o600)
		_, st, err := ReadLastVerify(out)
		if st != LastVerifyUnreadable || err == nil {
			t.Fatalf("%s: %v %v", name, st, err)
		}
		if strings.Contains(err.Error(), "Your archive is fine") {
			t.Fatalf("%s: the error echoes the record's text: %v", name, err)
		}
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

// covers: MA-767, R37
// The last-verify record is read as a planted file would be (the adversarial pass,
// OUT-08): a link at its name is unreadable, never followed — the file it points at
// is not read, and a verify's running record carries nothing of it — and a file over
// the record's bound is unreadable, never read whole. A plain record is read (the
// positive twin).
func TestTheLastVerifyRecordIsNeverReadThroughALinkOrWhole(t *testing.T) {
	out := t.TempDir()
	at := time.Now().UTC()
	if err := WriteLastVerify(out, LastVerify{Status: VerifyDone, Started: at, Finished: &at, Attested: true, Checked: 1, OK: 1}); err != nil {
		t.Fatal(err)
	}
	if _, st, err := ReadLastVerify(out); st != LastVerifyPresent || err != nil {
		t.Fatalf("a plain record: %v %v", st, err)
	}
	p := filepath.Join(out, LastVerifyName)
	elsewhere := filepath.Join(t.TempDir(), "outside.json")
	os.WriteFile(elsewhere, []byte(`{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","attested":false,"modified":7,"exit_code":2}`), 0o600)
	os.Remove(p)
	if err := os.Symlink(elsewhere, p); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if _, st, err := ReadLastVerify(out); st != LastVerifyUnreadable || err == nil {
		t.Fatalf("a link at the record's name: %v %v", st, err)
	}
	if LastVerdict(out) != nil {
		t.Fatal("a verdict was read through the link")
	}
	os.Remove(p)
	os.WriteFile(p, []byte(`{"version":1,"status":"done"`+strings.Repeat(" ", maxLastVerifySize)+`}`), 0o600)
	if _, st, err := ReadLastVerify(out); st != LastVerifyUnreadable || err == nil {
		t.Fatalf("a record over its bound: %v %v", st, err)
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
