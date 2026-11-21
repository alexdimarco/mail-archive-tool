package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// A combined ("All of them") archive keeps going when one of its mailboxes is gone
// (docs/design-1.7.5-fixes.md §1.2, MA-1471): real temp Thunderbird profiles under a home's
// .thunderbird, where auto-discovery looks, read by real runs.

// goneFix is a home whose Thunderbird profile holds an IMAP account folder per host and, when
// asked, Local Folders; each store holds mail.
type goneFix struct {
	home, profile, local string
	stores               map[string]string // host → its account folder
}

// goneAddress is the account name the fixture's prefs.js gives the account on host.
func goneAddress(host string) string {
	return "qz-" + strings.ReplaceAll(host, ".", "-") + "@example.com"
}

// goneProfileAt writes a profile named name under root holding an IMAP account folder per host,
// named in prefs.js by goneAddress, and Local Folders when local is set.
func goneProfileAt(t *testing.T, root, name string, local bool, hosts ...string) goneFix {
	t.Helper()
	f := goneFix{profile: filepath.Join(root, name), stores: map[string]string{}}
	var accts []tbAcct
	for i, h := range hosts {
		accts = append(accts, tbAcct{n: i + 1, host: h, typ: "imap", dirRel: "ImapMail/" + h, extra: map[string]string{"name": `"` + goneAddress(h) + `"`}})
		dir := filepath.Join(f.profile, "ImapMail", h)
		tbStoreAt(t, dir, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs(h, 2)})
		f.stores[h] = dir
	}
	if local {
		accts = append(accts, tbAcct{n: len(hosts) + 1, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"})
		f.local = filepath.Join(f.profile, "Mail", "Local Folders")
		tbStoreAt(t, f.local, tbFolder{path: "Inbox", msgs: tbMsgs("local", 1)})
	}
	tbWrite(t, filepath.Join(f.profile, "prefs.js"), tbPrefsJS(accts...))
	tbWrite(t, filepath.Join(f.profile, "compatibility.ini"), "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\nLastOSABI=Darwin_aarch64-gcc3\n")
	return f
}

// goneFixture is goneProfileAt in a new home's .thunderbird, with HOME and USERPROFILE pointing at
// that home, so auto-discovery looks there.
func goneFixture(t *testing.T, local bool, hosts ...string) goneFix {
	t.Helper()
	home := tmpDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	f := goneProfileAt(t, filepath.Join(home, ".thunderbird"), "abcd1234.default-release", local, hosts...)
	f.home = home
	return f
}

// goneRun runs opts into its archive, logging into the returned buffer.
func goneRun(t *testing.T, opts Options) (Result, error, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	opts.Mode = export.Incremental
	res, err := Run(context.Background(), opts, log.New(&buf, "", 0), nil)
	return res, err, &buf
}

// covers: MA-1471, MA-1288, R1, R18
// Positive twin of the rule: an "All of them" archive (its listed mailboxes plus auto-discovery)
// with one account folder gone and the profile's ImapMail folder still there skips that folder
// while another mailbox remains. The run ends ok, backs up the others, says the skip in its log
// straight after discovery ("Not found, so skipped: PATH. …": the log is a scheduled run's log
// file too, which never holds an account's name, MA-1288), and hands it back in Result.Skipped
// with NAME as Thunderbird names the account, read from the profile's settings for the folder that
// is gone. A refusal it no longer is: the source-loss streak resets.
func TestAGoneMailboxIsSkippedWhileAnotherRemains(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatalf("positive twin: both mailboxes there: %v", err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	res, err, logged := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true})
	if err != nil {
		t.Fatalf("a run with one account folder gone must skip it, not refuse: %v", err)
	}
	skipped := assure.Reached(t, res.Skipped, "the skipped mailboxes")
	if len(skipped) != 1 || skipped[0].Path != b || skipped[0].Name != goneAddress("imap.qzb.example") {
		t.Fatalf("Result.Skipped = %+v, want the gone folder %s named %s", skipped, b, goneAddress("imap.qzb.example"))
	}
	want := "Not found, so skipped: " + b + ". It is no longer on this computer; the mail already saved from it stays in the archive."
	if !strings.Contains(logged.String(), want) {
		t.Fatalf("the run log must say the skip: want %q in\n%s", want, logged)
	}
	// The run's log is a scheduled run's log file too: it never holds the account's name (MA-1288).
	if strings.Contains(logged.String(), goneAddress("imap.qzb.example")) {
		t.Fatalf("the run log names the account:\n%s", logged)
	}
	if strings.Contains(logged.String(), "were backed up") || strings.Contains(logged.String(), "are still backed up") {
		t.Fatalf("at discovery nothing is backed up yet; the log must not say so:\n%s", logged)
	}
	if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunOK {
		t.Fatalf("the run's record: %+v", lr)
	}
	if s, _ := state.ReadSourceStreak(out); s.ConsecutiveFails != 0 {
		t.Fatalf("a run that skipped a gone mailbox reached its source: the streak must reset, got %+v", s)
	}
	// The other mailbox's mail was read: a new message in it is exported.
	tbStoreAt(t, a, tbFolder{path: "INBOX", listed: 3, msgs: tbMsgs("imap.qza.example", 3)})
	if res, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil || res.Stats.Exported != 1 {
		t.Fatalf("the remaining mailbox must still be backed up: exported %d, %v", res.Stats.Exported, err)
	}
}

// covers: MA-1471, R1
// The owner's ruled case (Q4): one mail account plus Thunderbird's Local Folders. With that
// account's folder gone and ImapMail still there, Local Folders alone meets rule (d): the backup
// keeps succeeding on it. A folder with no account naming it says only its path.
func TestOneAccountPlusLocalFoldersKeepsGoing(t *testing.T) {
	f := goneFixture(t, true, "imap.qzone.example")
	acct := f.stores["imap.qzone.example"]
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{acct, f.local}, Auto: true, Out: out}); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if err := os.RemoveAll(acct); err != nil {
		t.Fatal(err)
	}
	// The account removed on purpose in Thunderbird: its settings no longer name the folder.
	tbWrite(t, filepath.Join(f.profile, "prefs.js"), tbPrefsJS(tbAcct{n: 2, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}))
	res, err, logged := goneRun(t, Options{Inputs: []string{acct, f.local}, Auto: true, Out: out, Unattended: true})
	if err != nil {
		t.Fatalf("Local Folders remains, so the run must skip the gone account: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != acct || res.Skipped[0].Name != "" {
		t.Fatalf("Result.Skipped = %+v, want %s with no name (its account is gone from the settings)", res.Skipped, acct)
	}
	if want := "Not found, so skipped: " + acct + ". It is no longer on this computer"; !strings.Contains(logged.String(), want) {
		t.Fatalf("a folder no account names is said by its path: want %q in\n%s", want, logged)
	}
}

// refusedGone runs opts unattended into out, which already holds an archive, and asserts the
// whole refusal: the typed source-unreachable error naming named, in its own words; nothing read
// (the manifest unchanged, nothing exported); the run recorded failed with those words; and the
// source-loss streak moved.
func refusedGone(t *testing.T, opts Options, named string) {
	t.Helper()
	opts.Unattended = true
	before, _ := os.ReadFile(filepath.Join(opts.Out, ".mailarchive-manifest.json"))
	streak, _ := state.ReadSourceStreak(opts.Out)
	res, err, _ := goneRun(t, opts)
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
	}
	if err != nil && !errors.As(err, &sue) {
		code = 3 // not the typed refusal
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(autoplace.GoneText(named)),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(opts.Out, ".mailarchive-manifest.json"))
			lr, _, _ := state.ReadLastRun(opts.Out)
			s, _ := state.ReadSourceStreak(opts.Out)
			return bytes.Equal(before, after) && res.Stats.Exported == 0 && len(res.Skipped) == 0 &&
				lr.Status == state.RunFailed && lr.Error == autoplace.GoneText(named) &&
				s.ConsecutiveFails == streak.ConsecutiveFails+1
		}))
}

// covers: MA-1471, MA-103, MA-688, R12, R18
// Each case the rule does not skip refuses exactly as 1.7.4 did, naming the input: a link whose
// target is gone; an input whose group folder is gone (a whole profile), whatever else remains; a
// profile reached through a link below the place's root; an input outside the places auto-discovery
// looks; on macOS and Linux an input spelled in another case; nothing remaining after the skips;
// and any gone input without auto-discovery. Positive twin first (TestAGoneMailboxIsSkipped…).
func TestAGoneMailboxTheRuleDoesNotSkipRefusesAsBefore(t *testing.T) {
	newArchive := func(t *testing.T, inputs ...string) string {
		t.Helper()
		out := tmpDir(t)
		if _, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out}); err != nil {
			t.Fatalf("positive twin: %v", err)
		}
		return out
	}

	t.Run("a link whose target is gone", func(t *testing.T) {
		f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
		a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
		out := newArchive(t, a, b)
		os.RemoveAll(b)
		if err := os.Symlink(filepath.Join(tmpDir(t), "gone-target"), b); err != nil {
			t.Skipf("this system cannot make a link here: %v", err)
		}
		refusedGone(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}, b)
	})

	t.Run("a whole profile gone", func(t *testing.T) {
		f := goneFixture(t, false, "imap.qza.example")
		g := goneProfileAt(t, filepath.Join(f.home, ".thunderbird"), "efgh5678.second", false, "imap.qzc.example")
		a, c := f.stores["imap.qza.example"], g.stores["imap.qzc.example"]
		out := newArchive(t, a, c)
		os.RemoveAll(g.profile)
		refusedGone(t, Options{Inputs: []string{a, c}, Auto: true, Out: out}, c)
	})

	t.Run("the ImapMail folder gone", func(t *testing.T) {
		f := goneFixture(t, true, "imap.qza.example")
		a := f.stores["imap.qza.example"]
		out := newArchive(t, a, f.local)
		os.RemoveAll(filepath.Dir(a))
		refusedGone(t, Options{Inputs: []string{a, f.local}, Auto: true, Out: out}, a)
	})

	t.Run("a profile reached through a link below the root", func(t *testing.T) {
		home := tmpDir(t)
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		elsewhere := goneProfileAt(t, tmpDir(t), "real.profile", false, "imap.qza.example", "imap.qzb.example")
		linked := filepath.Join(home, ".thunderbird", "abcd1234.default-release")
		if err := os.MkdirAll(filepath.Dir(linked), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere.profile, linked); err != nil {
			t.Skipf("this system cannot make a link here: %v", err)
		}
		a, b := filepath.Join(linked, "ImapMail", "imap.qza.example"), filepath.Join(linked, "ImapMail", "imap.qzb.example")
		out := newArchive(t, a, b)
		os.RemoveAll(elsewhere.stores["imap.qzb.example"])
		refusedGone(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}, b)
	})

	t.Run("an input outside the places", func(t *testing.T) {
		f := goneFixture(t, false, "imap.qza.example")
		a := f.stores["imap.qza.example"]
		usb := filepath.Join(tmpDir(t), "USB", "Inbox")
		maildirMessages(t, usb, 2)
		out := newArchive(t, a, usb)
		os.RemoveAll(usb)
		refusedGone(t, Options{Inputs: []string{a, usb}, Auto: true, Out: out}, usb)
	})

	t.Run("an input spelled in another case", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows folds case: there the same spelling rule matches")
		}
		f := goneFixture(t, false, "imap.qza.example")
		a := f.stores["imap.qza.example"]
		out := newArchive(t, a)
		other := filepath.Join(f.home, ".Thunderbird", "abcd1234.default-release", "ImapMail", "imap.qzb.example")
		if _, err := os.Lstat(other); err == nil {
			t.Skip("this volume folds case: the other spelling is there")
		}
		refusedGone(t, Options{Inputs: []string{a, other}, Auto: true, Out: out}, other)
	})

	t.Run("nothing remaining", func(t *testing.T) {
		f := goneFixture(t, false, "imap.qzb.example")
		b := f.stores["imap.qzb.example"]
		out := newArchive(t, b)
		os.RemoveAll(b)
		refusedGone(t, Options{Inputs: []string{b}, Auto: true, Out: out}, b)
	})

	t.Run("without auto-discovery", func(t *testing.T) {
		f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
		a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
		out := tmpDir(t)
		if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Out: out}); err != nil {
			t.Fatalf("positive twin: %v", err)
		}
		os.RemoveAll(b)
		refusedGone(t, Options{Inputs: []string{a, b}, Out: out}, b)
	})
}
