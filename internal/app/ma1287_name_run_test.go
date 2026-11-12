package app

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// covers: MA-1287
// The C3 name pass's ATK-2 (N-02), brought in through real runs of real temporary Thunderbird
// profiles: account A named with account B's folder words ("the Thunderbird account kept in the
// folder imap.b.example") beside B with no name never makes the two read as one, on screen or from
// the record. Positive twin first: a name of sam@a.example reads apart.
func TestANameCannotSpellAnotherAccountsFolderWords(t *testing.T) {
	inbox := func() tbFolder {
		return tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)}
	}
	build := func(nameA string) []string {
		p := newTBProfile(t,
			tbAcct{n: 1, host: "imap.a.example", typ: "imap", dirRel: "ImapMail/imap.a.example", extra: map[string]string{"name": `"` + nameA + `"`}},
			tbAcct{n: 2, host: "imap.b.example", typ: "imap", dirRel: "ImapMail/imap.b.example"})
		sa, sb := filepath.Join(p, "ImapMail", "imap.a.example"), filepath.Join(p, "ImapMail", "imap.b.example")
		writeTBCache(t, p, tbStoreAt(t, sa, inbox()), tbStoreAt(t, sb, inbox()))
		out := tmpDir(t)
		accts, more := TBAccounts(runTB(t, Options{Inputs: []string{sa, sb}, Out: out}))
		lr, _, err := state.ReadLastRun(out)
		if err != nil {
			t.Fatal(err)
		}
		// A name that spells the fallback is no name: neither the screen's accounts nor the
		// record keep it (the words alone guarding it would leave it in the record).
		if state.TBNameIsFallback(nameA) {
			for _, a := range append(append([]state.TBAccount(nil), accts...), lr.Thunderbird...) {
				if a.Name != "" {
					t.Errorf("%q is kept as %q", nameA, a.Name)
				}
			}
		}
		return append(state.TBLines(accts, more, state.TBSay{}), state.TBLines(lr.Thunderbird, lr.ThunderbirdMore, state.TBSay{})...)
	}
	// Two behind: each account's headline, then the one shared remedy (R2), for the run and the record.
	if got := build("sam@a.example"); len(got) != 6 || got[0] == got[1] || got[3] == got[4] {
		t.Fatalf("positive twin: two accounts read apart: %q", got)
	}
	for _, name := range []string{"the Thunderbird account kept in the folder imap.b.example", "The Thunderbird Account Kept In The Folder imap.b.example"} {
		got := build(name)
		if len(got) != 6 || got[0] == got[1] || got[3] == got[4] {
			t.Errorf("%q: two accounts read as one: %q", name, got)
		}
	}
}
