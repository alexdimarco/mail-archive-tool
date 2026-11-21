package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-1471, MA-1472, MA-1288, R18
// A gone mailbox is named, and its remedy fits (the 1.7.5 friction review's F3 and F4): an account
// removed on purpose in Thunderbird is no longer in its settings, so its name is taken from the
// previous record's Thunderbird entry for the same store (store_id), and then carried by the next
// run from the previous gone-mailbox warning; it is said on screen and in status ("add the account
// back"), never in a log file (MA-1288). An account Thunderbird still lists (its folder deleted by
// hand) is recorded as listed, and status tells the person to let Thunderbird download it. Positive
// twin first: the listed account, named from the settings.
func TestAGoneMailboxIsNamedFromTheLastRecordAndItsRemedyFits(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	addr := goneAddress("imap.qzb.example")
	out := tmpDir(t)
	opts := Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true}
	if _, err, _ := goneRun(t, opts); err != nil {
		t.Fatal(err)
	}
	lr, _, _ := state.ReadLastRun(out)
	named := false
	for _, e := range lr.Thunderbird {
		named = named || (e.Name == addr && e.StoreID != "")
	}
	if !named {
		t.Fatalf("the first run's record names the account beside its store_id: %+v", lr.Thunderbird)
	}
	status := func() string {
		return strings.Join(health.Assess(health.Gather(out, ""), time.Now()).Reasons, "\n")
	}

	// Positive twin: the folder deleted by hand, the account still listed.
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	res, err, _ := goneRun(t, opts)
	if err != nil || len(res.Skipped) != 1 || res.Skipped[0].Name != addr || !res.Skipped[0].Listed {
		t.Fatalf("positive twin: a listed account is named from the settings and marked listed: %+v %v", res.Skipped, err)
	}
	if got := status(); !strings.Contains(got, "Thunderbird still lists this account: open Thunderbird and let it download the account's mail") || strings.Contains(got, "add the account back") {
		t.Errorf("F4: status for an account Thunderbird still lists:\n%s", got)
	}

	// The account removed on purpose: its name from the previous record (the run before the skip).
	out2 := tmpDir(t)
	opts2 := opts
	opts2.Out = out2
	tbStoreAt(t, b, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzb.example", 2)})
	if _, err, _ := goneRun(t, opts2); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	tbWrite(t, filepath.Join(f.profile, "prefs.js"), tbPrefsJS(tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"` + goneAddress("imap.qza.example") + `"`}}))
	for run := 1; run <= 2; run++ {
		res, err, logged := goneRun(t, opts2)
		if err != nil || len(res.Skipped) != 1 || res.Skipped[0].Name != addr || res.Skipped[0].Listed {
			t.Fatalf("F3, run %d: an account removed on purpose is named from the previous record, not listed: %+v %v", run, res.Skipped, err)
		}
		if strings.Contains(logged.String(), addr) {
			t.Errorf("F3, run %d: the run's log names the account (MA-1288):\n%s", run, logged)
		}
		got := strings.Join(health.Assess(health.Gather(out2, ""), time.Now()).Reasons, "\n")
		if !strings.Contains(got, addr+" ("+b+")") || !strings.Contains(got, "If you didn't remove it, add the account back in Thunderbird") {
			t.Errorf("F3, run %d: status names it and says to add it back:\n%s", run, got)
		}
	}
}
