package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The account's name through real runs (the owner's ruling of 2026-11-08 on the C3 friction
// review's M1): a temp Thunderbird profile whose prefs.js names each account as Thunderbird's
// account wizard does (mail.server.<key>.name, the email address), read through C0c's reader.

// covers: MA-1284
// The run's words on screen (TBAccounts, what the wizard, Run now and the command line say) name
// the account as Thunderbird shows it; the record, `status` and `-json` keep the store folder and
// never the name, so the words read from the record name it by its folder. Positive twin first: a
// profile with no name line is said by its folder on screen too.
func TestTheRunSaysThunderbirdsNameForTheAccount(t *testing.T) {
	inbox := tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)}
	_, plain := imapProfile(t, "imap.qzname.example", nil, inbox)
	accts, _ := TBAccounts(runTB(t, Options{Inputs: []string{plain}}))
	if len(accts) != 1 || accts[0].Name != "" || !strings.Contains(state.TBHeadline(accts[0], state.TBSay{}), "in the Thunderbird account kept in the folder imap.qzname.example so far") {
		t.Fatalf("positive twin: no name line: %+v", accts)
	}
	_, store := imapProfile(t, "imap.qzname.example", map[string]string{"name": `"qzsam@qzname.example"`}, inbox)
	out := tmpDir(t)
	res := runTB(t, Options{Inputs: []string{store}, Out: out})
	accts, more := TBAccounts(res)
	lines := state.TBLines(accts, more, state.TBSay{})
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "Thunderbird has downloaded about 3 of 40 messages in qzsam@qzname.example so far.") {
		t.Errorf("the screen's words: %q", lines)
	}
	data, err := os.ReadFile(filepath.Join(out, state.LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "qzsam") || !strings.Contains(string(data), "imap.qzname.example") {
		t.Errorf("the record holds the name, or not the folder: %s", data)
	}
	lr, _, _ := state.ReadLastRun(out)
	if rl := state.TBLines(lr.Thunderbird, lr.ThunderbirdMore, state.TBSay{}); len(rl) != 1 || !strings.Contains(rl[0], "in the Thunderbird account kept in the folder imap.qzname.example so far") {
		t.Errorf("the record's words: %q", rl)
	}
	rep := health.Assess(health.Gather(out, ""), time.Now())
	if strings.Contains(strings.Join(rep.Reasons, "\n"), "qzsam") {
		t.Errorf("status says the name: %q", rep.Reasons)
	}
	if j := health.JSON(health.Gather(out, ""), rep); len(j.ThunderbirdCompleteness) != 1 || j.ThunderbirdCompleteness[0].Account != "imap.qzname.example" || j.ThunderbirdCompleteness[0].Name != "" {
		t.Errorf("-json: %+v", j.ThunderbirdCompleteness)
	}
	if h, _ := os.ReadFile(filepath.Join(out, state.RunHistoryName)); strings.Contains(string(h), "qzsam") {
		t.Errorf("the run history holds the name")
	}
}

// covers: MA-1284
// Two Gmail accounts (store folders imap.gmail.com and imap.gmail.com-1) read as two different
// addresses on screen (the owner's ruling); two accounts Thunderbird shows by the same name are
// both said by their folders instead, so they never read as one. Positive twin first: two
// different names.
func TestTwoAccountsNeverReadAsOne(t *testing.T) {
	build := func(name1, name2 string) []string {
		a1, a2 := map[string]string{"is_gmail": "true", "name": `"` + name1 + `"`}, map[string]string{"is_gmail": "true", "name": `"` + name2 + `"`}
		p := newTBProfile(t,
			tbAcct{n: 1, host: "imap.gmail.com", typ: "imap", dirRel: "ImapMail/imap.gmail.com", extra: a1},
			tbAcct{n: 2, host: "imap.gmail.com", typ: "imap", dirRel: "ImapMail/imap.gmail.com-1", extra: a2})
		s1, s2 := filepath.Join(p, "ImapMail", "imap.gmail.com"), filepath.Join(p, "ImapMail", "imap.gmail.com-1")
		writeTBCache(t, p, tbStoreAt(t, s1, gmailFolders(6)...), tbStoreAt(t, s2, gmailFolders(6)...))
		return []string{s1, s2}
	}
	lines := func(stores []string) []string {
		accts, more := TBAccounts(runTB(t, Options{Inputs: stores}))
		return state.TBLines(accts, more, state.TBSay{})
	}
	got := lines(build("sam.walker@gmail.com", "sam.work@gmail.com"))
	if len(got) != 2 || !strings.Contains(got[0], " sam.walker@gmail.com ") || !strings.Contains(got[1], " sam.work@gmail.com ") {
		t.Fatalf("positive twin: two addresses: %q", got)
	}
	for _, same := range [][2]string{{"Gmail", "Gmail"}, {"Gmail", "gmail"}, {"Sam​ Mail", "Sam Mail"}} {
		got = lines(build(same[0], same[1]))
		if len(got) != 2 || !strings.Contains(got[0], "kept in the folder imap.gmail.com ") || !strings.Contains(got[1], "kept in the folder imap.gmail.com-1 ") {
			t.Errorf("two accounts named %q and %q: %q", same[0], same[1], got)
		}
	}
}

// covers: MA-1196
// Fold check 3's N1: a Thunderbird store that source detection hands to another reader (the
// Evolution reader: a maildir-format account with folders named "cur" and ".x", ADV-C2-16), copied
// outside its profile, is "can't tell" when its folder holds any .msf file, never no result, so the
// run never says complete over it. Positive twin first: a real Evolution store (no .msf) under no
// Thunderbird profile is still told nothing.
func TestAStoreAnotherReaderOpenedWithMsfIsCantTell(t *testing.T) {
	evo := filepath.Join(tmpDir(t), "evolution", "mail", "local")
	tbWrite(t, filepath.Join(evo, "cur", "1.eml"), tbMsg("e-1", ""))
	tbWrite(t, filepath.Join(evo, ".Sub", "cur", "1.eml"), tbMsg("e-2", ""))
	if res := runTB(t, Options{Inputs: []string{evo}}); len(res.Thunderbird) != 0 || tbcomplete.RunLead(res.Thunderbird) != tbcomplete.LeadComplete {
		t.Fatalf("positive twin: an Evolution store was told: %+v", res.Thunderbird)
	}
	for _, msf := range []string{"INBOX.msf", "cur.MSF"} {
		copied := filepath.Join(tmpDir(t), "copied-store")
		tbWrite(t, filepath.Join(copied, "INBOX", "cur", "1.eml"), tbMsg("c-1", ""))
		tbWrite(t, filepath.Join(copied, "cur", "cur", "1.eml"), tbMsg("c-2", ""))
		tbWrite(t, filepath.Join(copied, ".x", "cur", "1.eml"), tbMsg("c-3", ""))
		tbWrite(t, filepath.Join(copied, msf), "")
		if tb := planTBOther(copied); tb == nil {
			t.Errorf("%s: a store with no profile holding a .msf was not planned", msf)
		}
		res := runTB(t, Options{Inputs: []string{copied}})
		if len(res.Thunderbird) != 1 || res.Thunderbird[0].Verdict != tbcomplete.Unknown || tbcomplete.RunLead(res.Thunderbird) == tbcomplete.LeadComplete {
			t.Errorf("%s: a copied store another reader opened: %+v (lead %v)", msf, res.Thunderbird, tbcomplete.RunLead(res.Thunderbird))
		}
	}
}

// covers: MA-1196
// Fold check 3's N2a and N2b: a store more than six levels below a profile, holding a .msf, is
// "can't tell" (the profile is found by walking up six levels only); a single mbox file given as
// the input, with no profile above it, is "can't tell" with a .msf beside it and a plain mail file
// without one (the positive twin first).
func TestTheMsfRuleBelowAndBesideTheInput(t *testing.T) {
	plain := filepath.Join(tmpDir(t), "mailfolder")
	tbWrite(t, filepath.Join(plain, "Inbox"), "From - Mon Mar  3 09:00:00 2025\n"+tbMsg("n2b-1", "")+"\n")
	if res := runTB(t, Options{Inputs: []string{filepath.Join(plain, "Inbox")}}); len(res.Thunderbird) != 0 {
		t.Fatalf("positive twin: one mbox file with no .msf beside it got %+v", res.Thunderbird)
	}
	withMsf := filepath.Join(tmpDir(t), "mailfolder")
	tbWrite(t, filepath.Join(withMsf, "Inbox"), "From - Mon Mar  3 09:00:00 2025\n"+tbMsg("n2b-2", "")+"\n")
	tbWrite(t, filepath.Join(withMsf, "Inbox.msf"), "")
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{filepath.Join(withMsf, "Inbox")}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonNoMatch {
		t.Errorf("one mbox file with a .msf beside it: %v %v, want unknown no_match", r.Verdict, r.Reason)
	}

	p := newTBProfile(t, tbAcct{n: 1, host: "imap.c2.example", typ: "imap", dirRel: "ImapMail/imap.c2.example"})
	deep := filepath.Join(p, "a", "b", "c", "d", "e", "f", "g", "store")
	tbWrite(t, filepath.Join(deep, "Inbox"), "From - Mon Mar  3 09:00:00 2025\n"+tbMsg("n2a-1", "")+"\n")
	tbWrite(t, filepath.Join(deep, "Inbox.msf"), "")
	if _, ok := findTBProfile(deep); ok {
		t.Fatalf("the deep store's profile was found: the test does not reach past six levels")
	}
	if r := onlyResult(t, runTB(t, Options{Inputs: []string{deep}})); r.Verdict != tbcomplete.Unknown || r.Reason != tbcomplete.ReasonNoMatch {
		t.Errorf("a store more than six levels below its profile, with a .msf: %v %v, want unknown no_match", r.Verdict, r.Reason)
	}
}
