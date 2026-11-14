package desktop

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// gmailYesBody is the page's Yes for out: the folder, the archive the question was drawn for and the
// fingerprint of the Gmail accounts it was drawn for, as the page sends them (MA-1323, MA-1334).
func gmailYesBody(out string) map[string]any {
	return map[string]any{"path": out, "answer": "yes", "archive": state.ArchiveID(out), "asked": gmailAsked(gmailOffAt(out, true))}
}

// gmailRefusedRC is a route reply's rc for assure.Refused: 2 for a refusal the page shows.
func gmailRefusedRC(m map[string]any) (int, string) {
	msg, _ := m["error"].(string)
	if m["ok"] == false {
		return 2, msg
	}
	return 0, msg
}

// covers: MA-1334, R45
// The B3 adversarial pass, round 3's H5: the card's Yes covers only the Gmail accounts the page
// asked about when it was drawn. The question's row carries their fingerprint beside the archive's
// id (data-gmail-asked; the accounts are not named on the page), which the page's script sends; a
// Yes from a page drawn before a later backup found another account off (an account added to
// Thunderbird since) is refused, "This archive's Gmail question changed since this page was drawn.
// Reload the page.", and nothing is kept, so the person is asked again about both. A Yes that sends
// no fingerprint is refused the same way. Settings' copy of the question carries the same one.
// Positive twin first: the fingerprint the page drew answers, and the Yes names that account.
func TestACardsYesCoversOnlyTheAccountsItWasDrawnFor(t *testing.T) {
	e := newDeleteEnv(t)
	out := e.plainArchive("Named")
	gmailWaits(t, out, "imap.gmail.com")
	id := state.ArchiveID(out)
	asked := assure.Reached(t, cardRow(t, e, out).GmailAsked, "the card's fingerprint of the accounts it asks about")
	if page := e.page(); !strings.Contains(page, `<div class="actions" data-gmail-archive="`+id+`" data-gmail-asked="`+asked+`"><button class="btn secondary" type="button" data-gmail-answer="yes" data-path="`+out+`">`) {
		t.Fatal("the card's question row does not carry the fingerprint beside the archive's id")
	}
	at := strings.Index(dashboardJS, "querySelectorAll('[data-gmail-answer]')")
	if at < 0 || !strings.Contains(dashboardJS[at:], "asked:b.parentElement.dataset.gmailAsked||''") {
		t.Fatal("the page's script does not send the fingerprint")
	}
	if s := gmailSettingsFor(out, true); s.Asked != asked {
		t.Fatalf("Settings' question carries %q, the card %q", s.Asked, asked)
	}
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "yes", "archive": id, "asked": asked}); m["ok"] != true {
		t.Fatalf("positive twin: the page's own fingerprint: %v", m)
	}
	if kept := gmailanswer.Read(out); !reflect.DeepEqual(kept.Yes, []string{"imap.gmail.com"}) {
		t.Fatalf("positive twin: the Yes kept %+v", kept)
	}
	if err := gmailanswer.Keep(out, gmailanswer.Answer{Archive: id}); err != nil { // nothing kept again
		t.Fatal(err)
	}

	// A later backup of the same archive finds a second Gmail account off; the page is not reloaded.
	gmailWaits(t, out, "imap.gmail.com", "imap.gmail.com-1")
	if now := cardRow(t, e, out).GmailAsked; now == asked {
		t.Fatal("premise: the fingerprint did not change with the accounts")
	}
	nothingKept := func() bool { return len(gmailanswer.Read(out).Yes) == 0 }
	for _, body := range []map[string]any{
		{"path": out, "answer": "yes", "archive": id, "asked": asked},
		{"path": out, "answer": "yes", "archive": id},
	} {
		_, m := e.post("/api/gmail-answer", body)
		rc, msg := gmailRefusedRC(m)
		assure.Refused(t, rc, msg, assure.Names("changed since this page was drawn", "Reload the page"), assure.NoSideEffect(nothingKept))
	}
}

// covers: MA-1334, R45
// H5 on an encrypted archive's card (kept in memory, MA-1315, MA-1324): the Yes covers only the
// accounts the question named when the page was drawn; after a later Run now found another one
// off, the page's Yes is refused and no yes waits for the next Run now. Positive twin first: the
// fingerprint the page drew answers, for the account it named.
func TestAnEncryptedCardsYesCoversOnlyTheAccountsItWasDrawnFor(t *testing.T) {
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Keys = newArchiveKeys(nil, readerLockedNote) })
	sealed := vaultedArchive(t)
	e.list(sealed)
	entry := openHere(t, e, sealed)
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Off: []string{"imap.gmail.com"}, Encrypted: true})
	asked := assure.Reached(t, cardRow(t, e, sealed).GmailAsked, "the encrypted card's fingerprint")
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": sealed, "answer": "yes", "asked": asked}); m["ok"] != true {
		t.Fatalf("positive twin: %v", m)
	}
	if got := e.d.gmailYesStoresForRunNow(sealed); !slices.Equal(got, []string{"imap.gmail.com"}) {
		t.Fatalf("positive twin: the Run now carries %v", got)
	}
	lastSyncMu.Lock()
	entry.gmailYes = nil // no answer again, the question as the page drew it
	lastSyncMu.Unlock()

	// A later Run now finds a second account off; the page drawn before is not reloaded.
	e.d.keepGmailOutcome(sealed, app.GmailOutcome{Off: []string{"imap.gmail.com", "imap.googlemail.com"}, Encrypted: true})
	_, m := e.post("/api/gmail-answer", map[string]any{"path": sealed, "answer": "yes", "asked": asked})
	rc, msg := gmailRefusedRC(m)
	assure.Refused(t, rc, msg, assure.Names("changed since this page was drawn", "Reload the page"),
		assure.NoSideEffect(func() bool { return len(e.d.gmailYesStoresForRunNow(sealed)) == 0 }))
}
