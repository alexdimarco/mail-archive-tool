package tbcomplete

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/thunderbird"
)

// covers: MA-1181
// The run's lead is set by its weakest account (§4.2's fold, the §17 re-read, the M7, M9 and L4
// folds), the verdict core's half of this row (the words and surfaces are C3's): any behind or
// limited account → LeadSoFar ("Backed up what Thunderbird has so far."); otherwise any account
// that may not say complete → LeadFinished ("Backup finished."); otherwise LeadComplete ("Export
// complete."). An account may say complete only when it is not_applicable, or caught_up with
// nothing missing, not stale, no TC-22 cap note, no deleted copies (expungedBytes > 0) and no
// not-kept folder that gets a WARN; Spam and Trash not kept and Gmail labels not kept are notes and
// do not block it (§0's lead's call). A run with no Thunderbird store keeps today's words. Positive
// twin first: one caught-up account says complete.
func TestTheWeakestAccountSetsTheLead(t *testing.T) {
	complete := Result{Verdict: CaughtUp}
	if !complete.MaySayComplete() || RunLead([]Result{complete}) != LeadComplete {
		t.Fatalf("the positive twin")
	}
	na := Result{Verdict: NotApplicable, NotKept: []FolderCount{{Path: "x", Listed: 1}}}
	for name, c := range map[string]struct {
		rs   []Result
		want Lead
	}{
		"no Thunderbird store":                {nil, LeadComplete},
		"not_applicable only":                 {[]Result{na, na}, LeadComplete},
		"caught up and not_applicable":        {[]Result{complete, na}, LeadComplete},
		"caught up and unknown":               {[]Result{complete, {Verdict: Unknown, Reason: ReasonPanorama}}, LeadFinished},
		"unknown, then behind":                {[]Result{{Verdict: Unknown}, {Verdict: Behind}}, LeadSoFar},
		"behind, then unknown":                {[]Result{{Verdict: Behind}, {Verdict: Unknown}}, LeadSoFar},
		"limited":                             {[]Result{complete, {Verdict: Limited}}, LeadSoFar},
		"caught up, 1 missing":                {[]Result{{Verdict: CaughtUp, Missing: 1}}, LeadFinished},
		"caught up, stale":                    {[]Result{{Verdict: CaughtUp, Stale: true}}, LeadFinished},
		"caught up, cap note":                 {[]Result{{Verdict: CaughtUp, CapNote: 1000}}, LeadFinished},
		"caught up, deleted copies":           {[]Result{{Verdict: CaughtUp, DeletedCopies: true}}, LeadFinished},
		"caught up, a folder not kept (WARN)": {[]Result{{Verdict: CaughtUp, NotKept: []FolderCount{{Path: "P", Listed: 5}}}}, LeadFinished},
		"caught up, Spam and Trash notes":     {[]Result{{Verdict: CaughtUp, DefaultNotKept: []FolderCount{{Path: "Trash", Listed: 5}}}}, LeadComplete},
		"caught up, Gmail labels note":        {[]Result{{Verdict: CaughtUp, Gmail: true, LabelsNotKept: 40}}, LeadComplete},
		"a verdict nobody defined":            {[]Result{{Verdict: Verdict(99)}}, LeadFinished},
	} {
		if got := RunLead(c.rs); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	var zero Lead
	if zero != LeadFinished {
		t.Errorf("the lead's zero value is %v, want finished: an unset lead never says complete", zero)
	}
	if LeadFinished.String() != "finished" || LeadSoFar.String() != "so_far" || LeadComplete.String() != "complete" || Lead(9).String() != "finished" {
		t.Errorf("the leads' log names")
	}
}

// covers: MA-1181, MA-1175, MA-1196
// The owner-shaped run, through the real readers (C0c's account match, B1's GmailForStore, C1a's
// ReadCache and ReadProfile) and a real temp profile, counts only, invented names: one Gmail store
// with All Mail and about 352,000 listed in all, one IMAP store that is not Gmail, and Local
// Folders, which is not_applicable. Everything on disk → "Backup complete." (was "Export complete."); the other IMAP store
// behind → "Backed up what Thunderbird has so far."; Gmail with 1,670 of 238,169 (the owner's first
// backup) → the same.
func TestTheOwnerShapedRun(t *testing.T) {
	p, gstore := newProfile(t, "imap.gmail.com")
	ostore := filepath.Join(p, "ImapMail", "imap.example.com")
	lstore := filepath.Join(p, "Mail", "Local Folders")
	mkdir(t, ostore)
	mkdir(t, lstore)
	write(t, filepath.Join(p, "prefs.js"), strings.Join([]string{
		`// Mozilla User Preferences`, ``,
		`user_pref("mail.account.account1.server", "server1");`,
		`user_pref("mail.account.account2.server", "server2");`,
		`user_pref("mail.account.account3.server", "server3");`,
		`user_pref("mail.accountmanager.accounts", "account1,account2,account3");`,
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail.com");`,
		`user_pref("mail.server.server1.hostname", "imap.gmail.com");`,
		`user_pref("mail.server.server1.is_gmail", true);`,
		`user_pref("mail.server.server1.type", "imap");`,
		`user_pref("mail.server.server2.directory-rel", "[ProfD]ImapMail/imap.example.com");`,
		`user_pref("mail.server.server2.hostname", "imap.example.com");`,
		`user_pref("mail.server.server2.type", "imap");`,
		`user_pref("mail.server.server3.directory-rel", "[ProfD]Mail/Local Folders");`,
		`user_pref("mail.server.server3.hostname", "Local Folders");`,
		`user_pref("mail.server.server3.type", "none");`, ``,
	}, "\n"))
	key := func(store, name string) string {
		parts := strings.Split(name, "/")
		for i := range parts[:len(parts)-1] {
			parts[i] += ".sbd"
		}
		parts[len(parts)-1] += ".msf"
		return filepath.Join(append([]string{store}, parts...)...)
	}
	pairs := []pair{{gstore, root()}}
	for _, n := range ownerGmailFolders() {
		pairs = append(pairs, pair{key(gstore, n.name), n.f})
	}
	pairs = append(pairs, pair{ostore, root()},
		pair{key(ostore, "INBOX"), folder(FlagInbox, 1200, 1200)},
		pair{key(ostore, "Sent"), folder(FlagSentMail, 3402, 3402)},
		pair{key(ostore, "Junk Email"), notKept(FlagJunk, 234)},
		pair{lstore, root()},
		pair{key(lstore, "Inbox"), fields{"flags": FlagInbox, "totalMsgs": 12, "pendingMsgs": 0, "expungedBytes": 0, "folderSize": 12000}})
	writeCache(t, p, pairs...)

	assess := func(store string, disk OnDisk) Result {
		t.Helper()
		in := Input{Account: filepath.Base(store), WholeStore: true, Now: vNow, OnDisk: disk, Profile: ReadProfile(p)}
		if acct, err := thunderbird.FindAccountForStore(p, store); err == nil {
			in.Matched, in.Type = true, acct.Type
		}
		in.Gmail, _ = thunderbird.GmailForStore(p, store)
		c, err := ReadCache(p)
		if err != nil {
			t.Fatal(err)
		}
		if in.Store, err = c.Store(store); err != nil {
			t.Fatal(err)
		}
		return Assess(in)
	}
	full := OnDisk{Distinct: 238169}
	other := OnDisk{Folders: map[string]int64{"INBOX": 1200, "Sent": 3402}}
	g, o, l := assess(gstore, full), assess(ostore, other), assess(lstore, OnDisk{Folders: map[string]int64{"Inbox": 12}})
	want(t, "Gmail", g, CaughtUp, NoReason)
	want(t, "the other IMAP account", o, CaughtUp, NoReason)
	want(t, "Local Folders", l, NotApplicable, NoReason)
	if !g.Gmail || g.Listed != 238169 || o.Gmail || o.Listed != 4602 || len(o.DefaultNotKept) != 1 {
		t.Fatalf("Gmail %+v; other %+v", g, o)
	}
	if lead := RunLead([]Result{g, o, l}); lead != LeadComplete {
		t.Fatalf("everything on disk: %v", lead)
	}

	behind := OnDisk{Folders: map[string]int64{"INBOX": 1200, "Sent": 0}}
	o = assess(ostore, behind)
	want(t, "the other IMAP account, Sent 0 of 3,402", o, Behind, NoReason)
	if lead := RunLead([]Result{g, o, l}); lead != LeadSoFar {
		t.Fatalf("one account behind: %v", lead)
	}
	g = assess(gstore, OnDisk{Distinct: 1670})
	want(t, "Gmail, 1,670 of 238,169", g, Behind, NoReason)
	if lead := RunLead([]Result{g, assess(ostore, other), l}); lead != LeadSoFar {
		t.Fatalf("Gmail behind: %v", lead)
	}
}
