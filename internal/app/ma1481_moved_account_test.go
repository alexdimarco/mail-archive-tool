package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// movedWords are the words a moved account's refusal must hold: the old folder, the new one, that
// Thunderbird still lists the account, and both ways to back its mail up again.
func movedWords(old, moved string) []string {
	// Amended 2026-11-20 by the 1.7.5 re-verify (RF-03), at the same strength: "Thunderbird still has
	// this account" for "Thunderbird still lists its account", and where the setting is, and how to stop
	// this archive looking for the old folder.
	// Amended again 2026-11-21 by the re-verify, round 2 (RF2-01), at the same strength: the account
	// named in the record says "Thunderbird still has the account that used it", and where Local
	// Folders keeps its setting.
	return []string{"input " + old + " does not exist, so this backup stopped", "Thunderbird still has the account that used it", moved,
		"Local Directory", "(in Thunderbird: Account Settings, then Server Settings, or Local Folders' own page)", "Make a new archive…",
		"Thunderbird / mbox mail folder", "stop this archive looking for " + old}
}

// stoppedOn174 plants what 1.7.4 left for an "All of them" archive at out that it stopped on the
// gone input b: a failed -auto record refused on b in the gone words, and a loud source streak.
func stoppedOn174(t *testing.T, out string, inputs []string, b string) {
	t.Helper()
	t0 := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(state.SurfaceAfter), LastKind: "local-source-unreachable"}); err != nil {
		t.Fatal(err)
	}
	job := []string{"-out", out, "-mode", "incremental", "-auto"}
	for _, in := range inputs {
		job = append(job, "-input", in)
	}
	at := t0.Add(state.SurfaceAfter)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Started: at, Finished: at, Error: autoplace.GoneText(b), Job: job}); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1481, MA-1471, MA-1479, R12, R18
// A Thunderbird account moved with its Local Directory setting (not removed) stops the backup, as
// 1.7.4 did (the 1.7.5 adversarial pass's INS-02; the owner, 2026-11-20, Q7: "Stop the backup
// (Recommended)"): its old folder is gone, but Thunderbird's settings still give it to a live
// account (by the folder's hostname) whose folder setting names another place, so the run refuses
// with the typed sourceUnreachableError, in words that say the account was moved, where to, and how
// to back that mail up; nothing is read, the run is recorded failed in those words, and the source
// streak moves. The Repair helper agrees: it says no for an archive 1.7.4 stopped on that folder.
// Positive twins first (Q4): the account removed from the list is skipped; an account whose folder
// setting still names the folder, deleted by hand, is skipped; and the helper says yes for the
// removed account.
func TestAnAccountMovedInThunderbirdStopsTheBackup(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"` + goneAddress("imap.qza.example") + `"`}}
	acctB := tbAcct{n: 2, host: "imap.qzb.example", typ: "imap", dirRel: "ImapMail/imap.qzb.example", extra: map[string]string{"name": `"` + goneAddress("imap.qzb.example") + `"`}}
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatalf("the archive: %v", err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}

	// Positive twin 1: the folder deleted by hand, its account's folder setting unchanged (Q4).
	if _, skipped, err := DiscoverInputs([]string{a, b}, true); err != nil || len(skipped) != 1 || skipped[0] != b {
		t.Fatalf("positive twin: an account whose folder setting is unchanged, its folder deleted by hand, is skipped: %v %v", skipped, err)
	}
	// Positive twin 3: the account listed with no folder setting at all: nothing says it moved.
	bare := acctB
	bare.dirRel = ""
	tbWrite(t, prefs, tbPrefsJS(acctA, bare))
	if _, skipped, err := DiscoverInputs([]string{a, b}, true); err != nil || len(skipped) != 1 || skipped[0] != b {
		t.Fatalf("positive twin: an account with no folder setting is not called moved: %v %v", skipped, err)
	}
	// Positive twin 2: the account removed from Thunderbird's list.
	tbWrite(t, prefs, tbPrefsJS(acctA))
	if _, skipped, err := DiscoverInputs([]string{a, b}, true); err != nil || len(skipped) != 1 || skipped[0] != b {
		t.Fatalf("positive twin: an account removed from Thunderbird is skipped: %v %v", skipped, err)
	}
	stopped := tmpDir(t)
	stoppedOn174(t, stopped, []string{a, b}, b)
	if !health.GoneMailboxRepairable(stopped) {
		t.Fatal("positive twin: the helper says Repair works for a removed account")
	}

	// The account moved: Thunderbird still lists it, its Local Directory now on a USB drive.
	usb := filepath.Join(tmpDir(t), "QZUSB", "imap.qzb.example")
	tbStoreAt(t, usb, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzb.example", 2)})
	moved := acctB
	moved.dirRel, moved.dir = "", usb
	tbWrite(t, prefs, tbPrefsJS(acctA, moved))

	if autoplace.GoneSkippable(b) {
		t.Error("GoneSkippable says a moved account's old folder may be skipped")
	}
	if health.GoneMailboxRepairable(stopped) {
		t.Error("the helper says Repair works for a moved account, where the run refuses")
	}
	before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	streak, _ := state.ReadSourceStreak(out)
	res, err, logged := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true})
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3 // not the typed refusal: the streak would not move
		}
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(movedWords(b, usb)...),
		assure.Forbid(goneAddress("imap.qzb.example")), // MA-1288: the refusal reaches log files
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
			lr, _, _ := state.ReadLastRun(out)
			s, _ := state.ReadSourceStreak(out)
			return bytes.Equal(before, after) && res.Stats.Exported == 0 && len(res.Skipped) == 0 &&
				lr.Status == state.RunFailed && lr.Error == msg &&
				s.ConsecutiveFails == streak.ConsecutiveFails+1 && s.LastKind == "local-source-unreachable"
		}))
	if strings.Contains(logged.String(), goneAddress("imap.qzb.example")) {
		t.Errorf("the run's log names the moved account (MA-1288):\n%s", logged)
	}
}

// refusesAsMoved asserts that DiscoverInputs refuses the gone input in with the typed error, in the
// moved account's words naming moved, and that GoneSkippable says no.
func refusesAsMoved(t *testing.T, inputs []string, in, moved string) {
	t.Helper()
	if autoplace.GoneSkippable(in) {
		t.Errorf("GoneSkippable(%s) is true for an account moved to %s", in, moved)
	}
	files, skipped, err := DiscoverInputs(inputs, true)
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3
		}
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("input "+in+" does not exist", moved),
		assure.NoSideEffect(func() bool { return len(files) == 0 && len(skipped) == 0 }))
}

// covers: MA-1481, MA-1471, R12, R18
// Q7's fuller form (the 1.7.5 re-verify's RV-02; the lead's call, in the direction of the owner's
// "Stop the backup (Recommended)"): an account counts as moved when exactly one live account has the
// gone folder's server name (its name, or its name without a "-N" suffix) and none of that account's
// folder settings lies inside this profile's ImapMail/ or Mail/. So a move to a folder of another
// name (RV-02a, written as Thunderbird writes it: directory and a directory-rel with "..") and a POP
// account's move under Mail/ (RV-02b) stop the backup. The traded case: a removed account whose only
// same-server sibling was moved off the profile stops it too (fail closed, Q7's direction). Positive
// twin first: an account removed beside a same-server account still in the profile is skipped.
func TestAMoveUnderAnotherNameOrUnderMailStopsTheBackup(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example"}
	acctB := tbAcct{n: 2, host: "imap.qzb.example", typ: "imap", dirRel: "ImapMail/imap.qzb.example"}
	local := tbAcct{n: 3, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	usb := tmpDir(t)

	// Positive twin: imap.qzd.example removed beside imap.qzd.example-1 (same server) in the profile.
	d0, d1 := filepath.Join(f.profile, "ImapMail", "imap.qzd.example"), filepath.Join(f.profile, "ImapMail", "imap.qzd.example-1")
	tbStoreAt(t, d1, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzd.example", 2)})
	sibling := tbAcct{n: 5, host: "imap.qzd.example", typ: "imap", dirRel: "ImapMail/imap.qzd.example-1"}
	tbWrite(t, prefs, tbPrefsJS(acctA, acctB, local, sibling))
	if _, skipped, err := DiscoverInputs([]string{a, d0, f.local}, true); err != nil || len(skipped) != 1 || !autoplace.GoneSkippable(d0) {
		t.Fatalf("positive twin: a removed account beside a same-server account in the profile is skipped: %v %v", skipped, err)
	}
	// The traded case: that sibling moved off the profile. Fails closed.
	away := filepath.Join(usb, "QZUSB", "imap.qzd.example-1")
	tbStoreAt(t, away, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzd.example", 2)})
	s2 := sibling
	s2.dirRel, s2.dir = "", away
	tbWrite(t, prefs, tbPrefsJS(acctA, acctB, local, s2))
	refusesAsMoved(t, []string{a, d0, f.local}, d0, away)

	// RV-02a: moved to a folder of another name.
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(usb, "QZUSB", "QZWork")
	tbStoreAt(t, other, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzb.example", 2)})
	rel, err := filepath.Rel(f.profile, other)
	if err != nil {
		t.Fatal(err)
	}
	m := acctB
	m.dirRel, m.dir = filepath.ToSlash(rel), other
	tbWrite(t, prefs, tbPrefsJS(acctA, m, local))
	refusesAsMoved(t, []string{a, b, f.local}, b, other)

	// RV-02b: a POP account under Mail/, moved.
	pop := filepath.Join(f.profile, "Mail", "pop.qzc.example")
	popMoved := filepath.Join(usb, "QZUSB2", "pop.qzc.example")
	tbStoreAt(t, popMoved, tbFolder{path: "Inbox", msgs: tbMsgs("pop", 2)})
	tbStoreAt(t, b, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzb.example", 2)})
	c := tbAcct{n: 4, host: "pop.qzc.example", typ: "pop3", dir: popMoved}
	tbWrite(t, prefs, tbPrefsJS(acctA, acctB, local, c))
	refusesAsMoved(t, []string{a, b, pop, f.local}, pop, popMoved)
}

// covers: MA-1481, MA-1479, R12, R18
// The owner's own setup (the 1.7.5 re-verify, round 2's RV2-01; the lead's call): three accounts on
// one mail server, their folders spelled as Thunderbird spells them (imap.qzg.example,
// imap.qzg-1.example, imap.qzg-2.example). The previous record names each folder's account, and that
// decides first: the second account removed in Thunderbird is skipped; the same account, still in
// Thunderbird but moved to another folder, stops the backup (Q7), named from the record (here carried
// by the skip's warning). The Repair helper agrees: it says Repair for the removed account, not for
// the moved one. Positive twin first: the removed account is skipped.
func TestTheRecordedNameDecidesAMoveAmongAccountsOnOneServer(t *testing.T) {
	f := goneFixture(t, false)
	host := "imap.qzg.example"
	folders := []string{"imap.qzg.example", "imap.qzg-1.example", "imap.qzg-2.example"}
	var accts []tbAcct
	var inputs []string
	for i, fd := range folders {
		dir := filepath.Join(f.profile, "ImapMail", fd)
		tbStoreAt(t, dir, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs(fd, 2)})
		accts = append(accts, tbAcct{n: i + 1, host: host, typ: "imap", dirRel: "ImapMail/" + fd, extra: map[string]string{"name": `"qz` + string(rune('a'+i)) + `@qzg.example"`}})
		inputs = append(inputs, dir)
	}
	prefs := filepath.Join(f.profile, "prefs.js")
	tbWrite(t, prefs, tbPrefsJS(accts...))
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out}); err != nil {
		t.Fatal(err)
	}
	b := inputs[1]
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	// Amended 2026-11-21 by the re-verify, round 3 (RV3-02; the lead's call), at the same strength,
	// beside its text: a removed account matches no live account by the record's name, so the
	// server-name rule now decides it. The positive twin is the removed account with the third
	// account in the profile: skipped. (It read: "The third account keeps its mail off the profile
	// …, so the server-name rule alone would decide both cases below the wrong way: it would stop for
	// the removed account … and skip the moved one …", with the removed account skipped beside the
	// third off the profile. That is now the traded case, asserted below as a stop.)
	// Positive twin: the second account removed in Thunderbird: skipped, named from the record.
	tbWrite(t, prefs, tbPrefsJS(accts[0], accts[2]))
	res, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out, Unattended: true})
	if err != nil || len(res.Skipped) != 1 || res.Skipped[0].Name != "qzb@qzg.example" {
		t.Fatalf("positive twin: a removed account among three on one server is skipped: %+v %v", res.Skipped, err)
	}
	stopped := tmpDir(t)
	plantStopped := func() {
		stoppedOn174(t, stopped, inputs, b)
		lr, _, _ := state.ReadLastRun(stopped)
		lr.Warnings = []state.RunWarning{{Code: state.MailboxGoneCode, Message: "x", Path: b, Name: "qzb@qzg.example", Since: lr.Started}}
		if err := state.WriteLastRun(stopped, lr); err != nil {
			t.Fatal(err)
		}
	}
	plantStopped()
	if !health.GoneMailboxRepairable(stopped) {
		t.Fatal("positive twin: the Repair helper says Repair for the removed account")
	}
	// The third account keeps its mail off the profile (moved long ago).
	away3 := filepath.Join(tmpDir(t), "QZUSB", "QZGmail3")
	tbStoreAt(t, away3, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzg-2.example", 2)})
	c3 := accts[2]
	c3.dirRel, c3.dir = "", away3
	// The traded case RV3-02 brings back (MA-1481): the removed account beside exactly one
	// same-server account whose mail is all off the profile stops the backup, in the words of a move
	// the server-name rule found; the Repair helper agrees.
	tbWrite(t, prefs, tbPrefsJS(accts[0], c3))
	if health.GoneMailboxRepairable(stopped) {
		t.Error("the Repair helper says Repair in the traded case, where the run stops")
	}
	if _, _, terr := DiscoverInputsFor(inputs, true, RecordedNamesAt(out)); terr == nil || !strings.Contains(terr.Error(), "a Thunderbird account on the same mail server") {
		t.Errorf("the traded case: a removed account beside one same-server account off the profile stops: %v", terr)
	}
	// The same account still in Thunderbird, moved to another folder. With the third account off the
	// profile too, the server-name rule alone would skip it (two same-server accounts outside): the
	// recorded name decides.
	away := filepath.Join(tmpDir(t), "QZUSB", "QZGmail2")
	tbStoreAt(t, away, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzg-1.example", 2)})
	m := accts[1]
	m.dirRel, m.dir = "", away
	tbWrite(t, prefs, tbPrefsJS(accts[0], m, c3))
	if health.GoneMailboxRepairable(stopped) {
		t.Error("the Repair helper says Repair for a moved account, where the run stops")
	}
	if dir, moved := autoplace.MovedToAs(b, "qzb@qzg.example"); !moved || dir != away {
		t.Errorf("named in the record, the moved account is found: %q %v", dir, moved)
	}
	before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	res, err, _ = goneRun(t, Options{Inputs: inputs, Auto: true, Out: out, Unattended: true})
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3
		}
	}
	// The account's name stays out of the refusal, which the run's log file holds (MA-1288).
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("input "+b+" does not exist", away, "Thunderbird still has the account that used it"),
		assure.Forbid("qzb@qzg.example"),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
			return bytes.Equal(before, after) && res.Stats.Exported == 0
		}))
}

// covers: MA-1481, R12
// The server-name fallback, for a record that names no account (the re-verify, round 2's RV2-01 (a)
// and (b), RV2-03): a "-N" at the folder name's end ("imap.qzd.example-1"), just before its last dot
// ("imap.qzd-1.example", Thunderbird's own spelling) or at the end of a name with no dot ("qzfeeds-1")
// is the same server; and among the live accounts with that server name, the one moved is found when
// exactly one has every folder setting outside the profile, even beside another kept in it. Positive
// twin first: a removed account beside a same-server account kept in the profile is skipped.
func TestTheServerNameFallbackFindsAMoveBesideASameServerAccount(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example")
	a := f.stores["imap.qza.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example"}
	local := tbAcct{n: 3, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	x := filepath.Join(f.profile, "ImapMail", "imap.qzd.example")
	tbStoreAt(t, x, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzd.example", 2)})
	xa := tbAcct{n: 5, host: "imap.qzd.example", typ: "imap", dirRel: "ImapMail/imap.qzd.example"}
	d1 := filepath.Join(f.profile, "ImapMail", "imap.qzd.example-1")
	tbWrite(t, prefs, tbPrefsJS(acctA, local, xa))
	if _, skipped, err := DiscoverInputs([]string{a, x, d1, f.local}, true); err != nil || len(skipped) != 1 {
		t.Fatalf("positive twin: removed beside a same-server account in the profile is skipped: %v %v", skipped, err)
	}
	away := filepath.Join(tmpDir(t), "QZUSB", "QZWork3")
	tbStoreAt(t, away, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzd.example", 2)})
	y := tbAcct{n: 6, host: "imap.qzd.example", typ: "imap", dir: away}
	tbWrite(t, prefs, tbPrefsJS(acctA, local, xa, y))
	refusesAsMoved(t, []string{a, x, d1, f.local}, d1, away) // (b), the "-N"-at-the-end spelling
	tb := filepath.Join(f.profile, "ImapMail", "imap.qzd-1.example")
	refusesAsMoved(t, []string{a, x, tb, f.local}, tb, away) // (a), Thunderbird's spelling
	feeds := filepath.Join(f.profile, "Mail", "qzfeeds-1")
	feedsAway := filepath.Join(tmpDir(t), "QZUSB", "QZFeeds")
	tbStoreAt(t, feedsAway, tbFolder{path: "Inbox", msgs: tbMsgs("feeds", 2)})
	fa := tbAcct{n: 7, host: "qzfeeds", typ: "rss", dir: feedsAway}
	tbWrite(t, prefs, tbPrefsJS(acctA, local, fa))
	refusesAsMoved(t, []string{a, feeds, f.local}, feeds, feedsAway) // a name with no dot
}

// covers: MA-1481, MA-1472, R18
// Settings MailArchive cannot read for certain tell no move (the builder's choice, weighed right by
// the re-verify: stopping would stop every removed account in such profiles, against Q4), and now say
// so (RV-04): the run records it on the gone mailbox's warning, and status adds "If you moved this
// account to another folder in Thunderbird, MailArchive could not tell from its settings." Positive
// twin first: with plain settings the move stops the backup, and a plain skip adds no such sentence.
func TestSettingsThatCannotBeReadForCertainSaySo(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example"}
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	// Amended 2026-11-21 (RF2-03), word for word: what to do follows.
	const unsure = "If you moved this account to another folder in Thunderbird, MailArchive could not tell from Thunderbird's settings, so that folder is not backed up: back it up as an archive of its own (Make a new archive…, then Thunderbird / mbox mail folder)."
	status := func() string { return strings.Join(health.Assess(health.Gather(out, ""), time.Now()).Reasons, "\n") }
	tbWrite(t, prefs, tbPrefsJS(acctA)) // removed on purpose, plain settings
	if res, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true}); err != nil || len(res.Skipped) != 1 || res.Skipped[0].SettingsUnsure || strings.Contains(status(), unsure) {
		t.Fatalf("positive twin: a plain skip says nothing of unreadable settings: %+v %v", res.Skipped, err)
	}
	tbWrite(t, filepath.Join(f.profile, "user.js"), "/* qz privacy settings */\nuser_pref(\"qz.example.flag\", true);\n")
	res, err, logged := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true})
	if err != nil || len(res.Skipped) != 1 || !res.Skipped[0].SettingsUnsure {
		t.Fatalf("settings not read for certain: still skipped (Q4), and marked: %+v %v", res.Skipped, err)
	}
	if got := status(); !strings.Contains(got, unsure) {
		t.Errorf("status adds the sentence:\n%s", got)
	}
	if strings.Contains(logged.String(), "could not tell") {
		t.Errorf("the run's log line stays the path alone:\n%s", logged)
	}
}
