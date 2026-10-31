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

// offlineProfile makes a Thunderbird profile whose prefs.js holds an IMAP account for
// imap.gmail.com plus extra, and returns the account's store folder.
func offlineProfile(t *testing.T, extra string) (profile, store string) {
	t.Helper()
	profile = t.TempDir()
	store = filepath.Join(profile, "ImapMail", "imap.gmail.com")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	prefs := "user_pref(\"mail.server.server1.hostname\", \"imap.gmail.com\");\n" +
		"user_pref(\"mail.server.server1.type\", \"imap\");\n" + extra
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	return profile, store
}

// enableOfflineLog runs the command line's -enable-offline step for store and returns its log.
func enableOfflineLog(t *testing.T, store string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := prepareStore(context.Background(), store, true, false, log.New(&buf, "", 0)); err != nil {
		t.Fatal(err)
	}
	return assure.Reached(t, buf.String(), "the -enable-offline log")
}

// covers: MA-947, R12
// -enable-offline says truly what it did (review finding, 2026-10-31). An account with no
// offline_download line in prefs.js uses Thunderbird's default, which keeps a new account's
// messages on this computer (the Windows lab of 2026-10-31), so writing the setting out
// changes nothing Thunderbird does: the log says it was on already and was written to make
// sure, never "Enabled offline download". Positive twin first: an account set to off is
// turned on and the log says "Enabled", as before; so is one with no line of its own where
// prefs.js turns the default off for every account (mail.server.default.offline_download).
func TestEnableOfflineSaysWhenItWasOnByDefault(t *testing.T) {
	enabled := "Enabled offline download for imap.gmail.com (prefs backed up to prefs.js.mailarchive-bak)."

	// The positive twin: the account was set to off.
	profile, store := offlineProfile(t, "user_pref(\"mail.server.server1.offline_download\", false);\n")
	if got := enableOfflineLog(t, store); !strings.Contains(got, enabled) {
		t.Errorf("an account set to off: the log does not say %q:\n%s", enabled, got)
	}
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js")); !strings.Contains(string(b), `user_pref("mail.server.server1.offline_download", true);`) {
		t.Errorf("an account set to off was not turned on:\n%s", b)
	}

	// The default turned off for every account, and no line of its own: it was off.
	_, store = offlineProfile(t, "user_pref(\"mail.server.default.offline_download\", false);\n")
	if got := enableOfflineLog(t, store); !strings.Contains(got, enabled) {
		t.Errorf("an account whose default is off: the log does not say %q:\n%s", enabled, got)
	}

	// No line at all: Thunderbird's default had it on.
	profile, store = offlineProfile(t, "")
	got := enableOfflineLog(t, store)
	want := "Offline download was already on for imap.gmail.com: Thunderbird keeps a new account's messages on this computer by default. The setting was written into prefs.js to make sure (prefs backed up to prefs.js.mailarchive-bak)."
	if !strings.Contains(got, want) {
		t.Errorf("an account with Thunderbird's default: the log does not say %q:\n%s", want, got)
	}
	if strings.Contains(got, "Enabled offline download") {
		t.Errorf("an account with Thunderbird's default: the log claims a change Thunderbird does not see:\n%s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(profile, "prefs.js")); !strings.Contains(string(b), `user_pref("mail.server.server1.offline_download", true);`) {
		t.Errorf("the setting was not written out:\n%s", b)
	}
}
