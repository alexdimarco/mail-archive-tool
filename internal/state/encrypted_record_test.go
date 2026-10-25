package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// leakyRecord is a failed run's record carrying every field that can hold mail
// metadata: a source path in the job, error and warning, a program path, counts
// and a pre-sync trace.
func leakyRecord(out string) LastRun {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	return LastRun{
		Status: RunFailed, Started: at, Finished: at.Add(time.Minute), PID: 4242,
		Exe: "/Users/someone/bin/mailarchive", Mode: "incremental",
		Job:      []string{"-out", out, "-input", "/Users/someone/Mail/SECRET-STORE"},
		Exported: 7, Filled: 2, Fillable: 1, Terminal: 1, Unknown: 1, IndexErrors: 1,
		Error:     "1 file(s) failed: /Users/someone/Mail/SECRET-STORE: permission denied",
		Warnings:  []RunWarning{{Code: "credential_in_archive", Message: "secret file " + out + "/client-secret.txt"}},
		ErrorKind: FailSourceUnreadable, GapsObserved: 3, Unreadable: 1,
		Presync: &PresyncRecord{Code: "presync_quit_failed"},
	}
}

// covers: MA-734, R37
// The last-run record, its attention notice and the run-history entry of an
// ENCRYPTED archive hold only the A6 allow-list — times, the outcome and the
// failure kind in fixed words — however much the record that reached the writer
// carried (P2a-C16). The positive twin: the same record of a plaintext archive is
// written exactly as before (every field, the error text and the archive path in
// its notice).
func TestAnEncryptedArchivesRunRecordsHoldOnlyTheAllowList(t *testing.T) {
	plain := t.TempDir()
	r := leakyRecord(plain)
	if err := WriteLastRun(plain, r); err != nil {
		t.Fatal(err)
	}
	r.Version = 1
	want, _ := json.MarshalIndent(r, "", "  ")
	if got, _ := os.ReadFile(filepath.Join(plain, LastRunName)); string(got) != string(want) {
		t.Fatalf("a plaintext record must be written as it always was:\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(string(want), `"encrypted"`) {
		t.Fatal("a plaintext record must not carry the encrypted field")
	}
	notice, _ := os.ReadFile(filepath.Join(plain, AttentionName))
	if !strings.Contains(string(notice), r.Error) || !strings.Contains(string(notice), plain) || !strings.Contains(string(notice), "index.html") {
		t.Fatalf("a plaintext notice keeps its error text, archive path and index.html line: %q", notice)
	}
	if e := RunHistoryEntryFrom(r); e.Error != r.Error || e.Exported != 7 || e.Mode != "incremental" || e.ErrorKind != "" {
		t.Fatalf("a plaintext history entry is unchanged: %+v", e)
	}

	sealed := t.TempDir()
	r = leakyRecord(sealed)
	r.Encrypted = true
	if err := WriteLastRun(sealed, r); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(sealed, LastRunName))
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for k := range keys {
		switch k {
		case "version", "status", "started", "finished", "error_kind", "encrypted":
		default:
			t.Fatalf("an encrypted archive's record carries %q, outside the allow-list:\n%s", k, raw)
		}
	}
	back, st, err := ReadLastRun(sealed)
	if err != nil || st != LastRunPresent || back.Status != RunFailed || back.ErrorKind != FailSourceUnreadable || !back.Encrypted || !back.Finished.Equal(r.Finished) {
		t.Fatalf("the minimal record does not read back: %+v (%v)", back, err)
	}
	notice, _ = os.ReadFile(filepath.Join(sealed, AttentionName))
	if !strings.Contains(string(notice), FailureText(FailSourceUnreadable)) || !strings.Contains(string(notice), "encrypted") {
		t.Fatalf("the encrypted notice must say the kind's fixed words: %q", notice)
	}
	e := RunHistoryEntryFrom(r)
	if e != (RunHistoryEntry{When: r.Finished, Status: RunFailed, ErrorKind: FailSourceUnreadable, Encrypted: true}) {
		t.Fatalf("an encrypted history entry is more than when, outcome and kind: %+v", e)
	}
	if err := AppendRunHistory(sealed, e); err != nil {
		t.Fatal(err)
	}
	hist, _ := os.ReadFile(filepath.Join(sealed, RunHistoryName))
	for _, f := range []struct {
		name string
		data []byte
	}{{LastRunName, raw}, {AttentionName, notice}, {RunHistoryName, hist}} {
		for _, s := range []string{sealed, filepath.Base(sealed), "SECRET-STORE", "someone", "permission denied", "client-secret", "presync"} {
			if strings.Contains(string(f.data), s) {
				t.Fatalf("%s of an encrypted archive names %q:\n%s", f.name, s, f.data)
			}
		}
	}

	// An ok run removes the encrypted notice like the plaintext one.
	r.Status, r.ErrorKind = RunOK, ""
	if err := WriteLastRun(sealed, r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sealed, AttentionName)); !os.IsNotExist(err) {
		t.Fatal("an ok run must remove the attention notice")
	}
}

// covers: MA-734, R37
// The failure kinds are a closed list: every kind has its own fixed sentence, no
// sentence names a path or carries a placeholder, and an unknown kind reads as the
// generic failure — never as the kind string or any other text.
func TestEveryFailureKindHasFixedWords(t *testing.T) {
	kinds := []string{FailEncryptedNeedsKey, FailEncryptedRefused, FailNoSources, FailSourceUnreachable,
		FailSourceUnreadable, FailArchiveUnwritable, FailArchiveAbsent, FailScratch, FailArchiveBusy, FailLockLost, FailManifestUnreadable,
		FailManifestSave, FailInventory, FailRecordOlder, FailRecordNewer, FailIndex, ErrKindIndexBusy, ErrKindIndexUnwritable, ErrKindIndexDamaged,
		ErrKindIndexDamagedNoManifest, ErrKindServiceUnavailable, FailSignIn, FailMailboxFailed, FailPartial, FailRunFailed, NoteIndexBehind}
	if len(kinds) != len(failureText) {
		t.Fatalf("the kind list (%d) and the text table (%d) disagree", len(kinds), len(failureText))
	}
	for _, k := range kinds {
		text, ok := failureText[k]
		if !ok || text == "" || strings.ContainsAny(text, "%/\\") || strings.Contains(text, k) {
			t.Fatalf("kind %q has no fixed, path-free sentence: %q", k, text)
		}
	}
	if FailureText("some_new_kind") != failureText[FailRunFailed] || FailureText("") != failureText[FailRunFailed] {
		t.Fatal("an unknown kind must read as the generic failure")
	}
}
