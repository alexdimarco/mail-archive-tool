package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1261, MA-1271, R12, S10
// -enable-offline never says "already on" unless it read prefs.js and user.js with certainty (the
// C0c pass round 3's R3-01, end to end). A user.js turning the setting on with pref( (which
// Thunderbird ignores in a user's settings file) was logged "Offline download already enabled …
// (user.js turns it on)" and the export ran on an account Thunderbird keeps off. It now refuses
// with exit 1 and the hand-edit words, names user.js, and leaves the profile byte for byte.
// Positive twin first: user.js turning it on with user_pref( is "already enabled".
func TestAPrefCallInUserJSIsNeverAlreadyOn(t *testing.T) {
	profile, store := offlineProfile(t, setOffOwnFolder)
	writeUserJS(t, profile, `user_pref("mail.server.server1.offline_download", true);`+"\n")
	if logText := enableOfflineLog(t, store); !strings.Contains(logText, "already enabled") {
		t.Fatalf("the positive twin: %s", logText)
	}

	for _, fn := range []string{"pref", "sticky_pref"} {
		t.Run(fn, func(t *testing.T) {
			profile, store := offlineProfile(t, setOffOwnFolder)
			writeUserJS(t, profile, fn+`("mail.server.server1.offline_download", true);`+"\n")
			before := snapshot(t, profile)
			var buf bytes.Buffer
			err := prepareStore(context.Background(), store, true, false, log.New(&buf, "", 0))
			rc, words := 0, ""
			if err != nil {
				rc, words = exitCode(err), err.Error()
			}
			assure.Refused(t, rc, words, assure.Code(1),
				// MA-1271: user.js is never Thunderbird's, so its words say whose file it is.
				assure.Names("-enable-offline changed nothing, and nothing was exported",
					"user.js has a line MailArchive cannot read with certainty (user.js is a file people or add-ons write by hand). Its line 1 holds a "+fn+"( statement"),
				assure.NoSideEffect(func() bool {
					return sameFiles(t, profile, before) && !strings.Contains(buf.String(), "already enabled")
				}))
			if strings.Contains(words, "changed outside Thunderbird") {
				t.Errorf("user.js is said to be changed outside Thunderbird: %s", words)
			}
		})
	}
}

// covers: MA-1262, R12, S10
// The C0c pass round 3's R3-05 end to end: -enable-offline pointed at the store of a copied
// profile kept under this profile's Mail/ without its own compatibility.ini no longer changes this
// profile's live account by hostname: it refuses with exit 1 and no file of the profile changes.
// Positive twin first: the profile's own store is turned on.
func TestACopiedProfileStoreNeverChangesTheOuterAccount(t *testing.T) {
	profile, store := offlineProfile(t, "user_pref(\"mail.server.server1.offline_download\", false);\n")
	enableOfflineLog(t, store)
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js")); !strings.Contains(string(b), `server1.offline_download", true`) {
		t.Fatalf("the positive twin was not turned on:\n%s", b)
	}

	profile, _ = offlineProfile(t, "user_pref(\"mail.server.server1.offline_download\", false);\n")
	old := filepath.Join(profile, "Mail", "old-profile")
	copyStore := filepath.Join(old, "ImapMail", "imap.gmail.com")
	if err := os.MkdirAll(copyStore, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "prefs.js"), []byte("user_pref(\"mail.accountmanager.accounts\", \"account7\");\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, profile)
	var buf bytes.Buffer
	err := prepareStore(context.Background(), copyStore, true, false, log.New(&buf, "", 0))
	rc, words := 0, ""
	if err != nil {
		rc, words = exitCode(err), err.Error()
	}
	assure.Refused(t, rc, words, assure.Code(1), assure.Names("changed nothing", "imap.gmail.com"),
		assure.NoSideEffect(func() bool {
			return sameFiles(t, profile, before) && !strings.Contains(buf.String(), "Enabled offline download")
		}))
}

// writeUserJS writes the profile's user.js.
func writeUserJS(t *testing.T, profile, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(profile, "user.js"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
