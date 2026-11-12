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

// testCompatINI is compatibility.ini as Thunderbird 153 writes it at every start; a profile is
// known by it and prefs.js (MA-1206).
const testCompatINI = "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\nLastOSABI=Darwin_aarch64-gcc3\n"

// writeCompat writes the profile's compatibility.ini.
func writeCompat(t *testing.T, profile, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(profile, "compatibility.ini"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// offlineProfile makes a Thunderbird profile (prefs.js and compatibility.ini, Thunderbird 153)
// whose prefs.js holds an IMAP account for imap.gmail.com (server1, listed as a live account)
// plus extra, and returns the account's store folder. A mail.accountmanager.accounts line in
// extra comes later, so it wins, as in Thunderbird.
func offlineProfile(t *testing.T, extra string) (profile, store string) {
	t.Helper()
	profile = t.TempDir()
	store = filepath.Join(profile, "ImapMail", "imap.gmail.com")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	prefs := "user_pref(\"mail.accountmanager.accounts\", \"account1\");\n" +
		"user_pref(\"mail.account.account1.server\", \"server1\");\n" +
		"user_pref(\"mail.server.server1.hostname\", \"imap.gmail.com\");\n" +
		"user_pref(\"mail.server.server1.type\", \"imap\");\n" + extra
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	writeCompat(t, profile, testCompatINI)
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
	// The words name the account (its folder and server) and where the backup is (the C0c
	// friction review's W7).
	enabled := "Enabled offline download for imap.gmail.com (folder imap.gmail.com, server1); prefs.js backed up to prefs.js.mailarchive-bak in the Thunderbird profile folder."

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
	want := "Offline download was already on for imap.gmail.com (folder imap.gmail.com, server1): Thunderbird keeps a new account's messages on this computer by default. The setting was written into prefs.js to make sure; prefs.js backed up to prefs.js.mailarchive-bak in the Thunderbird profile folder."
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
