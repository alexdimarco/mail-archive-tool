package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
)

// An archive of one Thunderbird folder, auto-discovery off, whose account Thunderbird no longer keeps
// there (docs/design-per-mailbox-archives.md §11 "When Thunderbird stops using an archive's folder",
// C2, with revision 2.1's F-01 and F-08; MA-1504): real temp profiles under a home's .thunderbird,
// read by real runs.

// oneFolderArchive makes an archive at a new folder of the single folder in, auto-discovery off, and
// checks that its record names the folder's account name (the condition the stop starts from).
func oneFolderArchive(t *testing.T, in, name string) string {
	t.Helper()
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{in}, Out: out}); err != nil {
		t.Fatalf("the archive of %s: %v", in, err)
	}
	if lr, _, _ := state.ReadLastRun(out); autoplace.RecordedName(lr, in) != name {
		t.Fatalf("the archive's record must name %s's account %q: %q", in, name, autoplace.RecordedName(lr, in))
	}
	return out
}

// runsOK asserts a scheduled run of the archive at out, listing inputs with auto-discovery as auto,
// ends ok: nothing stopped it.
func runsOK(t *testing.T, what string, out string, auto bool, inputs ...string) {
	t.Helper()
	if _, err, _ := goneRun(t, Options{Inputs: inputs, Auto: auto, Out: out, Unattended: true}); err != nil {
		t.Errorf("positive twin, %s: the run must not stop: %v", what, err)
	}
}

// refusesNoLongerUsed asserts a scheduled run of the archive at out of its one folder in refuses
// with the typed stop in C2's words (in, now kept in moved), naming no account (MA-1288), reading
// nothing, recorded failed in those words with source_reason moved, the source streak moved.
func refusesNoLongerUsed(t *testing.T, out, in, moved, account string) {
	t.Helper()
	before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	streak, _ := state.ReadSourceStreak(out)
	res, err, logged := goneRun(t, Options{Inputs: []string{in}, Out: out, Unattended: true})
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3 // not the typed refusal: the streak would not move
		}
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("Thunderbird no longer uses "+in+": the account that used it now keeps its mail in "+moved+", so this backup stopped.",
			"The mail saved so far stays in this archive.", "To back up "+moved+", click Make a new archive…",
			"choose One archive for each mailbox and the same folder as before", "click Pause (make read-only) on this archive",
			"set the account's Local Directory back to "+in+" (in Thunderbird: Account Settings, then Server Settings)."),
		assure.Forbid(account),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
			lr, _, _ := state.ReadLastRun(out)
			s, _ := state.ReadSourceStreak(out)
			return bytes.Equal(before, after) && res.Stats.Exported == 0 &&
				lr.Status == state.RunFailed && lr.Error == msg && lr.SourceReason == state.SourceReasonMoved &&
				s.ConsecutiveFails == streak.ConsecutiveFails+1 && s.LastKind == "local-source-unreachable"
		}))
	if msg != autoplace.NoLongerUsedText(in, moved) {
		t.Errorf("the stop's words:\n%s\nwant\n%s", msg, autoplace.NoLongerUsedText(in, moved))
	}
	if bytes.Contains(logged.Bytes(), []byte(account)) {
		t.Errorf("the run's log names the account (MA-1288):\n%s", logged)
	}
}

// covers: MA-1504, MA-1481, R12, R18
// C2: an archive of one Thunderbird folder, auto-discovery off, whose record names the folder's
// account, stops when that account now keeps its mail in another folder while the old folder is
// still there (removed in Thunderbird, folder kept, added again: Thunderbird makes "-1"). Three
// conditions, all needed: the record names the account; the settings say for certain that no live
// account uses the folder (thunderbird.NoLiveOwner, F-01); exactly one live account has the name,
// its folder elsewhere (autoplace.MovedHowAs, decided by the name). The stop is the typed refusal
// in C2's words (folders only), nothing is read, the record keeps source_reason moved, the streak
// moves, the next run stops again, and the Repair helper says no. Positive twins first, each
// running ok: the account still owns the folder; a folder the record never named (with an account
// the server-name rule alone would call moved); Local Folders moved; auto-discovery on (the new
// folder is found); two listed inputs; settings that cannot be read for certain; no live account
// with the name; another account on the same server that still names the folder.
func TestThunderbirdNoLongerUsingAFolderStopsItsArchive(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example")
	a := f.stores["imap.qza.example"]
	name := goneAddress("imap.qza.example")
	prefs := filepath.Join(f.profile, "prefs.js")
	acct := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"` + name + `"`}}
	local := tbAcct{n: 2, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	out := oneFolderArchive(t, a, name)
	never := oneFolderArchive(t, a, name) // then its record is made to name no account
	lr, _, _ := state.ReadLastRun(never)
	lr.Thunderbird, lr.ThunderbirdNames = nil, nil
	if err := state.WriteLastRun(never, lr); err != nil {
		t.Fatal(err)
	}
	lfOut := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{f.local}, Out: lfOut}); err != nil {
		t.Fatal(err)
	}
	autoOut := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a}, Auto: true, Out: autoOut}); err != nil {
		t.Fatal(err)
	}
	twoOut := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, f.local}, Out: twoOut}); err != nil {
		t.Fatal(err)
	}

	// Positive twin: the account still owns the folder.
	runsOK(t, "the account still owns its folder", out, false, a)

	// The account re-added: Thunderbird keeps it in imap.qza.example-1; the old folder stays.
	newDir := filepath.Join(f.profile, "ImapMail", "imap.qza.example-1")
	tbStoreAt(t, newDir, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("imap.qza.example-1", 1)})
	reAdded := acct
	reAdded.n, reAdded.dirRel = 3, "ImapMail/imap.qza.example-1"
	// Moved off the profile instead: the server-name rule alone would call that moved too.
	usb := filepath.Join(tmpDir(t), "QZUSB", "imap.qza.example")
	tbStoreAt(t, usb, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("usb", 1)})
	offProfile := acct
	offProfile.dirRel, offProfile.dir = "", usb

	// Positive twins with the account moved.
	tbWrite(t, prefs, tbPrefsJS(offProfile, local))
	runsOK(t, "a folder the record never named", never, false, a)
	lfAway := filepath.Join(tmpDir(t), "QZLocal")
	tbStoreAt(t, lfAway, tbFolder{path: "Inbox", msgs: tbMsgs("lf", 1)})
	movedLocal := local
	movedLocal.dirRel, movedLocal.dir = "", lfAway
	tbWrite(t, prefs, tbPrefsJS(acct, movedLocal))
	runsOK(t, "Local Folders moved (its record never names it)", lfOut, false, f.local)
	tbWrite(t, prefs, tbPrefsJS(reAdded, local))
	runsOK(t, "auto-discovery on", autoOut, true, a)
	if files, _, err := DiscoverInputsFor([]string{a}, true, RecordedNamesAt(autoOut)); err != nil || !slices.Contains(files, newDir) {
		t.Errorf("positive twin: with auto-discovery on the new folder is found: %v %v", files, err)
	}
	runsOK(t, "two listed inputs", twoOut, false, a, f.local)
	// These look through the run's own discovery with the archive's record (a whole run would write a
	// record that no longer names the account, which the stop below starts from).
	discoversOK := func(what string) {
		t.Helper()
		if _, _, err := DiscoverInputsFor([]string{a}, false, RecordedNamesAt(out)); err != nil {
			t.Errorf("positive twin, %s: discovery must not stop: %v", what, err)
		}
	}
	tbWrite(t, filepath.Join(f.profile, "user.js"), "/* qz privacy settings */\nuser_pref(\"qz.example.flag\", true);\n")
	discoversOK("settings that cannot be read for certain")
	if err := os.Remove(filepath.Join(f.profile, "user.js")); err != nil {
		t.Fatal(err)
	}
	renamed := reAdded
	renamed.extra = map[string]string{"name": `"qz-renamed@example.com"`}
	tbWrite(t, prefs, tbPrefsJS(renamed, local))
	discoversOK("no live account with the recorded name")
	other := tbAcct{n: 4, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"qz-other@example.com"`}}
	tbWrite(t, prefs, tbPrefsJS(reAdded, other, local))
	discoversOK("another account on the same server still names the folder")
	sameServerBare := tbAcct{n: 4, host: "imap.qza.example", typ: "imap", extra: map[string]string{"name": `"qz-other@example.com"`}}
	tbWrite(t, prefs, tbPrefsJS(reAdded, sameServerBare, local))
	discoversOK("another account on the same server with no folder setting")

	// The stop: the named account re-added at "-1", the old folder still there.
	tbWrite(t, prefs, tbPrefsJS(reAdded, local))
	refusesNoLongerUsed(t, out, a, newDir, name)
	// The next run stops again: the refused record carried the names.
	refusesNoLongerUsed(t, out, a, newDir, name)
	if health.GoneMailboxRepairable(out) {
		t.Error("the Repair helper says Repair for an archive Thunderbird no longer uses")
	}
	// Moved off the profile: the same stop, naming the folder it moved to.
	tbWrite(t, prefs, tbPrefsJS(offProfile, local))
	refusesNoLongerUsed(t, out, a, usb, name)
}

// covers: MA-1504, R12, R18
// F-01: two accounts on one mail server (the owner's own setup), the first removed in Thunderbird
// with its folder kept and added again, which Thunderbird keeps at "-2" since "-1" is the second
// account's. For the old folder ReadStoreFacts says ambiguous (both live accounts have its host), so
// only NoLiveOwner says no live account uses it, and the archive of that one folder stops. Positive
// twin first: before the re-add the folder is still the first account's, and the archive runs.
func TestTwoAccountsOnOneServerOneReAddedStopsItsArchive(t *testing.T) {
	f := goneFixture(t, false)
	host := "imap.qzg.example"
	old, second, readded := filepath.Join(f.profile, "ImapMail", "imap.qzg.example"), filepath.Join(f.profile, "ImapMail", "imap.qzg-1.example"), filepath.Join(f.profile, "ImapMail", "imap.qzg-2.example")
	tbStoreAt(t, old, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("qzg", 2)})
	tbStoreAt(t, second, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("qzg-1", 2)})
	first := tbAcct{n: 1, host: host, typ: "imap", dirRel: "ImapMail/imap.qzg.example", extra: map[string]string{"name": `"qza@qzg.example"`}}
	sec := tbAcct{n: 2, host: host, typ: "imap", dirRel: "ImapMail/imap.qzg-1.example", extra: map[string]string{"name": `"qzb@qzg.example"`}}
	prefs := filepath.Join(f.profile, "prefs.js")
	tbWrite(t, prefs, tbPrefsJS(first, sec))
	out := oneFolderArchive(t, old, "qza@qzg.example")
	runsOK(t, "the first account still owns its folder", out, false, old)

	tbStoreAt(t, readded, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("qzg-2", 1)})
	again := first
	again.n, again.dirRel = 3, "ImapMail/imap.qzg-2.example"
	tbWrite(t, prefs, tbPrefsJS(sec, again))
	// The case F-01 is about: ReadStoreFacts cannot say "none" for the old folder, NoLiveOwner can.
	_, err := thunderbird.ReadStoreFacts(f.profile, old)
	if k, ok := thunderbird.NoAccountKindOf(err); !ok || k != thunderbird.NoAccountAmbiguous || !autoplace.NoLiveOwner(old) {
		t.Fatalf("F-01's case: ReadStoreFacts must say ambiguous (%v %v, %v) and NoLiveOwner no live owner (%v)", k, ok, err, autoplace.NoLiveOwner(old))
	}
	refusesNoLongerUsed(t, out, old, readded, "qza@qzg.example")
}

// covers: MA-1504, MA-1481, R12, R18
// F-08: an archive of one folder, auto-discovery off, whose folder is gone and whose record names
// its account, which now keeps its mail elsewhere, stops with C2's moved words (the 1.7.5 rule ran
// only with auto-discovery on), recorded with source_reason moved. An auto-discovery run's 1.7.5 moved
// stop records moved too, both when the record named the account and when the server-name rule found
// the move. Positive twins first: a gone folder whose record names no account keeps today's words,
// with no source_reason; and one whose named account's folder setting still names it (deleted by
// hand, Q4) too.
func TestAGoneFolderWhoseAccountMovedStopsWithTheMovedWords(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	name := goneAddress("imap.qzb.example")
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"` + goneAddress("imap.qza.example") + `"`}}
	acctB := tbAcct{n: 2, host: "imap.qzb.example", typ: "imap", dirRel: "ImapMail/imap.qzb.example", extra: map[string]string{"name": `"` + name + `"`}}
	local := tbAcct{n: 3, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	out := oneFolderArchive(t, b, name)
	autoOut := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: autoOut}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	refusedWith := func(o string, opts Options) (string, state.LastRun) {
		t.Helper()
		opts.Out, opts.Unattended = o, true
		_, err, _ := goneRun(t, opts)
		var sue *sourceUnreachableError
		if !errors.As(err, &sue) {
			t.Fatalf("the run must refuse with the typed stop: %v", err)
		}
		lr, _, _ := state.ReadLastRun(o)
		return err.Error(), lr
	}

	// Positive twins: today's words, and no reason recorded.
	nameless := tmpDir(t)
	if err := state.WriteLastRun(nameless, state.LastRun{Status: state.RunOK, Job: []string{"-out", nameless, "-input", b}}); err != nil {
		t.Fatal(err)
	}
	if msg, lr := refusedWith(nameless, Options{Inputs: []string{b}}); msg != autoplace.GoneText(b) || lr.SourceReason != "" {
		t.Errorf("positive twin: a gone folder whose record names no account keeps today's words, no reason: %q %q", msg, lr.SourceReason)
	}
	if msg, lr := refusedWith(out, Options{Inputs: []string{b}}); msg != autoplace.GoneText(b) || lr.SourceReason != "" {
		t.Errorf("positive twin: deleted by hand (its setting still names it) keeps today's words: %q %q", msg, lr.SourceReason)
	}

	// The account moved off the profile: the moved stop, now also with auto-discovery off.
	usb := filepath.Join(tmpDir(t), "QZUSB", "imap.qzb.example")
	tbStoreAt(t, usb, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("usb", 1)})
	moved := acctB
	moved.dirRel, moved.dir = "", usb
	tbWrite(t, prefs, tbPrefsJS(acctA, moved, local))
	streak, _ := state.ReadSourceStreak(out)
	msg, lr := refusedWith(out, Options{Inputs: []string{b}})
	assure.Refused(t, 1, msg, assure.Code(1), assure.Names(autoplace.NoLongerUsedText(b, usb)), assure.Forbid(name),
		assure.NoSideEffect(func() bool {
			s, _ := state.ReadSourceStreak(out)
			return lr.Status == state.RunFailed && lr.Error == msg && lr.SourceReason == state.SourceReasonMoved &&
				s.ConsecutiveFails == streak.ConsecutiveFails+1
		}))
	// MA-1481, amended: the auto-discovery run's moved stop records moved too, by name and by server.
	if msg, lr := refusedWith(autoOut, Options{Inputs: []string{a, b}, Auto: true}); msg != autoplace.MovedText(b, usb) || lr.SourceReason != state.SourceReasonMoved {
		t.Errorf("an auto-discovery run's moved stop: %q, source_reason %q", msg, lr.SourceReason)
	}
	sameServer := tmpDir(t)
	if err := state.WriteLastRun(sameServer, state.LastRun{Status: state.RunOK, Job: []string{"-out", sameServer, "-auto", "-input", a, "-input", b}}); err != nil {
		t.Fatal(err)
	}
	if msg, lr := refusedWith(sameServer, Options{Inputs: []string{a, b}, Auto: true}); msg != autoplace.MovedSameServerText(b, usb) || lr.SourceReason != state.SourceReasonMoved {
		t.Errorf("an auto-discovery run's moved stop found by the server name: %q, source_reason %q", msg, lr.SourceReason)
	}
}
