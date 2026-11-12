package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

const setOffOwnFolder = "user_pref(\"mail.server.server1.directory-rel\", \"[ProfD]ImapMail/imap.gmail.com\");\n" +
	"user_pref(\"mail.server.server1.offline_download\", false);\n"

// covers: MA-1208, R12
// -enable-offline checks every store for every refusal before it changes any (the C0c pass round
// 2's R2-05): Thunderbird running for that profile, a backup path that is not a regular file, a
// hand-edited line it will not rewrite, user.js keeping the setting off, and no account it can
// tell. A refusal for a later store leaves an earlier store's prefs.js byte-identical, with no
// backup, and the run exits 1. Positive twin first: two healthy profiles are both turned on.
func TestALaterStoreRefusalLeavesEarlierStoresAsTheyWere(t *testing.T) {
	first, storeA := offlineProfile(t, setOffOwnFolder)
	second, storeB := offlineProfile(t, setOffOwnFolder)
	asStore(t, storeA)
	asStore(t, storeB)
	if code, msg, logText := enableOfflineRun(t, storeA, storeB); code != 0 {
		t.Fatalf("the positive twin: exit %d, %s\n%s", code, msg, logText)
	}
	for _, p := range []string{first, second} {
		if b, _ := os.ReadFile(filepath.Join(p, "prefs.js")); !strings.Contains(string(b), `server1.offline_download", true`) {
			t.Fatalf("the positive twin: %s was not turned on:\n%s", p, b)
		}
	}

	plant := map[string]func(t *testing.T, profile string){
		"Thunderbird running for the second profile": func(t *testing.T, profile string) {
			if runtime.GOOS == "windows" {
				t.Skip("the lock link is the Unix rule; Windows holds parent.lock open, which a test cannot hold the same way")
			}
			if err := os.Symlink(fmt.Sprintf("127.0.0.1:+%d", os.Getpid()), filepath.Join(profile, "lock")); err != nil {
				t.Skip("no links here:", err)
			}
		},
		"a folder at the second profile's backup": func(t *testing.T, profile string) {
			if err := os.Mkdir(filepath.Join(profile, "prefs.js.mailarchive-bak"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"a hand-edited line in the second profile": func(t *testing.T, profile string) {
			b, _ := os.ReadFile(filepath.Join(profile, "prefs.js"))
			b = bytes.Replace(b, []byte(`offline_download", false);`), []byte(`offline_download", false); // mine`), 1)
			if err := os.WriteFile(filepath.Join(profile, "prefs.js"), b, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"user.js keeps the second profile off": func(t *testing.T, profile string) {
			if err := os.WriteFile(filepath.Join(profile, "user.js"), []byte("user_pref(\"mail.server.server1.offline_download\", false);\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mk := range plant {
		t.Run(name, func(t *testing.T) {
			first, storeA := offlineProfile(t, setOffOwnFolder)
			second, storeB := offlineProfile(t, setOffOwnFolder)
			asStore(t, storeA)
			asStore(t, storeB)
			mk(t, second)
			beforeA, beforeB := snapshot(t, first), snapshot(t, second)
			code, msg, logText := enableOfflineRun(t, storeA, storeB)
			assure.Refused(t, code, msg, assure.Code(1),
				assure.Names("-enable-offline changed nothing, and nothing was exported"),
				assure.NoSideEffect(func() bool {
					return sameFiles(t, first, beforeA) && sameFiles(t, second, beforeB) && !strings.Contains(logText, "Enabled offline download")
				}))
		})
	}
}

// covers: MA-1206, S10
// The C0c pass round 2's R2-03 end to end: an IMAP folder named Mail kept as a folder (maildir)
// inside an account whose folder "prefs.js" is an mbox file holding a crafted message.
// -enable-offline pointed at that Mail folder never takes the store for the profile: no file in
// the store is changed or added. Positive twin first: the store itself is turned on in the real
// profile.
func TestAMailFolderInsideAStoreIsNeverWritten(t *testing.T) {
	profile, store := offlineProfile(t, setOffOwnFolder)
	enableOfflineLog(t, store)
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js")); !strings.Contains(string(b), `server1.offline_download", true`) {
		t.Fatalf("the positive twin was not turned on:\n%s", b)
	}

	profile, store = offlineProfile(t, setOffOwnFolder)
	sub := filepath.Join(store, "Mail")
	if err := os.MkdirAll(filepath.Join(sub, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From - Sat Nov 07 10:00:00 2026\nFrom: someone@example.com\nSubject: hi\n\n" +
		"user_pref(\"mail.accountmanager.accounts\", \"account9\");\n" +
		"user_pref(\"mail.account.account9.server\", \"server9\");\n" +
		"user_pref(\"mail.server.server9.hostname\", \"Mail\");\n" +
		"user_pref(\"mail.server.server9.offline_download\", false);\n\n"
	if err := os.WriteFile(filepath.Join(store, "prefs.js"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "compatibility.ini"), []byte("From - Sat Nov 07 10:00:00 2026\n\n[Compatibility]\nLastVersion=153.6.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeStore, beforeProfile := snapshot(t, store), snapshot(t, profile)
	var buf bytes.Buffer
	err := prepareStore(context.Background(), sub, true, false, log.New(&buf, "", 0))
	rc, words := 0, ""
	if err != nil {
		rc, words = exitCode(err), err.Error()
	}
	assure.Refused(t, rc, words, assure.Code(1), assure.Names("changed nothing"),
		assure.NoSideEffect(func() bool { return sameFiles(t, store, beforeStore) && sameFiles(t, profile, beforeProfile) }))
}
