package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// movedWords are the words a moved account's refusal must hold: the old folder, the new one, that
// Thunderbird still lists the account, and both ways to back its mail up again.
func movedWords(old, moved string) []string {
	return []string{"input " + old + " does not exist", "Thunderbird still lists its account", moved,
		"Local Directory", "Make a new archive…", "Thunderbird / mbox mail folder"}
}

// stoppedOn174 plants what 1.7.4 left for an "All of them" archive at out that it stopped on the
// gone input b: a failed -auto record refused on b in the gone words, and a loud source streak.
func stoppedOn174(t *testing.T, out string, inputs []string, b string) {
	t.Helper()
	t0 := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: t0, LastUnreachable: t0.Add(state.SurfaceAfter), LastKind: "local-source-unreachable"}); err != nil {
		t.Fatal(err)
	}
	job := []string{"-out", out, "-mode", "incremental", "-auto"}
	for _, in := range inputs {
		job = append(job, "-input", in)
	}
	at := t0.Add(state.SurfaceAfter)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Started: at, Finished: at, Error: autoplace.GoneText(b), Job: job}); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1481, MA-1471, MA-1479, R12, R18
// A Thunderbird account moved with its Local Directory setting (not removed) stops the backup, as
// 1.7.4 did (the 1.7.5 adversarial pass's INS-02; the owner, 2026-11-20, Q7: "Stop the backup
// (Recommended)"): its old folder is gone, but Thunderbird's settings still give it to a live
// account (by the folder's hostname) whose folder setting names another place, so the run refuses
// with the typed sourceUnreachableError, in words that say the account was moved, where to, and how
// to back that mail up; nothing is read, the run is recorded failed in those words, and the source
// streak moves. The Repair helper agrees: it says no for an archive 1.7.4 stopped on that folder.
// Positive twins first (Q4): the account removed from the list is skipped; an account whose folder
// setting still names the folder, deleted by hand, is skipped; and the helper says yes for the
// removed account.
func TestAnAccountMovedInThunderbirdStopsTheBackup(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example", extra: map[string]string{"name": `"` + goneAddress("imap.qza.example") + `"`}}
	acctB := tbAcct{n: 2, host: "imap.qzb.example", typ: "imap", dirRel: "ImapMail/imap.qzb.example", extra: map[string]string{"name": `"` + goneAddress("imap.qzb.example") + `"`}}
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatalf("the archive: %v", err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}

	// Positive twin 1: the folder deleted by hand, its account's folder setting unchanged (Q4).
	if _, skipped, err := DiscoverInputs([]string{a, b}, true); err != nil || len(skipped) != 1 || skipped[0] != b {
		t.Fatalf("positive twin: an account whose folder setting is unchanged, its folder deleted by hand, is skipped: %v %v", skipped, err)
	}
	// Positive twin 3: the account listed with no folder setting at all: nothing says it moved.
	bare := acctB
	bare.dirRel = ""
	tbWrite(t, prefs, tbPrefsJS(acctA, bare))
	if _, skipped, err := DiscoverInputs([]string{a, b}, true); err != nil || len(skipped) != 1 || skipped[0] != b {
		t.Fatalf("positive twin: an account with no folder setting is not called moved: %v %v", skipped, err)
	}
	// Positive twin 2: the account removed from Thunderbird's list.
	tbWrite(t, prefs, tbPrefsJS(acctA))
	if _, skipped, err := DiscoverInputs([]string{a, b}, true); err != nil || len(skipped) != 1 || skipped[0] != b {
		t.Fatalf("positive twin: an account removed from Thunderbird is skipped: %v %v", skipped, err)
	}
	stopped := tmpDir(t)
	stoppedOn174(t, stopped, []string{a, b}, b)
	if !health.GoneMailboxRepairable(stopped) {
		t.Fatal("positive twin: the helper says Repair works for a removed account")
	}

	// The account moved: Thunderbird still lists it, its Local Directory now on a USB drive.
	usb := filepath.Join(tmpDir(t), "QZUSB", "imap.qzb.example")
	tbStoreAt(t, usb, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzb.example", 2)})
	moved := acctB
	moved.dirRel, moved.dir = "", usb
	tbWrite(t, prefs, tbPrefsJS(acctA, moved))

	if autoplace.GoneSkippable(b) {
		t.Error("GoneSkippable says a moved account's old folder may be skipped")
	}
	if health.GoneMailboxRepairable(stopped) {
		t.Error("the helper says Repair works for a moved account, where the run refuses")
	}
	before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
	streak, _ := state.ReadSourceStreak(out)
	res, err, logged := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true})
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3 // not the typed refusal: the streak would not move
		}
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(movedWords(b, usb)...),
		assure.Forbid(goneAddress("imap.qzb.example")), // MA-1288: the refusal reaches log files
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-manifest.json"))
			lr, _, _ := state.ReadLastRun(out)
			s, _ := state.ReadSourceStreak(out)
			return bytes.Equal(before, after) && res.Stats.Exported == 0 && len(res.Skipped) == 0 &&
				lr.Status == state.RunFailed && lr.Error == msg &&
				s.ConsecutiveFails == streak.ConsecutiveFails+1 && s.LastKind == "local-source-unreachable"
		}))
	if strings.Contains(logged.String(), goneAddress("imap.qzb.example")) {
		t.Errorf("the run's log names the moved account (MA-1288):\n%s", logged)
	}
}
