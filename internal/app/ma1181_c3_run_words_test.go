package app

import (
	"context"
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

// Slice C3 end to end: real backup runs of temp Thunderbird profiles (prefs.js and
// compatibility.ini as Thunderbird writes them, folderCache.json, mbox files; counts and invented
// names only), and the words every surface says from them: from the run's result in memory (the
// wizard, Run now, the command line) and from the plaintext record it wrote (`status`, Status &
// health), through one producer (state.TBLines; tbcomplete.RunLead and RecordLead).

// c3Words is a run's lead and lines from memory, and from its record when out holds one.
func c3Words(t *testing.T, res Result, out string) (lead string, lines []string, recLead string, recLines []string) {
	t.Helper()
	accts, more := TBAccounts(res)
	lead, lines = tbcomplete.RunLead(res.Thunderbird).Sentence(), state.TBLines(accts, more, state.TBSay{})
	if lr, st, _ := state.ReadLastRun(out); st == state.LastRunPresent {
		recLead, recLines = tbcomplete.RecordLead(lr.Thunderbird, lr.ThunderbirdMore).Sentence(), state.TBLines(lr.Thunderbird, lr.ThunderbirdMore, state.TBSay{})
	}
	return
}

// ownerProfile is the owner's shape: a Gmail account whose All Mail holds everything, another
// account caught up, and Local Folders. It returns the three stores.
func ownerProfile(t *testing.T, otherSent []string) []string {
	t.Helper()
	p := newTBProfile(t,
		tbAcct{n: 1, host: "imap.gmail.com", typ: "imap", dirRel: "ImapMail/imap.gmail.com", extra: map[string]string{"is_gmail": "true"}},
		tbAcct{n: 2, host: "imap.example.com", typ: "imap", dirRel: "ImapMail/imap.example.com"},
		tbAcct{n: 3, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"})
	gmail := filepath.Join(p, "ImapMail", "imap.gmail.com")
	other := filepath.Join(p, "ImapMail", "imap.example.com")
	local := filepath.Join(p, "Mail", "Local Folders")
	tbWrite(t, filepath.Join(local, "Inbox"), "From - Mon Mar  3 09:00:00 2025\n"+tbMsg("local-1", "")+"\n")
	writeTBCache(t, p, tbStoreAt(t, gmail, gmailFolders(6)...),
		tbStoreAt(t, other, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("o-in", 2)},
			tbFolder{path: "Sent", flags: tbcomplete.FlagSentMail, listed: 3, msgs: otherSent}))
	return []string{gmail, other, local}
}

// covers: MA-1181, MA-1183
// The owner-shaped run (Gmail caught up, the other account caught up, Local Folders never given a
// line) leads "Export complete." on every surface, each account's line saying "everything", from
// memory and from the record alike; status WARNs nothing about it. With the other account's Sent
// 0 of 3 on disk it leads "Backed up what Thunderbird has so far." with both numbers (O2), and
// status WARNs thunderbird_behind. Positive twin first.
func TestTheOwnerShapedRunSaysExportComplete(t *testing.T) {
	out := tmpDir(t)
	res := runTB(t, Options{Inputs: ownerProfile(t, tbMsgs("o-sent", 3)), Out: out})
	lead, lines, recLead, recLines := c3Words(t, res, out)
	want := []string{"Thunderbird has downloaded everything it lists for imap.gmail.com (6 messages).", "Thunderbird has downloaded everything it lists for imap.example.com (5 messages)."}
	if lead != "Export complete." || recLead != "Export complete." || strings.Join(lines, "\n") != strings.Join(want, "\n") || strings.Join(recLines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("positive twin: the owner-shaped run:\n%s %q\n%s %q", lead, lines, recLead, recLines)
	}
	for _, l := range append(lines, recLines...) {
		if strings.Contains(l, "Local Folders") {
			t.Errorf("Local Folders has a completeness line: %q", l)
		}
	}
	// The version that last opened the profile (its compatibility.ini, 153.6.0esr) is recorded for
	// the fix-it words' labels (§6's L5 fold).
	if lr, _, _ := state.ReadLastRun(out); len(lr.Thunderbird) != 2 || lr.Thunderbird[0].ThunderbirdVersion != 153 || lr.Thunderbird[1].ThunderbirdVersion != 153 {
		t.Errorf("the recorded Thunderbird version: %+v", lr.Thunderbird)
	}
	rep := health.Assess(health.Gather(out, ""), time.Now())
	for i, c := range rep.Codes {
		if strings.HasPrefix(c, "thunderbird_") && strings.HasPrefix(rep.Reasons[i], "WARN") {
			t.Errorf("status WARNs for the owner-shaped run: %s", rep.Reasons[i])
		}
	}

	out = tmpDir(t)
	res = runTB(t, Options{Inputs: ownerProfile(t, nil), Out: out})
	lead, lines, recLead, recLines = c3Words(t, res, out)
	behind := "Thunderbird has downloaded about 2 of 5 messages in imap.example.com so far. Furthest behind: Sent 0 of 3."
	if lead != "Backed up what Thunderbird has so far." || recLead != lead || len(lines) != 2 || !strings.HasPrefix(lines[1], behind) || !strings.HasPrefix(recLines[1], behind) {
		t.Errorf("the other account's Sent not downloaded:\n%s %q\n%s %q", lead, lines, recLead, recLines)
	}
	rep = health.Assess(health.Gather(out, ""), time.Now())
	found := false
	for i, c := range rep.Codes {
		found = found || (c == "thunderbird_behind" && strings.HasPrefix(rep.Reasons[i], "WARN: "+behind))
	}
	if !found {
		t.Errorf("status does not WARN thunderbird_behind: %q", rep.Reasons)
	}
}

// covers: MA-1181, MA-1279
// The owner's 2 of 170 messages written without X-Mozilla-Status but with well-formed headers
// (the owner's ruling of 2026-11-08) count for certain: the run leads "Export complete.". The
// same 2 without a Date line (not well-formed) are 2 short for certain, within the tolerance: the
// run leads "Backup finished." and the line says both numbers and why. Positive twin first: all
// 170 with the line.
func TestTwoOf170WellFormedSaysExportComplete(t *testing.T) {
	noStatus := func(id string) string {
		return "From: Alice <alice@example.com>\nTo: Bob <bob@example.com>\nSubject: message " + id +
			"\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + id + "@c2.example>\nContent-Type: text/plain\n\nbody of " + id + "\n"
	}
	run := func(msgs []string) (string, []string) {
		_, s := imapProfile(t, "imap.owner.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 170, msgs: msgs})
		out := tmpDir(t)
		res := runTB(t, Options{Inputs: []string{s}, Out: out})
		_, _, recLead, recLines := c3Words(t, res, out)
		if lead := tbcomplete.RunLead(res.Thunderbird).Sentence(); lead != recLead {
			t.Errorf("the lead in memory %q and from the record %q differ", lead, recLead)
		}
		return recLead, recLines
	}
	if lead, _ := run(tbMsgs("in", 170)); lead != "Export complete." {
		t.Fatalf("positive twin: %q", lead)
	}
	lead, lines := run(append(tbMsgs("in", 168), noStatus("ns-1"), noStatus("ns-2")))
	if lead != "Export complete." || len(lines) != 1 || lines[0] != "Thunderbird has downloaded everything it lists for imap.owner.example (170 messages)." {
		t.Errorf("2 of 170 well-formed: %q %q", lead, lines)
	}
	undated := func(id string) string { return strings.Replace(noStatus(id), "Date: Mon, 03 Mar 2025 09:00:00 +0000\n", "", 1) }
	lead, lines = run(append(tbMsgs("in", 168), undated("ns-1"), undated("ns-2")))
	if lead != "Backup finished." || len(lines) != 1 || lines[0] != "Thunderbird has downloaded about 168 of the 170 messages it lists for imap.owner.example. The rest is likely mail that arrived or was deleted since Thunderbird's count." {
		t.Errorf("2 of 170 not well-formed: %q %q", lead, lines)
	}
}

// covers: MA-1181
// When MailArchive cannot tell (here: Thunderbird's folderCache.json is gone), the run leads
// "Backup finished." and the account's line says why in one sentence; status notes it, never a
// WARN. Positive twin first: the same store with its folder list leads "Export complete.".
func TestCannotTellSaysBackupFinishedAndWhy(t *testing.T) {
	p, s := imapProfile(t, "imap.example.com", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 2, msgs: tbMsgs("in", 2)})
	out := tmpDir(t)
	if lead, _, _, _ := c3Words(t, runTB(t, Options{Inputs: []string{s}, Out: out}), out); lead != "Export complete." {
		t.Fatalf("positive twin: %q", lead)
	}
	if err := os.Remove(filepath.Join(p, tbcomplete.CacheFile)); err != nil {
		t.Fatal(err)
	}
	out = tmpDir(t)
	lead, lines, recLead, recLines := c3Words(t, runTB(t, Options{Inputs: []string{s}, Out: out}), out)
	why := "MailArchive cannot tell whether Thunderbird has downloaded all of imap.example.com, because it could not read Thunderbird's list of folders."
	if lead != "Backup finished." || recLead != lead || len(lines) != 1 || lines[0] != why || len(recLines) != 1 || recLines[0] != why {
		t.Errorf("no folder list: %q %q / %q %q", lead, lines, recLead, recLines)
	}
	rep := health.Assess(health.Gather(out, ""), time.Now())
	for i, c := range rep.Codes {
		if c == "thunderbird_unknown" && (rep.Reasons[i] != why) {
			t.Errorf("status's note: %q", rep.Reasons[i])
		}
	}
}

// covers: MA-1186
// An encrypted archive (O1): after a behind run, the run's result in memory gives the lead and
// both numbers for the screen (the Run now note, the wizard), while nothing about completeness is
// written beside the archive: no completeness key in the last-run record, none in the history,
// no streak record, and status says nothing of it. Positive twin first: the same store into a
// plaintext archive writes the record and status WARNs.
func TestAnEncryptedRunShowsItsWordsOnlyOnScreen(t *testing.T) {
	_, store := imapProfile(t, "imap.qzc3account.example", nil, tbFolder{path: "INBOX", flags: tbcomplete.FlagInbox, listed: 40, msgs: tbMsgs("in", 3)})
	plain := tmpDir(t)
	runTB(t, Options{Inputs: []string{store}, Out: plain})
	if rep := health.Assess(health.Gather(plain, ""), time.Now()); !strings.Contains(strings.Join(rep.Codes, " "), "thunderbird_behind") {
		t.Fatalf("positive twin: the plaintext archive's status: %q", rep.Codes)
	}

	out, key := encryptedOut(t)
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Key: key, Mode: export.Incremental}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	accts, more := TBAccounts(res)
	lines := state.TBLines(accts, more, state.TBSay{})
	if lead := tbcomplete.RunLead(res.Thunderbird).Sentence(); lead != "Backed up what Thunderbird has so far." || len(lines) != 1 || !strings.HasPrefix(lines[0], "Thunderbird has downloaded about 3 of 40 messages in imap.qzc3account.example so far.") {
		t.Errorf("the screen's words: %q %q", lead, lines)
	}
	data, err := os.ReadFile(filepath.Join(out, state.LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"thunderbird", "qzc3account", "behind", "on_disk"} {
		if strings.Contains(string(data), s) {
			t.Errorf("the encrypted last-run record holds %q", s)
		}
	}
	if _, err := os.Stat(filepath.Join(out, state.TBStreakName)); !os.IsNotExist(err) {
		t.Errorf("a streak record beside an encrypted archive (%v)", err)
	}
	rep := health.Assess(health.Gather(out, ""), time.Now())
	for _, c := range rep.Codes {
		if strings.HasPrefix(c, "thunderbird_") {
			t.Errorf("status says %s for an encrypted archive", c)
		}
	}
	if j := health.JSON(health.Gather(out, ""), rep); j.ThunderbirdCompleteness != nil || j.ThunderbirdLead != "" {
		t.Errorf("-json carries completeness for an encrypted archive")
	}
}
