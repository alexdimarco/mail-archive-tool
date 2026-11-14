package desktop

import (
	"slices"
	"testing"

	"mail-archive-tool/internal/app"
)

// covers: MA-1324, R45
// The Desktop keeps an encrypted archive's yes for the Gmail stores its question named (the B3
// adversarial pass, round 2's G6): the Run now that applies it carries those stores
// (gmailYesStoresForRunNow, RunNow.GmailYesStores), not a yes for every Gmail account the run
// reads, and a store it switched on is spent while the others still wait. Positive twin first:
// before an answer the Run now carries no yes.
func TestTheDesktopsEncryptedYesNamesItsStores(t *testing.T) {
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Keys = newArchiveKeys(nil, readerLockedNote) })
	sealed := vaultedArchive(t)
	e.list(sealed)
	openHere(t, e, sealed)
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Off: []string{"imap.gmail.com", "imap.googlemail.com"}, Encrypted: true})
	if got := e.d.gmailYesStoresForRunNow(sealed); len(got) != 0 {
		t.Fatalf("positive twin: before an answer the Run now carries %v", got)
	}
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": sealed, "answer": "yes"}); m["ok"] != true {
		t.Fatalf("Yes: %v", m)
	}
	want := []string{"imap.gmail.com", "imap.googlemail.com"}
	if got := e.d.gmailYesStoresForRunNow(sealed); !slices.Equal(got, want) {
		t.Fatalf("the Run now carries %v, want the stores the question named %v", got, want)
	}
	// A Run now that switched one of them on spends that one; the other still waits.
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Switched: []string{"imap.gmail.com"}, On: []string{"imap.gmail.com"}, Off: nil, Encrypted: true})
	if got := e.d.gmailYesStoresForRunNow(sealed); !slices.Equal(got, []string{"imap.googlemail.com"}) {
		t.Fatalf("after one store was switched the Run now carries %v", got)
	}
}
