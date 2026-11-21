package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
)

// covers: MA-1481, MA-1480, R12
// -enable-offline's and -sync-wait's discovery (prepareThunderbird) asks the run's own rule with the
// archive record's names (the 1.7.5 re-verify, round 3's RV3-03), so it never refuses where the run
// skips, nor goes ahead where the run stops. Three accounts on one mail server: the archive holds the
// first two, the third has always kept its mail off the profile; the record is a stopped backup's,
// which names the folders (RV3-01). Positive twin first: the second account's folder deleted by hand
// (Thunderbird still names it) is skipped, as the run skips it, where the server-name rule alone
// would stop. Then the second account moved to another drive: refused, as the run refuses, where the
// server-name rule alone would go ahead; prefs.js is left as it was.
func TestPrepareAsksWithTheRecordsNames(t *testing.T) {
	restore := tbRunning
	t.Cleanup(func() { tbRunning = restore })
	tbRunning = func(string) bool { return false }
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	profile := filepath.Join(home, ".thunderbird", "abcd1234.default-release")
	a := filepath.Join(profile, "ImapMail", "imap.qzg.example")
	b := filepath.Join(profile, "ImapMail", "imap.qzg-1.example")
	third := filepath.Join(t.TempDir(), "QZUSB", "QZGmail3")
	for _, d := range []string{a, b, third} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		asStore(t, d)
	}
	writeCompat(t, profile, testCompatINI)
	prefs := filepath.Join(profile, "prefs.js")
	write := func(bDir string) {
		t.Helper()
		var s strings.Builder
		line := func(k, v string) { fmt.Fprintf(&s, "user_pref(%q, %s);\n", k, v) }
		q := func(v string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"` }
		line("mail.accountmanager.accounts", q("account1,account2,account3"))
		for i, name := range []string{"qza@qzg.example", "qzb@qzg.example", "qzc@qzg.example"} {
			n := fmt.Sprint(i + 1)
			line("mail.account.account"+n+".server", q("server"+n))
			line("mail.server.server"+n+".hostname", q("imap.qzg.example"))
			line("mail.server.server"+n+".type", q("imap"))
			line("mail.server.server"+n+".name", q(name))
			line("mail.server.server"+n+".offline_download", "false")
		}
		line("mail.server.server1.directory-rel", q("[ProfD]ImapMail/imap.qzg.example"))
		if bDir == "" {
			line("mail.server.server2.directory-rel", q("[ProfD]ImapMail/imap.qzg-1.example"))
		} else {
			line("mail.server.server2.directory", q(bDir))
		}
		line("mail.server.server3.directory", q(third))
		if err := os.WriteFile(prefs, []byte(s.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	// The archive's record as a stopped backup writes it: the folders' names carried (RV3-01).
	out := filepath.Join(t.TempDir(), "arch")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Error: "qz stopped", ThunderbirdNames: []state.TBName{
		{StoreID: autoplace.StoreID(profile, a), Name: "qza@qzg.example"},
		{StoreID: autoplace.StoreID(profile, b), Name: "qzb@qzg.example"},
	}}); err != nil {
		t.Fatal(err)
	}
	inputs := []string{a, b}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	if _, skipped, err := app.DiscoverInputsFor(inputs, true, app.RecordedNamesAt(out)); err != nil || len(skipped) != 1 {
		t.Fatalf("setup: the run skips a folder deleted by hand: %v %v", skipped, err)
	}
	var buf bytes.Buffer
	if err := prepareThunderbird(context.Background(), out, inputs, true, true, false, log.New(&buf, "", 0)); err != nil {
		t.Fatalf("positive twin: the run skips the folder deleted by hand, so the prep must too: %v\n%s", err, buf.String())
	}

	away := filepath.Join(t.TempDir(), "QZUSB", "QZGmail2")
	if err := os.MkdirAll(away, 0o755); err != nil {
		t.Fatal(err)
	}
	asStore(t, away)
	write(away)
	if _, _, err := app.DiscoverInputsFor(inputs, true, app.RecordedNamesAt(out)); err == nil {
		t.Fatal("setup: the run stops for the moved account")
	}
	before, _ := os.ReadFile(prefs)
	err := prepareThunderbird(context.Background(), out, inputs, true, true, false, log.New(&buf, "", 0))
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("input "+b+" does not exist", away, "Thunderbird still has the account that used it"),
		assure.Forbid("qzb@qzg.example"),
		assure.NoSideEffect(func() bool { after, _ := os.ReadFile(prefs); return bytes.Equal(before, after) }))
}
