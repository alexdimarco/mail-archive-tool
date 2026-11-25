package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1509, R12, R18
// The reused stop (docs/design-per-mailbox-archives.md revision 3; the adversarial pass's INS-03): an
// archive of one Thunderbird folder, auto-discovery off, whose record keeps the identity of the
// folder's account. The account is removed with its mail data (its folder deleted; that run stops as
// removed), then another account on the same server is added and Thunderbird gives it the free folder
// name. The next run stops before reading anything: the typed refusal in the design's words (the
// folder only, MA-1288), the record failed with source_reason reused, the source streak moved, the
// manifest untouched, nothing exported, neither account's name in the words or the log; and again on
// the next run (the refused record carried the identity). An encrypted archive's refused record never
// keeps the reason. Positive twins first, each running ok: the account still owns its folder; renamed
// (the same identity); a record with no identity (revision 2.2's rules: nothing is decided by
// identity); and an owner with no identity (no user name: nothing can be told).
func TestAFolderThunderbirdGaveToAnotherAccountStopsItsArchive(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example")
	a := f.stores["imap.qza.example"]
	name := goneAddress("imap.qza.example")
	prefs := filepath.Join(f.profile, "prefs.js")
	local := tbAcct{n: 2, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	mine := idAcctFor(1, "imap.qza.example", "ImapMail/imap.qza.example", name)
	tbWrite(t, prefs, tbPrefsJS(mine, local))
	out := oneFolderArchive(t, a, name)
	if recordedTBID(t, out, a) == "" {
		t.Fatal("the archive's record must keep its folder's account identity")
	}
	nameOnly := tmpDir(t)
	lr, _, _ := state.ReadLastRun(out)
	lr.ThunderbirdIDs = nil
	if err := state.WriteLastRun(nameOnly, lr); err != nil {
		t.Fatal(err)
	}

	// Positive twins.
	runsOK(t, "the account still owns its folder", out, false, a)
	renamed := mine
	renamed.extra = map[string]string{"name": `"qz-renamed@example.com"`, "userName": `"` + idUser("imap.qza.example") + `"`}
	tbWrite(t, prefs, tbPrefsJS(renamed, local))
	runsOK(t, "the same account renamed", out, false, a)

	// Removed with its mail data: the folder deleted, so that run stops as removed.
	tbWrite(t, prefs, tbPrefsJS(local))
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	if _, err, _ := goneRun(t, Options{Inputs: []string{a}, Out: out, Unattended: true}); err == nil || err.Error() != autoplace.RemovedText(a) {
		t.Fatalf("the account removed with its folder stops as removed: %v", err)
	}

	// Another account on the same server added: Thunderbird gives it the free folder name.
	const other = "qz-other@example.com"
	tbStoreAt(t, a, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("other-account", 1)})
	otherAcct := tbAcct{n: 3, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"` + other + `"`}}
	tbWrite(t, prefs, tbPrefsJS(otherAcct, local)) // with no user name: no identity, nothing told
	if _, _, err := DiscoverInputsFor([]string{a}, false, RecordedNamesAt(out)); err != nil {
		t.Errorf("positive twin: an owner with no identity tells nothing: %v", err)
	}
	otherAcct.extra["userName"] = `"` + other + `"`
	tbWrite(t, prefs, tbPrefsJS(otherAcct, local))
	if _, _, err := DiscoverInputsFor([]string{a}, false, RecordedNamesAt(nameOnly)); err != nil {
		t.Errorf("positive twin: a record with no identity keeps revision 2.2's rules: %v", err)
	}

	for i := 1; i <= 2; i++ {
		before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
		streak, _ := state.ReadSourceStreak(out)
		res, err, logged := goneRun(t, Options{Inputs: []string{a}, Out: out, Unattended: true})
		code, msg := 0, ""
		var sue *sourceUnreachableError
		if err != nil {
			code, msg = 1, err.Error()
			if !errors.As(err, &sue) {
				code = 3 // not the typed refusal: the streak would not move
			}
		}
		// Restated 2026-11-25 (the re-verify of rounds A and B, RF-02: these words reach the failure
		// email, where "its row" pointed nowhere and "that account" was unclear), beside the old third
		// needle, kept here: "To back up that account, click Make a new archive… and choose it; to stop
		// this archive's backups, click Pause (make read-only) on its row." It now reads as below, and
		// the message ends with it.
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names("Thunderbird now uses "+a+" for a different account than the one this archive was made for, so this backup stopped and nothing was added.",
				"The mail saved so far stays in this archive.",
				"To back up the account that uses it now, click Make a new archive… in MailArchive Desktop and choose it; to stop this archive's backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."),
			assure.Forbid(name, other),
			assure.NoSideEffect(func() bool {
				after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
				lr, _, _ := state.ReadLastRun(out)
				s, _ := state.ReadSourceStreak(out)
				return bytes.Equal(before, after) && res.Stats.Exported == 0 &&
					lr.Status == state.RunFailed && lr.Error == msg && lr.SourceReason == state.SourceReasonReused &&
					s.ConsecutiveFails == streak.ConsecutiveFails+1 && s.LastKind == "local-source-unreachable"
			}))
		if msg != autoplace.ReusedText(a) || !strings.HasSuffix(msg, "under Your archives in MailArchive Desktop.") || bytes.Contains(logged.Bytes(), []byte(name)) || bytes.Contains(logged.Bytes(), []byte(other)) {
			t.Errorf("run %d: the words %q, or the log names an account:\n%s", i, msg, logged)
		}
	}

	// Never beside an encrypted archive.
	enc := tmpDir(t)
	writeRefusedRun(Options{Out: enc, Key: &vault.Key{}}, &sourceUnreachableError{path: a, reused: true})
	if data, err := os.ReadFile(filepath.Join(enc, state.LastRunName)); err != nil || bytes.Contains(data, []byte("source_reason")) || bytes.Contains(data, []byte("imap.qza")) {
		t.Errorf("an encrypted archive's refused record gained the reason: %v\n%s", err, data)
	}
}
