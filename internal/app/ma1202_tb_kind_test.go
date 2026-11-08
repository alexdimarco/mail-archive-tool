package app

import (
	"context"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// runTB backs inputs up into a fresh archive and gives the run's result.
func runTB(t *testing.T, opts Options) Result {
	t.Helper()
	if opts.Out == "" {
		opts.Out = tmpDir(t)
	}
	opts.Mode = export.Incremental
	res, err := Run(context.Background(), opts, discard(), nil)
	if err != nil {
		t.Fatalf("run of %v: %v", opts.Inputs, err)
	}
	return res
}

// covers: MA-1202
// A partial -input (one mbox file of a store, or a subfolder inside ImapMail/<host>) is unknown,
// partial_input, with nothing counted and Thunderbird's folder list never read: the person chose
// to back up part of the account. The whole store is checked. The store is told by the input
// path as given, never by -copy-first's copy: one mbox file of a store read through its snapshot
// is still that account's part (TC-11).
func TestAPartialInputIsUnknownAndNeverCompared(t *testing.T) {
	_, store := imapProfile(t, "imap.c2.example", nil,
		tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("in", 2)},
		tbFolder{path: "Archive", listed: 1, msgs: tbMsgs("ar", 1)},
		tbFolder{path: "Archive/2024", listed: 1, msgs: tbMsgs("old", 1)})
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{store}})); r.Verdict != tbcomplete.CaughtUp {
		t.Fatalf("positive twin: the whole store is %v (%v)", r.Verdict, r.Reason)
	}
	for _, c := range []struct {
		name      string
		input     string
		copyFirst bool
	}{
		{"one mbox file", filepath.Join(store, "INBOX"), false},
		{"one mbox file, -copy-first", filepath.Join(store, "INBOX"), true},
		{"a subfolder", filepath.Join(store, "Archive.sbd"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			reads := countCacheReads(t, nil)
			res := runTB(t, Options{Inputs: []string{c.input}, CopyFirst: c.copyFirst})
			r := onlyResult(t, res)
			if r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonPartialInput || r.Account != "imap.c2.example" {
				t.Errorf("%s: %v %v account %q, want unknown partial_input for imap.c2.example", c.name, r.Verdict, r.Reason, r.Account)
			}
			if *reads != 0 {
				t.Errorf("%s: the folder cache was read %d times for a part of an account", c.name, *reads)
			}
			if tb := planTB(c.input); tb.counts != nil || tb.facts.Whole || tb.facts.Account == nil {
				t.Errorf("%s: planned %+v: counted, whole, or no account", c.name, tb.facts)
			}
		})
	}
}

// covers: MA-1196
// The kind of account is the matched account's type, never the folder's path (the C1a pass round
// 2's R2-02 note; §2.2's TC-5 fold): a Local Folders store and a POP3 store, under Mail/, get no
// completeness verdict at all, are never counted and never reach the folder cache (the run never
// hands them to the IMAP path), and the record carries nothing for them; a Thunderbird Exchange
// (ews) store under Mail/ is unknown, account_type; an IMAP account whose folder is outside
// ImapMail/ (inside its profile, matched by its directory setting) is checked.
func TestTheAccountsTypeDecidesNeverItsPath(t *testing.T) {
	p := newTBProfile(t,
		tbAcct{n: 1, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"},
		tbAcct{n: 2, host: "pop.c2.example", typ: "pop3", dirRel: "Mail/pop.c2.example"},
		tbAcct{n: 3, host: "ews.c2.example", typ: "ews", dirRel: "Mail/ews.c2.example"},
		tbAcct{n: 4, host: "imap.c2.example", typ: "imap", dirRel: "Mail/custom-imap"})
	local, pop, ews, custom := filepath.Join(p, "Mail", "Local Folders"), filepath.Join(p, "Mail", "pop.c2.example"),
		filepath.Join(p, "Mail", "ews.c2.example"), filepath.Join(p, "Mail", "custom-imap")
	inbox := func(prefix string) tbFolder {
		return tbFolder{path: "Inbox", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs(prefix, 2)}
	}
	// Each store is listed in the folder cache, behind, so a store handed to the IMAP path
	// would show a verdict.
	behind := func(prefix string) tbFolder { f := inbox(prefix); f.listed = 50; return f }
	writeTBCache(t, p, tbStoreAt(t, local, behind("lf")), tbStoreAt(t, pop, behind("pop")),
		tbStoreAt(t, ews, behind("ews")), tbStoreAt(t, custom, inbox("imap")))

	r := onlyResult(t, runTB(t, Options{Inputs: []string{custom}}))
	if r.Verdict != tbcomplete.CaughtUp {
		t.Fatalf("positive twin: an imap account's custom folder outside ImapMail/ is %v (%v)", r.Verdict, r.Reason)
	}
	for _, store := range []string{local, pop} {
		reads := countCacheReads(t, nil)
		out := tmpDir(t)
		res := runTB(t, Options{Inputs: []string{store}, Out: out})
		if len(res.Thunderbird) != 0 {
			t.Errorf("%s got a completeness verdict: %+v", filepath.Base(store), res.Thunderbird)
		}
		if *reads != 0 {
			t.Errorf("%s reached the folder cache (%d reads)", filepath.Base(store), *reads)
		}
		if tb := planTB(store); tb.counts != nil {
			t.Errorf("%s was counted: %+v", filepath.Base(store), tb.counts)
		}
		if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunOK || lr.Thunderbird != nil {
			t.Errorf("%s: record %q %+v", filepath.Base(store), lr.Status, lr.Thunderbird)
		}
		if _, ok := lastRunRaw(t, out)["thunderbird"]; ok {
			t.Errorf("%s: the record has a thunderbird key", filepath.Base(store))
		}
	}
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{ews}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonAccountType {
		t.Errorf("an Exchange store: %v %v, want unknown account_type", r.Verdict, r.Reason)
	}
	// One run of all four: only the two Thunderbird accounts MailArchive judges have results.
	res := runTB(t, Options{Inputs: []string{local, pop, ews, custom}})
	if len(res.Thunderbird) != 2 {
		t.Errorf("a run of all four stores gave %d results, want 2 (ews and imap): %+v", len(res.Thunderbird), res.Thunderbird)
	}
	if got := tbcomplete.RunLead(res.Thunderbird); got != tbcomplete.LeadFinished {
		t.Errorf("the run's lead with an Exchange store: %v, want finished", got)
	}
}

// covers: MA-1196
// The M6 facts come from the run (§2.2's M6 and M6-residual folds): a folder of mbox files with
// no profile above it, no ImapMail part and no .msf is a plain mail folder and gets no verdict;
// the same folder holding a .msf (or an .MSF), a copied ImapMail/<host> with no profile, and a
// store whose profile names no account for it (two accounts on its host) are each unknown,
// no_match.
func TestTheM6FactsComeFromTheRun(t *testing.T) {
	msgs := func(dir string) {
		tbWrite(t, filepath.Join(dir, "Inbox"), "From - Mon Mar  3 09:00:00 2025\n"+tbMsg("m6-"+filepath.Base(dir), "")+"\n")
	}
	plain := filepath.Join(tmpDir(t), "mailfolder")
	msgs(plain)
	if res := runTB(t, Options{Inputs: []string{plain}}); len(res.Thunderbird) != 0 {
		t.Fatalf("positive twin: a plain mail folder got %+v", res.Thunderbird)
	}
	for _, name := range []string{"Inbox.msf", "Inbox.MSF"} {
		withMsf := filepath.Join(tmpDir(t), "mailfolder")
		msgs(withMsf)
		tbWrite(t, filepath.Join(withMsf, name), "")
		if r := onlyResult(t, runTB(t, Options{Inputs: []string{withMsf}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonNoMatch {
			t.Errorf("a folder holding %s: %v %v, want unknown no_match", name, r.Verdict, r.Reason)
		}
	}
	copied := filepath.Join(tmpDir(t), "backup", "ImapMail", "imap.c2.example")
	msgs(copied)
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{copied}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonNoMatch {
		t.Errorf("a copied ImapMail/<host>: %v %v, want unknown no_match", r.Verdict, r.Reason)
	}
	p := newTBProfile(t,
		tbAcct{n: 1, host: "imap.c2.example", typ: "imap"},
		tbAcct{n: 2, host: "imap.c2.example", typ: "imap"})
	ambiguous := filepath.Join(p, "ImapMail", "imap.c2.example")
	writeTBCache(t, p, tbStoreAt(t, ambiguous, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 1, msgs: tbMsgs("amb", 1)}))
	reads := countCacheReads(t, nil)
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{ambiguous}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonNoMatch {
		t.Errorf("two accounts on the store's host: %v %v, want unknown no_match", r.Verdict, r.Reason)
	}
	if *reads != 0 {
		t.Errorf("a store with no account read the folder cache %d times", *reads)
	}
}
