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

// twoStoresOneProfile is one profile holding two IMAP accounts, both off: server1 at
// ImapMail/imap.gmail.com and server2 at ImapMail/outlook.office365.com, both seen as stores.
func twoStoresOneProfile(t *testing.T) (profile, storeA, storeB string) {
	t.Helper()
	extra := setOffOwnFolder +
		"user_pref(\"mail.accountmanager.accounts\", \"account1,account2\");\n" +
		"user_pref(\"mail.account.account2.server\", \"server2\");\n" +
		"user_pref(\"mail.server.server2.hostname\", \"outlook.office365.com\");\n" +
		"user_pref(\"mail.server.server2.type\", \"imap\");\n" +
		"user_pref(\"mail.server.server2.directory-rel\", \"[ProfD]ImapMail/outlook.office365.com\");\n" +
		"user_pref(\"mail.server.server2.offline_download\", false);\n"
	profile, storeA = offlineProfile(t, extra)
	storeB = filepath.Join(profile, "ImapMail", "outlook.office365.com")
	if err := os.MkdirAll(storeB, 0o755); err != nil {
		t.Fatal(err)
	}
	asStore(t, storeA)
	asStore(t, storeB)
	return profile, storeA, storeB
}

// covers: MA-1266, R12, S10
// -enable-offline -sync-wait with several stores checks every store, then changes every store
// with Thunderbird closed throughout, then says "START Thunderbird" once, then waits for every
// store (the C0c pass round 4's R4-04). Before, the changes and the waits took turns, so a person
// who started Thunderbird at the first store's wait had the second store refused for it ("looks
// like it is running"), on every run: the second account was never turned on. Thunderbird's
// running state is injected (tbRunning): it turns true once the log has said "START Thunderbird",
// as a person following the log does. stdin is not a terminal under go test, so each wait
// returns at once. Positive twin first: without -sync-wait both accounts are turned on, with one
// "START Thunderbird". Refusal twin: Thunderbird running before the run changes nothing.
func TestSyncWaitChangesEveryStoreBeforeStartingThunderbird(t *testing.T) {
	restore := tbRunning
	t.Cleanup(func() { tbRunning = restore })
	on := func(profile, key string) bool {
		b, _ := os.ReadFile(filepath.Join(profile, "prefs.js"))
		return strings.Contains(string(b), key+`.offline_download", true`)
	}

	profile, storeA, storeB := twoStoresOneProfile(t)
	var buf bytes.Buffer
	if err := prepareThunderbird(context.Background(), []string{storeA, storeB}, false, true, false, log.New(&buf, "", 0)); err != nil || !on(profile, "server1") || !on(profile, "server2") {
		t.Fatalf("the positive twin: %v\n%s", err, buf.String())
	}
	if n := strings.Count(buf.String(), "START Thunderbird"); n != 1 {
		t.Fatalf("the positive twin says START Thunderbird %d times:\n%s", n, buf.String())
	}

	profile, storeA, storeB = twoStoresOneProfile(t)
	buf.Reset()
	started := false
	tbRunning = func(string) bool {
		started = started || strings.Contains(buf.String(), "START Thunderbird")
		return started
	}
	err := prepareThunderbird(context.Background(), []string{storeA, storeB}, false, true, true, log.New(&buf, "", 0))
	logText := buf.String()
	if err != nil || !on(profile, "server1") || !on(profile, "server2") {
		t.Fatalf("one run that follows the log's steps leaves an account off: %v\n%s", err, logText)
	}
	start := strings.Index(logText, "Next: START Thunderbird, then right-click each account -> Download/Sync Now.")
	if start < 0 || strings.Count(logText, "START Thunderbird") != 1 {
		t.Fatalf("not one START Thunderbird for both accounts:\n%s", logText)
	}
	if strings.LastIndex(logText, "Enabled offline download") > start || strings.Index(logText, "Waiting for") < start ||
		strings.Count(logText[start:], "Waiting for") != 2 {
		t.Fatalf("not every change before START Thunderbird and every wait after it:\n%s", logText)
	}

	profile, storeA, storeB = twoStoresOneProfile(t)
	before := snapshot(t, profile)
	tbRunning = func(string) bool { return true }
	buf.Reset()
	err = prepareThunderbird(context.Background(), []string{storeA, storeB}, false, true, true, log.New(&buf, "", 0))
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("-enable-offline changed nothing, and nothing was exported: Thunderbird looks like it is running for the profile "+profile),
		assure.NoSideEffect(func() bool { return sameFiles(t, profile, before) && !strings.Contains(buf.String(), "Waiting for") }))
}
