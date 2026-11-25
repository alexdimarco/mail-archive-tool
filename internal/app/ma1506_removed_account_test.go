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
	"mail-archive-tool/internal/vault"
)

// covers: MA-1506, R12, R18
// C13 (docs/design-per-mailbox-archives.md §11 "When the account was removed for good"): an archive
// of one Thunderbird folder, auto-discovery off, whose folder is gone, whose previous record names
// its account, and whose profile's settings (read for certain) name no live account with that name,
// stops as before, typed, so the source streak moves and the 30-day rule is unchanged, in its own
// words in place of "check the path, or that the drive holding it is mounted": "Thunderbird no
// longer has the account that used FOLDER. This archive keeps the mail saved so far. To stop its
// backups, click Pause (make read-only) on its row." Those words are the run's error, so they are
// what its record, status, Run now's note and the failure email say; the record keeps
// source_reason removed. They name the folder only (MA-1288). Positive twins first, each keeping
// today's words and recording no reason: a gone folder whose record names no account; one whose
// named account is still live (its folder deleted by hand, Q4); settings that cannot be read for
// certain.
func TestAnAccountRemovedForGoodStopsInItsOwnWords(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example", "imap.qzb.example")
	b := f.stores["imap.qzb.example"]
	name := goneAddress("imap.qzb.example")
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"` + goneAddress("imap.qza.example") + `"`}}
	local := tbAcct{n: 3, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}
	out := oneFolderArchive(t, b, name)
	nameless := oneFolderArchive(t, b, name)
	lr, _, _ := state.ReadLastRun(nameless)
	lr.Thunderbird, lr.ThunderbirdNames = nil, nil
	if err := state.WriteLastRun(nameless, lr); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	run := func(o string) (string, bool, state.LastRun, *bytes.Buffer) {
		t.Helper()
		_, err, logged := goneRun(t, Options{Inputs: []string{b}, Out: o, Unattended: true})
		var sue *sourceUnreachableError
		lr, _, _ := state.ReadLastRun(o)
		if err == nil {
			return "", false, lr, logged
		}
		return err.Error(), errors.As(err, &sue), lr, logged
	}
	todays := func(what, o string) {
		t.Helper()
		if msg, typed, lr, _ := run(o); msg != autoplace.GoneText(b) || !typed || lr.SourceReason != "" {
			t.Errorf("positive twin, %s: today's typed words and no reason: %q typed=%v reason=%q", what, msg, typed, lr.SourceReason)
		}
	}

	// Positive twins.
	tbWrite(t, prefs, tbPrefsJS(acctA, local)) // the account removed in Thunderbird
	todays("a gone folder whose record names no account", nameless)
	tbWrite(t, filepath.Join(f.profile, "user.js"), "/* qz privacy settings */\nuser_pref(\"qz.example.flag\", true);\n")
	todays("settings that cannot be read for certain", out)
	if err := os.Remove(filepath.Join(f.profile, "user.js")); err != nil {
		t.Fatal(err)
	}
	acctB := tbAcct{n: 2, host: "imap.qzb.example", typ: "imap", dirRel: "ImapMail/imap.qzb.example", extra: map[string]string{"name": `"` + name + `"`}}
	tbWrite(t, prefs, tbPrefsJS(acctA, acctB, local))
	todays("its account still in Thunderbird, the folder deleted by hand", out)

	// The account removed for good.
	tbWrite(t, prefs, tbPrefsJS(acctA, local))
	for i := 0; i < 2; i++ { // and again on the next run: the refused record carried the names
		before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
		streak, _ := state.ReadSourceStreak(out)
		msg, typed, lr, logged := run(out)
		code := 0
		if msg != "" {
			code = 1
			if !typed {
				code = 3 // not the typed refusal: the streak would not move
			}
		}
		assure.Refused(t, code, msg, assure.Code(1),
			// Amended 2026-11-25 (the friction review's F12; nothing weakened): the words reach the
			// failure email, so they say where the row is; they ended "… on its row." before.
			assure.Names("Thunderbird no longer has the account that used "+b+".", "This archive keeps the mail saved so far.",
				"To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."),
			assure.Forbid(name, "check the path, or that the drive holding it is mounted"),
			assure.NoSideEffect(func() bool {
				after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
				s, _ := state.ReadSourceStreak(out)
				return bytes.Equal(before, after) && lr.Status == state.RunFailed && lr.Error == msg &&
					lr.SourceReason == state.SourceReasonRemoved &&
					s.ConsecutiveFails == streak.ConsecutiveFails+1 && s.LastKind == "local-source-unreachable"
			}))
		if msg != autoplace.RemovedText(b) || bytes.Contains(logged.Bytes(), []byte(name)) {
			t.Errorf("run %d: the words %q, or the log names the account:\n%s", i+1, msg, logged)
		}
	}
}

// covers: MA-1506, MA-734, R37
// The reason code goes in a plaintext refused record only: an encrypted archive's refused record
// keeps its allow-list and gains no field (MA-734), whatever stopped it. Positive twin first: the
// same refusal into a plaintext archive's record keeps source_reason.
func TestAnEncryptedRecordGainsNoReason(t *testing.T) {
	for _, cause := range []*sourceUnreachableError{{path: "/qz/ImapMail/imap.qzb.example", removed: true}, {path: "/qz/ImapMail/imap.qzb.example", movedTo: "/qz/away", one: true}} {
		plain := tmpDir(t)
		writeRefusedRun(Options{Out: plain}, cause)
		if data, _ := os.ReadFile(filepath.Join(plain, state.LastRunName)); !bytes.Contains(data, []byte(`"source_reason": "`+cause.sourceReason()+`"`)) || cause.sourceReason() == "" {
			t.Fatalf("positive twin: a plaintext refused record keeps the reason %q:\n%s", cause.sourceReason(), data)
		}
		enc := tmpDir(t)
		writeRefusedRun(Options{Out: enc, Key: &vault.Key{}}, cause)
		data, err := os.ReadFile(filepath.Join(enc, state.LastRunName))
		if err != nil || bytes.Contains(data, []byte("source_reason")) || bytes.Contains(data, []byte("imap.qzb")) {
			t.Errorf("an encrypted archive's refused record gained a field: %v\n%s", err, data)
		}
	}
}
