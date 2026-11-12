package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The account's name through real runs (the owner's ruling of 2026-11-08 on the C3 friction
// review's M1): a temp Thunderbird profile whose prefs.js names each account as Thunderbird's
// account wizard does (mail.server.<key>.name, the email address), read through C0c's reader.

// recordTBEntry is the first `thunderbird` entry of out's last-run record as it is on disk.
func recordTBEntry(t *testing.T, out string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, state.LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Thunderbird []map[string]any `json:"thunderbird"`
	}
	if err := json.Unmarshal(data, &rec); err != nil || len(rec.Thunderbird) == 0 {
		t.Fatalf("the record's thunderbird: %v %s", err, data)
	}
	return rec.Thunderbird[0]
}

// covers: MA-1284
// Every surface names the account as Thunderbird shows it (the owner's ruling of 2026-11-08 and the
// lead's call: not only those that see the live run): the run's words on screen (TBAccounts, what
// the wizard, Run now and the command line say), and, from the plaintext record and history it
// wrote, `status`, Status & health (one producer), the archive card and Recent backups. The record
// and the history keep `account` as the store folder, with `name` beside it; `-json` carries both.
// Positive twin first: a profile with no name line is said by its folder everywhere, and its record
// entry has no `name`.
func TestEverySurfaceSaysThunderbirdsNameForTheAccount(t *testing.T) {
	inbox := tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)}
	_, plain := imapProfile(t, "imap.qzname.example", nil, inbox)
	out := tmpDir(t)
	accts, _ := TBAccounts(runTB(t, Options{Inputs: []string{plain}, Out: out}))
	const folderWords = "in the Thunderbird account kept in the folder imap.qzname.example so far"
	if len(accts) != 1 || accts[0].Name != "" || !strings.Contains(state.TBHeadline(accts[0], state.TBSay{}), folderWords) {
		t.Fatalf("positive twin: no name line: %+v", accts)
	}
	if e := recordTBEntry(t, out); e["account"] != "imap.qzname.example" || e["name"] != nil {
		t.Fatalf("positive twin: the record's entry: %v", e)
	}
	if rep := health.Assess(health.Gather(out, ""), time.Now()); !strings.Contains(strings.Join(rep.Reasons, "\n"), folderWords) {
		t.Fatalf("positive twin: status: %q", rep.Reasons)
	}

	_, store := imapProfile(t, "imap.qzname.example", map[string]string{"name": `"qzsam@qzname.example"`}, inbox)
	out = tmpDir(t)
	res := runTB(t, Options{Inputs: []string{store}, Out: out})
	accts, more := TBAccounts(res)
	const nameWords = "Thunderbird has downloaded about 3 of 40 messages in qzsam@qzname.example so far"
	if lines := state.TBLines(accts, more, state.TBSay{}); len(lines) != 1 || !strings.HasPrefix(lines[0], nameWords+".") {
		t.Errorf("the screen's words: %q", lines)
	}
	if e := recordTBEntry(t, out); e["account"] != "imap.qzname.example" || e["name"] != "qzsam@qzname.example" {
		t.Errorf("the record's entry: %v", e)
	}
	lr, _, _ := state.ReadLastRun(out)
	if rl := state.TBLines(lr.Thunderbird, lr.ThunderbirdMore, state.TBSay{}); len(rl) != 1 || !strings.HasPrefix(rl[0], nameWords+".") {
		t.Errorf("the record's words: %q", rl)
	}
	if b := state.TBCardBanner(lr.Thunderbird); !strings.HasPrefix(b, "Thunderbird is still downloading qzsam@qzname.example: about 3 of 40 messages") {
		t.Errorf("the card: %q", b)
	}
	rep := health.Assess(health.Gather(out, ""), time.Now())
	if got := strings.Join(rep.Reasons, "\n"); !strings.Contains(got, "WARN: "+nameWords+".") || strings.Contains(got, "kept in the folder") {
		t.Errorf("status: %q", rep.Reasons)
	}
	if j := health.JSON(health.Gather(out, ""), rep); len(j.ThunderbirdCompleteness) != 1 || j.ThunderbirdCompleteness[0].Account != "imap.qzname.example" || j.ThunderbirdCompleteness[0].Name != "qzsam@qzname.example" {
		t.Errorf("-json: %+v", j.ThunderbirdCompleteness)
	}
	h, err := state.ReadRunHistory(out)
	if err != nil || len(h) == 0 || h[len(h)-1].TB == nil || len(*h[len(h)-1].TB) != 1 {
		t.Fatalf("the history: %+v %v", h, err)
	}
	c := (*h[len(h)-1].TB)[0]
	if s, _ := state.TBCompactSentence(c); c.Account != "imap.qzname.example" || s != nameWords {
		t.Errorf("Recent backups: %+v %q", c, s)
	}
}

// covers: MA-1284, MA-1186
// An encrypted archive writes no name (O1, unchanged): after a run of a named account, the name is
// on screen only, and no file in the archive folder holds it. Positive twin first: the screen says
// it.
func TestAnEncryptedRunWritesNoName(t *testing.T) {
	_, store := imapProfile(t, "imap.qzencname.example", map[string]string{"name": `"qzencsam@qzencname.example"`},
		tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)})
	out, key := encryptedOut(t)
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Mode: export.Incremental}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if accts, more := TBAccounts(res); len(accts) != 1 || !strings.Contains(strings.Join(state.TBLines(accts, more, state.TBSay{}), "\n"), " qzencsam@qzencname.example ") {
		t.Fatalf("positive twin: the screen's words: %+v", accts)
	}
	n := 0
	err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "qzencsam") || strings.Contains(d.Name(), "qzencsam") {
			t.Errorf("%s holds the account's name", p)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("the walk of the archive: %d files, %v", n, err)
	}
}

// covers: MA-1284
// Two Gmail accounts (store folders imap.gmail.com and imap.gmail.com-1) read as two different
// addresses on screen (the owner's ruling); two accounts Thunderbird shows by the same name are
// both said by their folders instead, so they never read as one, on screen and from the record
// alike. Positive twin first: two different names.
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
	// lines is the run's words on screen, then its record's words.
	lines := func(stores []string) []string {
		out := tmpDir(t)
		accts, more := TBAccounts(runTB(t, Options{Inputs: stores, Out: out}))
		lr, _, _ := state.ReadLastRun(out)
		return append(state.TBLines(accts, more, state.TBSay{}), state.TBLines(lr.Thunderbird, lr.ThunderbirdMore, state.TBSay{})...)
	}
	got := lines(build("sam.walker@gmail.com", "sam.work@gmail.com"))
	if len(got) != 4 || !strings.Contains(got[0], " sam.walker@gmail.com ") || !strings.Contains(got[1], " sam.work@gmail.com ") ||
		!strings.Contains(got[2], " sam.walker@gmail.com ") || !strings.Contains(got[3], " sam.work@gmail.com ") {
		t.Fatalf("positive twin: two addresses: %q", got)
	}
	for _, same := range [][2]string{{"Gmail", "Gmail"}, {"Gmail", "gmail"}, {"Sam​ Mail", "Sam Mail"}} {
		got = lines(build(same[0], same[1]))
		if len(got) != 4 || !strings.Contains(got[0], "kept in the folder imap.gmail.com ") || !strings.Contains(got[1], "kept in the folder imap.gmail.com-1 ") ||
			!strings.Contains(got[2], "kept in the folder imap.gmail.com ") || !strings.Contains(got[3], "kept in the folder imap.gmail.com-1 ") {
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
