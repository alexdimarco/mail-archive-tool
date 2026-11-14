package desktop

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// gmailPost answers the Gmail question for out as the page does: the folder, the answer, and the
// archive id the page drew the question for.
func gmailPost(e *delEnv, out, answer string) map[string]any {
	e.t.Helper()
	_, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": answer, "archive": state.ArchiveID(out)})
	return m
}

// covers: MA-1318, R45
// Not now on the card takes back a yes still waiting for the same archive (the B3 adversarial
// pass, round 2's G2, turned from its probe TestB3AdvR2NotNowKeepsAWaitingYes): after Yes for one
// store, and Not now when a later backup finds a second store off, no yes waits, so the next
// backup switches nothing, as the note's "Nothing changed" says. Positive twin first: the Yes is
// kept for the first store.
func TestNotNowOnTheCardTakesBackAWaitingYes(t *testing.T) {
	e := newDeleteEnv(t)
	out := e.plainArchive("TwoAccounts")
	gmailWaits(t, out, "imap.gmail.com")
	if m := gmailPost(e, out, "yes"); m["ok"] != true {
		t.Fatalf("positive twin: yes: %v", m)
	}
	if kept := gmailanswer.Read(out); !reflect.DeepEqual(kept.Yes, []string{"imap.gmail.com"}) {
		t.Fatalf("positive twin: the yes was not kept: %+v", kept)
	}
	// A later backup that did not read the first store found a second one off.
	gmailWaits(t, out, "imap.gmail.com-1")
	if q, _ := gmailCard(out, true, false); q == "" {
		t.Fatal("the card does not ask for the second store")
	}
	m := gmailPost(e, out, "not-now")
	if m["ok"] != true || m["note"] != gmailNotNowNote {
		t.Fatalf("Not now: %v", m)
	}
	if kept := gmailanswer.Read(out); !kept.NotNow || len(kept.Yes) != 0 {
		t.Fatalf("after Not now a yes still waits for the next backup: %+v", kept)
	}
}

// covers: MA-1323, R45
// The card's Yes and Not now carry the archive the page drew the question for (the B3 adversarial
// pass, round 2's G5, turned from its probe TestB3AdvR2StalePageYesBindsToTheArchiveThereNow): a
// page left open while the folder came to hold another archive (a drive of a rotation swapped at
// the same letter) is refused, "There is no Gmail question for this archive now. Reload the page.",
// and nothing is kept for the archive the folder holds now; a request naming no archive is refused
// the same way. The page draws the id on the question's box, and its script sends it. Positive
// twin first: the id the page drew answers.
func TestAStalePagesAnswerIsRefused(t *testing.T) {
	e := newDeleteEnv(t)
	out := e.plainArchive("Rotation")
	gmailWaits(t, out, "imap.gmail.com")
	idA := state.ArchiveID(out)
	assure.Reached(t, idA, "archive A's id")
	if page := e.page(); !strings.Contains(page, `<div class="actions" data-gmail-archive="`+idA+`"><button class="btn secondary" type="button" data-gmail-answer="yes" data-path="`+out+`">`) {
		t.Fatal("the card's question box does not carry the archive's id beside its buttons")
	}
	at := strings.Index(dashboardJS, "querySelectorAll('[data-gmail-answer]')")
	if at < 0 || !strings.Contains(dashboardJS[at:], "archive:b.parentElement.dataset.gmailArchive") {
		t.Fatal("the page's script does not send the archive the question was drawn for")
	}
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "not-now", "archive": idA}); m["ok"] != true {
		t.Fatalf("positive twin: the page's own archive: %v", m)
	}
	if err := gmailanswer.Keep(out, gmailanswer.Answer{Archive: idA}); err != nil { // nothing kept again
		t.Fatal(err)
	}

	// Drive swap: the folder now holds B (its own id, the same store off).
	now := time.Now().UTC()
	idB := state.NewArchiveID()
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, GmailOff: []string{"imap.gmail.com"}, Archive: idB}); err != nil {
		t.Fatal(err)
	}
	nothingKept := func() bool { k := gmailanswer.Read(out); return !k.NotNow && len(k.Yes) == 0 }
	for _, body := range []map[string]any{
		{"path": out, "answer": "yes", "archive": idA},
		{"path": out, "answer": "yes"},
	} {
		_, m := e.post("/api/gmail-answer", body)
		rc := 0
		if m["ok"] == false {
			rc = 2
		}
		msg, _ := m["error"].(string)
		assure.Refused(t, rc, msg, assure.Names("no Gmail question for this archive now", "Reload the page"), assure.NoSideEffect(nothingKept))
	}
}

// covers: MA-1326, R45, R37
// Spending a used yes (gmailanswer.Settle) after the Desktop's Run now rewrites the archive's job
// file, so it takes the lock Save settings and the Gmail answer take on that file (the B3
// adversarial pass, round 2's G8): the Run now's hooks carry a hold of jobFileMu, which
// wizard.RunJobInProcess takes around Settle (proven there by MA-1326's wizard test). Positive twin
// first: the lock is free before the hold and after its release.
func TestTheRunNowsSettleTakesTheJobFileLock(t *testing.T) {
	e := newDeleteEnv(t)
	if !jobFileMu.TryLock() {
		t.Fatal("positive twin: the job file lock is already held")
	}
	jobFileMu.Unlock()
	rn, _, endWait := e.d.runNowHooks(e.d.cfg.goos())
	defer endWait()
	hold := assure.Reached(t, rn.HoldJobFile, "the Run now's hold of the job file lock")
	release := hold()
	if jobFileMu.TryLock() {
		jobFileMu.Unlock()
		release()
		t.Fatal("the Run now's hold does not take the lock Save settings takes")
	}
	release()
	if !jobFileMu.TryLock() {
		t.Fatal("the hold's release left the job file lock held")
	}
	jobFileMu.Unlock()
}
