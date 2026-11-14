package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
)

// openHere stands an archive open in this Desktop's key registry, as an Unlock leaves it (no
// key is needed for what the card reads), and returns its entry.
func openHere(t *testing.T, e *delEnv, out string) *keyEntry {
	t.Helper()
	_, key := keyOf(out)
	en := &keyEntry{path: out, id: "test-archive"}
	e.cfg.Keys.mu.Lock()
	e.cfg.Keys.entries[key] = en
	e.cfg.Keys.mu.Unlock()
	t.Cleanup(func() {
		e.cfg.Keys.mu.Lock()
		delete(e.cfg.Keys.entries, key)
		e.cfg.Keys.mu.Unlock()
	})
	return en
}

// filesUnder is every file under dir with its bytes.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			got[p] = string(b)
		}
		return nil
	})
	return got
}

// covers: MA-1315, R45
// The Desktop asks the Gmail question for an encrypted archive too (the owner's ruling on F18,
// 2026-11-13): while the archive is unlocked here and its last Run now found a Gmail store off,
// its row asks the question with the encrypted add-on. The answer is kept in memory on the open
// archive only, never in a file (nothing beside an encrypted archive, nor this computer's
// settings, says it holds a Gmail account, O1): Yes is applied by the next Run now of that
// archive while it stays unlocked, and is then spent; Not now silences the row; a Lock forgets
// both. Positive twins first: a locked encrypted archive's row asks nothing, and so does an
// unlocked one whose last Run now found no Gmail store off.
func TestTheDesktopAsksTheGmailQuestionForAnUnlockedEncryptedArchive(t *testing.T) {
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Keys = newArchiveKeys(nil, readerLockedNote) })
	sealed := vaultedArchive(t)
	e.list(sealed)
	if r := cardRow(t, e, sealed); r.Gmail != "" || r.GmailYes != "" {
		t.Fatalf("positive twin: a locked encrypted archive asks %q / %q", r.Gmail, r.GmailYes)
	}
	en := openHere(t, e, sealed)
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Encrypted: true})
	if r := cardRow(t, e, sealed); r.Gmail != "" {
		t.Fatalf("positive twin: nothing off, yet the row asks %q", r.Gmail)
	}

	jobDir, _ := job.Dir()
	beforeSettings, beforeArchive := filesUnder(t, jobDir), filesUnder(t, sealed)
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Off: []string{"imap.gmail.com"}, Encrypted: true})
	if r := cardRow(t, e, sealed); r.Gmail != state.GmailSwitchQuestion(true) || !strings.Contains(r.Gmail, state.GmailQuestionEncrypted) {
		t.Fatalf("the unlocked encrypted archive's row asks %q", r.Gmail)
	}

	// Yes: kept in memory only, applied by the next Run now, then spent.
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": sealed, "answer": "yes", "asked": e.d.cfg.encryptedGmailAsked(sealed)}); m["ok"] != true || m["note"] != gmailYesWaitsEncrypted {
		t.Fatalf("Yes: %v", m)
	}
	if !en.gmailYesWaits() {
		t.Fatal("the yes is not kept for the open archive")
	}
	if r := cardRow(t, e, sealed); r.Gmail != "" || r.GmailYes != gmailYesWaitsEncrypted {
		t.Fatalf("after Yes the row says %q / %q", r.Gmail, r.GmailYes)
	}
	if p, _ := gmailanswer.Path(sealed); func() bool { _, err := os.Lstat(p); return err == nil }() {
		t.Fatal("an encrypted archive's yes was written to this computer's Gmail answer file")
	}
	if after := filesUnder(t, jobDir); len(after) != len(beforeSettings) {
		t.Fatalf("answering wrote this computer's settings: %v", after)
	}
	if after := filesUnder(t, sealed); len(after) != len(beforeArchive) {
		t.Fatal("answering wrote into the encrypted archive")
	}
	if !e.d.gmailYesForRunNow(sealed) {
		t.Fatal("the next Run now does not carry the yes")
	}
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Switched: []string{"imap.gmail.com"}, On: []string{"imap.gmail.com"}, Encrypted: true})
	if e.d.gmailYesForRunNow(sealed) || en.gmailYesWaits() {
		t.Fatal("the yes the Run now applied is not spent")
	}
	if r := cardRow(t, e, sealed); r.Gmail != "" || r.GmailYes != "" {
		t.Fatalf("after the switch the row says %q / %q", r.Gmail, r.GmailYes)
	}

	// Not now: the row asks no more; a Lock forgets it, and the next Run now asks again.
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Off: []string{"second"}, Encrypted: true})
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": sealed, "answer": "not-now"}); m["ok"] != true || m["note"] != gmailNotNowEncryptedNote {
		t.Fatalf("Not now: %v", m)
	}
	if r := cardRow(t, e, sealed); r.Gmail != "" {
		t.Fatal("after Not now the row still asks")
	}
	e.cfg.Keys.mu.Lock()
	_, key := keyOf(sealed)
	delete(e.cfg.Keys.entries, key)
	e.cfg.Keys.mu.Unlock()
	if r := cardRow(t, e, sealed); r.Gmail != "" || e.d.gmailYesForRunNow(sealed) {
		t.Fatal("a locked archive still asks, or carries a yes")
	}
	_, m := e.post("/api/gmail-answer", map[string]any{"path": sealed, "answer": "yes"})
	if m["ok"] == true || m["error"] != gmailNoQuestionLocked {
		t.Fatalf("a Yes for a locked encrypted archive: %v", m)
	}
}
