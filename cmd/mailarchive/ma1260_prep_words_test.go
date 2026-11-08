package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1260, R12
// Every -enable-offline refusal says that nothing was changed and nothing was exported, why (the
// technical detail may stay here, after the plain sentence), what to make sure of in Thunderbird,
// and to run again without -enable-offline (the C0c friction review's W5, F1, F13): an account
// removed from Thunderbird whose folder was left, a hand-edited line MailArchive will not
// rewrite, and Thunderbird running. The run exits 1 and the profile keeps every byte. Positive
// twin first: the healthy profile is turned on with exit 0.
func TestEveryPrepRefusalGivesTheWayOut(t *testing.T) {
	_, store := offlineProfile(t, setOffOwnFolder)
	asStore(t, store)
	if code, msg, _ := enableOfflineRun(t, store); code != 0 {
		t.Fatalf("the positive twin: exit %d, %s", code, msg)
	}

	t.Run("a removed account's folder", func(t *testing.T) {
		profile, _ := offlineProfile(t, setOffOwnFolder)
		left := filepath.Join(profile, "ImapMail", "mail.oldjob.com")
		if err := os.MkdirAll(left, 0o755); err != nil {
			t.Fatal(err)
		}
		asStore(t, left)
		before := snapshot(t, profile)
		code, msg, _ := enableOfflineRun(t, left)
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names("-enable-offline changed nothing, and nothing was exported: MailArchive could not find a Thunderbird account for the mail in mail.oldjob.com",
				"no account in prefs.js matches the store folder mail.oldjob.com",
				// The lead's split of the C0c friction re-look's R2 (2026-11-08): never "ignore this
				// message" after "nothing was exported"; the way on is to run again.
				"If you once removed an account on mail.oldjob.com from Thunderbird, this may be its leftover mail folder; if so, there is nothing to turn on: just run the same command again without -enable-offline.",
				// The re-look's R1, its command-line form: where Account Settings is.
				"If the account is still in Thunderbird, open Account Settings (the menu button ≡, top right) and, for that account, make sure",
				"Then run the same command again without -enable-offline."),
			assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) }))
	})

	t.Run("a hand-edited line", func(t *testing.T) {
		profile, store := offlineProfile(t, setOffOwnFolder)
		asStore(t, store)
		b, _ := os.ReadFile(filepath.Join(profile, "prefs.js"))
		b = bytes.Replace(b, []byte(`offline_download", false);`), []byte(`offline_download", false); // mine`), 1)
		if err := os.WriteFile(filepath.Join(profile, "prefs.js"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		before := snapshot(t, profile)
		code, msg, _ := enableOfflineRun(t, store)
		assure.Refused(t, code, msg, assure.Code(1),
			// Since the allow-list (MA-1264) a comment beside the statement is a line Thunderbird
			// never writes, named as such, no longer "together with something else".
			assure.Names("-enable-offline changed nothing, and nothing was exported: prefs.js line 6 holds more after its user_pref statement (another statement or a comment). Thunderbird never writes such a line",
				"MailArchive cannot be sure what Thunderbird applies for mail.server.server1.offline_download",
				"In Thunderbird, open Account Settings (the menu button ≡, top right) and, for the account on imap.gmail.com, make sure",
				"Then run the same command again without -enable-offline."),
			assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) }))
	})

	t.Run("Thunderbird running", func(t *testing.T) {
		profile, store := offlineProfile(t, setOffOwnFolder)
		asStore(t, store)
		if err := os.Symlink("127.0.0.1:+"+strconv.Itoa(os.Getpid()), filepath.Join(profile, "lock")); err != nil {
			t.Skip("no links here (Windows holds parent.lock open instead):", err)
		}
		before := snapshot(t, profile)
		code, msg, _ := enableOfflineRun(t, store)
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names("-enable-offline changed nothing, and nothing was exported: Thunderbird looks like it is running for the profile "+profile,
				"Close it, then run the same command again"),
			assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) }))
	})
}
