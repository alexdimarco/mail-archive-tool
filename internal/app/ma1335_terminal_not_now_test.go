package app

import (
	"context"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1335, R45
// The B3 adversarial pass, round 3's H6: a Not now given in a run (at a terminal) holds for the
// rest of that same run. A yes kept for one Gmail account (the card's) waits; a backup typed at a
// terminal reads a second Gmail account of the archive first and asks; the person says Not now (the
// terminal keeps it and takes the waiting yes back, gmailanswer.NotNowHere). The run then switches
// nothing when it reaches the first account: the archive stays at version 6, as "Nothing changed"
// says, and this computer keeps the Not now and no yes. Positive twin first: with no Not now in the
// run (nobody to ask), the kept yes switches the first account.
func TestANotNowInTheRunHoldsForTheRestOfIt(t *testing.T) {
	setup := func() (out, s1, s2 string, opts Options) {
		ma1316Settings(t)
		p := newTBProfile(t,
			tbAcct{n: 1, host: "imap.gmail.com", typ: "imap", dirRel: "ImapMail/imap.gmail.com", extra: map[string]string{"is_gmail": "true"}},
			tbAcct{n: 2, host: "imap.gmail.com", typ: "imap", dirRel: "ImapMail/imap.gmail.com-1", extra: map[string]string{"is_gmail": "true"}})
		s1, s2 = filepath.Join(p, "ImapMail", "imap.gmail.com"), filepath.Join(p, "ImapMail", "imap.gmail.com-1")
		writeTBCache(t, p, tbStoreAt(t, s1, gmailFolders(6)...), tbStoreAt(t, s2, gmailFolders(6)...))
		out = tmpDir(t)
		gmailDetectOff = true
		runTB(t, Options{Inputs: []string{s1, s2}, Out: out})
		gmailDetectOff = false
		res := runTB(t, Options{Inputs: []string{s1, s2}, Out: out, Unattended: true})
		id := state.ArchiveID(out)
		if len(res.Gmail.Off) != 2 || id == "" {
			t.Fatalf("premise: both accounts off: %+v (id %q)", res.Gmail, id)
		}
		if err := gmailanswer.Keep(out, gmailanswer.Answer{Archive: id, Yes: []string{"imap.gmail.com"}}); err != nil {
			t.Fatal(err)
		}
		kept := gmailanswer.Read(out)
		opts = Options{Inputs: []string{s2, s1}, Out: out, Mode: export.Incremental,
			GmailYesStores: kept.Yes, GmailYesFor: kept.Archive, GmailNotNow: kept.NotNow}
		return out, s1, s2, opts
	}

	out, _, _, twin := setup()
	if _, err := Run(context.Background(), twin, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if v, on := manifestOnDisk(t, out); v != 7 || len(on) != 1 {
		t.Fatalf("positive twin: the kept yes did not switch the first account (version %d, on %v)", v, on)
	}

	out, _, _, opts := setup()
	asked := 0
	opts.AskGmail = func(string) bool {
		asked++
		_ = gmailanswer.NotNowHere(out, state.ArchiveID(out)) // what the terminal does on a typed Not now
		return false
	}
	r, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Fatalf("premise: asked %d times", asked)
	}
	if v, on := manifestOnDisk(t, out); v != 6 || len(on) != 0 || len(r.Gmail.Switched) != 0 {
		t.Fatalf("the run where the person said Not now switched %v (version %d, on %v)", r.Gmail.Switched, v, on)
	}
	if kept := gmailanswer.Read(out); !kept.NotNow || len(kept.Yes) != 0 {
		t.Fatalf("this computer keeps %+v after the Not now, want the Not now and no yes", kept)
	}
}
