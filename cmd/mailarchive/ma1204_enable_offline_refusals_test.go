package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// snapshot is every file under dir, by path, with its bytes (a link by its target, never
// followed: Thunderbird's lock is a link to "<ip>:+<pid>").
func snapshot(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, lerr := os.Readlink(p)
			out[p] = []byte("link -> " + target)
			return lerr
		}
		b, rerr := os.ReadFile(p)
		out[p] = b
		return rerr
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sameFiles reports whether dir holds exactly the files of before, byte for byte.
func sameFiles(t *testing.T, dir string, before map[string][]byte) bool {
	t.Helper()
	now := snapshot(t, dir)
	if len(now) != len(before) {
		return false
	}
	for p, b := range before {
		if !bytes.Equal(now[p], b) {
			return false
		}
	}
	return true
}

// enableOfflineRun runs the command line's -enable-offline preparation over inputs, as
// runExport does, and returns the exit code it gives (exitCode), its error words and its log.
func enableOfflineRun(t *testing.T, inputs ...string) (int, string, string) {
	t.Helper()
	var buf bytes.Buffer
	err := prepareThunderbird(context.Background(), inputs, false, true, false, log.New(&buf, "", 0))
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return exitCode(err), msg, buf.String()
}

// asStore makes dir look like a Thunderbird store to input discovery (a summary file).
func asStore(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "INBOX.msf"), []byte("// summary\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	labelOld = `"Keep messages in all folders for this account on this computer"`
	labelNew = `"Save copies of all folders and messages on this computer for offline use"`
)

// covers: MA-1203
// A Thunderbird folder named "prefs.js" (the mbox file ImapMail/<host>/prefs.js) is never
// taken for the profile's settings (the C0c pass's INS-01): -enable-offline changes no byte of
// any file in the store, writes no backup there, and turns on the real account in the
// profile's own prefs.js. Positive twin first: the same profile without that folder is turned
// on.
func TestAMailFolderNamedPrefsJsIsNeverWritten(t *testing.T) {
	setOff := "user_pref(\"mail.server.server1.directory-rel\", \"[ProfD]ImapMail/imap.gmail.com\");\n" +
		"user_pref(\"mail.server.server1.offline_download\", false);\n"
	profile, store := offlineProfile(t, setOff)
	enableOfflineLog(t, store)
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js")); !strings.Contains(string(b), `server1.offline_download", true`) {
		t.Fatalf("the positive twin was not turned on:\n%s", b)
	}

	profile, store = offlineProfile(t, setOff)
	msg := "From - Sat Nov 07 10:00:00 2026\nFrom: someone@example.com\nSubject: hi\n\n" +
		"user_pref(\"mail.server.server9.directory-rel\", \"[ProfD]\");\n" +
		"user_pref(\"mail.accountmanager.accounts\", \"account9\");\n" +
		"user_pref(\"mail.account.account9.server\", \"server9\");\n\n"
	if err := os.WriteFile(filepath.Join(store, "prefs.js"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "prefs.js.msf"), []byte("// summary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, store)
	logText := enableOfflineLog(t, store)
	if !sameFiles(t, store, before) {
		t.Errorf("a file in the store ImapMail/imap.gmail.com was changed or added; log:\n%s", logText)
	}
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js")); !strings.Contains(string(b), `server1.offline_download", true`) {
		t.Errorf("the real account was not turned on; log:\n%s", logText)
	}
}

// covers: MA-1204, R12
// When MailArchive cannot tell which account a store belongs to, -enable-offline refuses
// (the C0c pass's OUT-01): the run stops with the command line's refusal code (1, unchanged),
// and the words say that nothing was changed and nothing was exported, why (the reason naming
// prefs.js and the accounts), that a removed account's leftover folder can cause it, what to
// make sure of by hand for each account on that server and where it is, labelled for the
// Thunderbird version that last opened the profile (§6 fold L5: below 160 the older label on
// "Synchronization & Storage", 160 and above the newer one under "Offline message storage",
// unknown both), and to run again without -enable-offline (the friction review's W5, F1).
// Nothing is written: prefs.js keeps its bytes, no backup appears, and a second store in the same
// run that could be prepared is not changed either. Positive twin first: one account is turned
// on with exit 0.
func TestNoAccountRefusesWithTheReason(t *testing.T) {
	profile, store := offlineProfile(t, "user_pref(\"mail.server.server1.offline_download\", false);\n")
	asStore(t, store)
	if code, msg, _ := enableOfflineRun(t, store); code != 0 {
		t.Fatalf("the positive twin: exit %d, %s", code, msg)
	}
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js")); !strings.Contains(string(b), `server1.offline_download", true`) {
		t.Fatalf("the positive twin was not turned on:\n%s", b)
	}

	ambiguous := "user_pref(\"mail.accountmanager.accounts\", \"account1,account2\");\n" +
		"user_pref(\"mail.account.account2.server\", \"server2\");\n" +
		"user_pref(\"mail.server.server2.hostname\", \"imap.gmail.com\");\n" +
		"user_pref(\"mail.server.server2.type\", \"imap\");\n"
	for _, tc := range []struct {
		name, compat string
		want, never  []string
	}{
		{"no version", "LastVersion=not-a-version", []string{
			`make sure ` + labelOld + ` is ticked (on its "Synchronization & Storage" page; in Thunderbird 160 and later, ` + labelNew + ` under "Offline message storage").`}, nil},
		{"153", "LastVersion=153.6.0_20251212000000/20251212000000", []string{
			`make sure ` + labelOld + ` is ticked (on its "Synchronization & Storage" page).`}, []string{labelNew, "Offline message storage"}},
		{"160", "LastVersion=160.0_20261201000000/20261201000000", []string{
			`make sure ` + labelNew + ` is ticked (under "Offline message storage").`}, []string{labelOld, "Synchronization & Storage"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, store := offlineProfile(t, ambiguous)
			asStore(t, store)
			writeCompat(t, profile, "[Compatibility]\n"+tc.compat+"\n")
			before := snapshot(t, profile)
			code, msg, _ := enableOfflineRun(t, store)
			assure.Refused(t, code, msg, assure.Code(1),
				assure.Names(append([]string{
					"-enable-offline changed nothing, and nothing was exported: MailArchive could not tell which Thunderbird account the mail in imap.gmail.com belongs to",
					"more than one account in prefs.js (server1, server2)",
					"If you removed an account from Thunderbird, its leftover mail folder can cause this; if so, you can ignore this message.",
					"In Thunderbird, open Account Settings and, for each account on imap.gmail.com, make sure",
					"Then run the same command again without -enable-offline.",
				}, tc.want...)...),
				assure.Forbid(tc.never...),
				assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) }))
		})
	}

	t.Run("one store's own step", func(t *testing.T) {
		// prepareStore refuses on its own too, not only behind prepareThunderbird's check.
		profile, store := offlineProfile(t, ambiguous)
		before := snapshot(t, profile)
		var buf bytes.Buffer
		err := prepareStore(context.Background(), store, true, false, log.New(&buf, "", 0))
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		assure.Refused(t, exitCode(err), msg, assure.Code(1),
			assure.Names("more than one account", "changed nothing"),
			assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) }))
	})

	t.Run("a second store in the run", func(t *testing.T) {
		profile, good := offlineProfile(t, "user_pref(\"mail.server.server1.directory-rel\", \"[ProfD]ImapMail/imap.gmail.com\");\n"+
			"user_pref(\"mail.server.server1.offline_download\", false);\n"+
			"user_pref(\"mail.accountmanager.accounts\", \"account1,account2,account3\");\n"+
			"user_pref(\"mail.account.account2.server\", \"server2\");\n"+
			"user_pref(\"mail.account.account3.server\", \"server3\");\n"+
			"user_pref(\"mail.server.server2.hostname\", \"mail.example.com\");\n"+
			"user_pref(\"mail.server.server3.hostname\", \"mail.example.com\");\n")
		bad := filepath.Join(profile, "ImapMail", "mail.example.com")
		if err := os.MkdirAll(bad, 0o755); err != nil {
			t.Fatal(err)
		}
		asStore(t, good)
		asStore(t, bad)
		before := snapshot(t, profile)
		code, msg, _ := enableOfflineRun(t, good, bad)
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names("mail.example.com", "changed nothing", "more than one account"),
			assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) }))
	})
}

// covers: MA-1195, R12
// One run over two stores reports the backup truly (the C0c pass's CON-04): the first change
// writes prefs.js.mailarchive-bak, and the second says prefs.js was not backed up again because
// that backup still holds prefs.js from before MailArchive first changed it, never that it
// backed this change up. Each line names the account (its folder and server), not only the
// server, and says the backup is in the Thunderbird profile folder (the friction review's W7,
// F10). Positive twin first: the first store's line says it backed prefs.js up.
func TestTheKeptBackupIsReportedTruly(t *testing.T) {
	profile := twoAccountProfile(t)
	first, second := filepath.Join(profile, "ImapMail", "imap.gmail.com"), filepath.Join(profile, "ImapMail", "imap.gmail.com-1")
	asStore(t, first)
	asStore(t, second)
	original, _ := os.ReadFile(filepath.Join(profile, "prefs.js"))
	code, msg, logText := enableOfflineRun(t, first, second)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, msg)
	}
	if !strings.Contains(logText, "Enabled offline download for imap.gmail.com (folder imap.gmail.com, server1); prefs.js backed up to prefs.js.mailarchive-bak in the Thunderbird profile folder.") {
		t.Fatalf("the positive twin: the first line does not say it backed prefs.js up:\n%s", logText)
	}
	if strings.Count(logText, "prefs.js backed up to") != 1 ||
		!strings.Contains(logText, "Enabled offline download for imap.gmail.com (folder imap.gmail.com-1, server2). prefs.js was not backed up again: prefs.js.mailarchive-bak, in the Thunderbird profile folder, still holds prefs.js from before MailArchive first changed it.") {
		t.Fatalf("the second change does not say the backup was kept:\n%s", logText)
	}
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js.mailarchive-bak")); !bytes.Equal(b, original) {
		t.Fatalf("the backup is not the original prefs.js:\n%s", b)
	}
}

// covers: MA-1205
// When user.js sets the account's offline setting to off, -enable-offline changes nothing
// and says user.js controls it, naming the file (the C0c pass's CON-06), with the refusal
// code (1). The words name the server, say nothing was exported, say to leave it if you or
// your IT set it on purpose and run again without -enable-offline, and give the file's path
// last (the friction review's W6, F9). Positive twin first: an empty user.js does not stop the
// change.
func TestUserJSRefusalOnTheCommandLine(t *testing.T) {
	setOff := "user_pref(\"mail.server.server1.offline_download\", false);\n"
	profile, store := offlineProfile(t, setOff)
	asStore(t, store)
	if err := os.WriteFile(filepath.Join(profile, "user.js"), []byte("// nothing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, msg, _ := enableOfflineRun(t, store); code != 0 {
		t.Fatalf("the positive twin: exit %d, %s", code, msg)
	}

	profile, store = offlineProfile(t, setOff)
	asStore(t, store)
	userJS := filepath.Join(profile, "user.js")
	if err := os.WriteFile(userJS, []byte(setOff), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, profile)
	code, msg, _ := enableOfflineRun(t, store)
	if !strings.HasSuffix(msg, "The file: "+userJS) {
		t.Errorf("the path is not last: %s", msg)
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(userJS, "changed nothing", "mail.server.server1.offline_download",
			"-enable-offline changed nothing, and nothing was exported",
			"user.js sets mail.server.server1.offline_download (imap.gmail.com)",
			"If you or your IT set this on purpose, leave it and run again without -enable-offline."),
		assure.NoSideEffect(func() bool {
			_, err := os.Lstat(filepath.Join(profile, "prefs.js.mailarchive-bak"))
			return sameFiles(t, profile, before) && errors.Is(err, fs.ErrNotExist)
		}))
}
