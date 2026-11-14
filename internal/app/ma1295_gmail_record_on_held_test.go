package app

import (
	"reflect"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1295, R45
// What a plaintext run keeps in its last-run record so Settings can show the Gmail question again
// after a Not now, and say an archive is switched on (the B3 friction review's F1 and F6).
// Positive twin first: a store that is not Gmail leaves no Gmail field. A store held back by a Not
// now is kept as held (gmail_held), not as waiting (gmail_off), so the card still asks no more; a
// store switched on, by this run or an earlier one, is kept as on (gmail_on), and the run's outcome
// names it.
func TestTheLastRunRecordKeepsTheGmailStoresHeldAndOn(t *testing.T) {
	_, plain := imapProfile(t, "imap.example.org", nil, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("p", 1)})
	out := tmpDir(t)
	runTB(t, Options{Inputs: []string{plain}, Out: out})
	if lr, _, _ := state.ReadLastRun(out); len(lr.GmailOff)+len(lr.GmailHeld)+len(lr.GmailOn) != 0 {
		t.Fatalf("positive twin: a store that is not Gmail left %+v %+v %+v", lr.GmailOff, lr.GmailHeld, lr.GmailOn)
	}

	store, out := existingGmailArchive(t)
	res := runTB(t, Options{Inputs: []string{store}, Out: out, GmailNotNow: true})
	lr, _, _ := state.ReadLastRun(out)
	if len(lr.GmailOff) != 0 || !reflect.DeepEqual(lr.GmailHeld, res.Gmail.Off) || len(lr.GmailHeld) != 1 || len(lr.GmailOn) != 0 {
		t.Fatalf("a Not now: off %v, held %v, on %v", lr.GmailOff, lr.GmailHeld, lr.GmailOn)
	}
	held := lr.GmailHeld

	res = runTB(t, Options{Inputs: []string{store}, Out: out, GmailYesStores: held, GmailYesFor: lr.Archive}) // given for this archive (MA-1302)
	if !reflect.DeepEqual(res.Gmail.On, held) || !reflect.DeepEqual(res.Gmail.Switched, held) {
		t.Fatalf("the yes: outcome on %v, switched %v", res.Gmail.On, res.Gmail.Switched)
	}
	if lr, _, _ := state.ReadLastRun(out); !reflect.DeepEqual(lr.GmailOn, held) || len(lr.GmailHeld)+len(lr.GmailOff) != 0 {
		t.Fatalf("after the yes: on %v, held %v, off %v", lr.GmailOn, lr.GmailHeld, lr.GmailOff)
	}
	// A later run: still on, switched nothing.
	res = runTB(t, Options{Inputs: []string{store}, Out: out, Unattended: true})
	if lr, _, _ := state.ReadLastRun(out); !reflect.DeepEqual(lr.GmailOn, held) || len(res.Gmail.Switched) != 0 {
		t.Fatalf("a later run: on %v, switched %v", lr.GmailOn, res.Gmail.Switched)
	}
}
