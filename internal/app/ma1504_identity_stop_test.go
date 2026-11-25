package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
)

// Moved and removed decided by the account's identity (docs/design-per-mailbox-archives.md revision
// 3; the adversarial pass's INS-04, INS-05 and INS-06): an archive of one Thunderbird folder,
// auto-discovery off, whose record keeps its folder's account identity (thunderbird_ids) is judged by
// who that account is, not by the name it shows. Real temp profiles whose accounts have user names,
// read by real runs.

// covers: MA-1504, R12, R18
// INS-04 (the lead's call): an account with a name of its own ("QZ Work Mail") is removed in
// Thunderbird with its folder kept, a backup runs in the gap (ok: the record keeps the name and the
// identity), and the account is added again under Thunderbird's default name, kept at "-1". No live
// account has the recorded name, but one has the recorded identity, with its folder elsewhere: the
// run stops with C2's words, source_reason moved. Positive twins first: the account still owns its
// folder; the same record with no identity (revision 2.2's name rules) does not stop; nor does a
// record whose identity no live account has (the re-added account under another user name).
func TestAReAddedAccountUnderAnotherNameStopsByItsIdentity(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example")
	a := f.stores["imap.qza.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	local := tbAcct{n: 2, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	const shown = "QZ Work Mail"
	tbWrite(t, prefs, tbPrefsJS(idAcctFor(1, "imap.qza.example", "ImapMail/imap.qza.example", shown), local))
	out := oneFolderArchive(t, a, shown)
	if recordedTBID(t, out, a) == "" {
		t.Fatal("the archive's record must keep its folder's account identity")
	}
	runsOK(t, "the account still owns its folder", out, false, a)

	// Removed, folder kept; a backup in the gap keeps the name and the identity.
	tbWrite(t, prefs, tbPrefsJS(local))
	runsOK(t, "a backup in the gap", out, false, a)
	if lr, _, _ := state.ReadLastRun(out); autoplace.RecordedName(lr, a) != shown || recordedTBID(t, out, a) == "" {
		t.Fatalf("the gap backup keeps the name and the identity: %q %+v", autoplace.RecordedName(lr, a), lr.ThunderbirdIDs)
	}

	// Added again under its address, at "-1".
	newDir := filepath.Join(f.profile, "ImapMail", "imap.qza.example-1")
	tbStoreAt(t, newDir, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("imap.qza.example-1", 1)})
	again := idAcctFor(3, "imap.qza.example", "ImapMail/imap.qza.example-1", idUser("imap.qza.example"))

	// Positive twins.
	nameOnly := tmpDir(t)
	lr, _, _ := state.ReadLastRun(out)
	lr.ThunderbirdIDs = nil
	if err := state.WriteLastRun(nameOnly, lr); err != nil {
		t.Fatal(err)
	}
	someoneElse := again
	someoneElse.extra = map[string]string{"name": `"qz-else@example.com"`, "userName": `"qz-else@example.com"`}
	tbWrite(t, prefs, tbPrefsJS(someoneElse, local))
	if _, _, err := DiscoverInputsFor([]string{a}, false, RecordedNamesAt(out)); err != nil {
		t.Errorf("positive twin: a record whose identity no live account has must not stop: %v", err)
	}
	tbWrite(t, prefs, tbPrefsJS(again, local))
	if _, _, err := DiscoverInputsFor([]string{a}, false, RecordedNamesAt(nameOnly)); err != nil {
		t.Errorf("positive twin: a record with no identity keeps revision 2.2's name rules (no live account named %q): %v", shown, err)
	}

	// The stop, by the identity.
	refusesNoLongerUsed(t, out, a, newDir, shown)
	refusesNoLongerUsed(t, out, a, newDir, shown) // and again: the refused record carried the identity
}

// covers: MA-1504, R12, R18
// INS-06 (= CON-05): a carried name never makes another account a move. An account named "Work" is
// removed with its folder kept and backups go on (the record keeps its name and identity); a different
// account, also named "Work", is added on another server. The old archive's run is not stopped with
// the moved words: its record's identity is not that account's, and the run ends ok. Its twin: the
// same account (its identity) added again at "-1" stops it, with C2's words.
func TestADifferentAccountWithTheSameNameIsNotAMove(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example")
	a := f.stores["imap.qza.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	local := tbAcct{n: 2, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	work := idAcctFor(1, "imap.qza.example", "ImapMail/imap.qza.example", "Work")
	tbWrite(t, prefs, tbPrefsJS(work, local))
	out := oneFolderArchive(t, a, "Work")
	tbWrite(t, prefs, tbPrefsJS(local))
	runsOK(t, "a backup in the gap", out, false, a)

	// A different account named Work, on another server.
	c := filepath.Join(f.profile, "ImapMail", "imap.qzc.example")
	tbStoreAt(t, c, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("qzc", 1)})
	tbWrite(t, prefs, tbPrefsJS(idAcctFor(3, "imap.qzc.example", "ImapMail/imap.qzc.example", "Work"), local))
	runsOK(t, "INS-06: another account that merely shares the name", out, false, a)
	if lr, _, _ := state.ReadLastRun(out); lr.SourceReason != "" || lr.Status != state.RunOK {
		t.Errorf("INS-06: no stop recorded: %q %q", lr.Status, lr.SourceReason)
	}

	// The same account added again at "-1": the stop.
	newDir := filepath.Join(f.profile, "ImapMail", "imap.qza.example-1")
	tbStoreAt(t, newDir, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("imap.qza.example-1", 1)})
	again := work
	again.n, again.dirRel = 4, "ImapMail/imap.qza.example-1"
	tbWrite(t, prefs, tbPrefsJS(idAcctFor(3, "imap.qzc.example", "ImapMail/imap.qzc.example", "Work"), again, local))
	refusesNoLongerUsed(t, out, a, newDir, "Work")
}

// covers: MA-1506, R12, R18
// Removed by the identity (C13; INS-05): an archive of one folder whose folder is gone stops with the
// removed account's words only when no live account has the recorded identity. INS-05: the account
// renamed and its folder deleted by hand (its setting still names the folder, so Thunderbird makes it
// again) is not removed: today's typed words, no reason recorded. And an account removed for good is
// removed even though a different account now shows its old name (the name alone would have said it
// is still there). Positive twin first: the account still owns its folder, and the run ends ok.
func TestARenamedAccountIsNotRemovedAndARemovedOneIsKnownByItsIdentity(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example", "imap.qzb.example")
	b := f.stores["imap.qzb.example"]
	name := goneAddress("imap.qzb.example")
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := idAcctFor(1, "imap.qza.example", "ImapMail/imap.qza.example", goneAddress("imap.qza.example"))
	acctB := idAcctFor(2, "imap.qzb.example", "ImapMail/imap.qzb.example", name)
	local := tbAcct{n: 3, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	tbWrite(t, prefs, tbPrefsJS(acctA, acctB, local))
	out := oneFolderArchive(t, b, name)
	runsOK(t, "the account still owns its folder", out, false, b)

	// INS-05: renamed, then its folder deleted by hand.
	renamed := acctB
	renamed.extra = map[string]string{"name": `"qz-renamed@example.com"`, "userName": `"` + idUser("imap.qzb.example") + `"`}
	tbWrite(t, prefs, tbPrefsJS(acctA, renamed, local))
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	_, err, _ := goneRun(t, Options{Inputs: []string{b}, Out: out, Unattended: true})
	var sue *sourceUnreachableError
	if lr, _, _ := state.ReadLastRun(out); err == nil || !errors.As(err, &sue) || err.Error() != autoplace.GoneText(b) || lr.SourceReason != "" {
		t.Errorf("INS-05: a renamed account whose folder was deleted by hand keeps today's typed words and no reason: %v, reason %q", err, lr.SourceReason)
	}

	// Removed for good, while a different account now shows its old name.
	tbWrite(t, prefs, tbPrefsJS(acctA, idAcctFor(4, "imap.qzc.example", "ImapMail/imap.qzc.example", name), local))
	before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	streak, _ := state.ReadSourceStreak(out)
	_, err, logged := goneRun(t, Options{Inputs: []string{b}, Out: out, Unattended: true})
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3
		}
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(autoplace.RemovedText(b)),
		assure.Forbid(name, "check the path, or that the drive holding it is mounted"),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
			lr, _, _ := state.ReadLastRun(out)
			s, _ := state.ReadSourceStreak(out)
			return bytes.Equal(before, after) && lr.Status == state.RunFailed && lr.Error == msg &&
				lr.SourceReason == state.SourceReasonRemoved && s.ConsecutiveFails == streak.ConsecutiveFails+1
		}))
	if bytes.Contains(logged.Bytes(), []byte(name)) {
		t.Errorf("the run's log names the account (MA-1288):\n%s", logged)
	}
}
