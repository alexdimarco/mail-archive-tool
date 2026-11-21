package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// movedOff puts a store for the account a at a new folder off the profile and returns the account
// moved there and the folder.
func movedOff(t *testing.T, a tbAcct, label string) (tbAcct, string) {
	t.Helper()
	away := filepath.Join(tmpDir(t), "QZUSB", label)
	tbStoreAt(t, away, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs(label, 2)})
	a.dirRel, a.dir = "", away
	return a, away
}

// named is an account with the name Thunderbird shows for it.
func named(a tbAcct, name string) tbAcct {
	a.extra = map[string]string{"name": `"` + name + `"`}
	return a
}

// stopsByName runs an unattended backup (a scheduled run; Run now builds the same unattended options,
// wizard.optsForJob) and asserts the refusal for in, moved to away, in the words of a move the record's
// name decided, with nothing backed up and the manifest unchanged; that the account's name is in the
// record the refusal wrote and in no log line or attention notice (MA-1288; the run history keeps the
// names an ok run's entries had, by design, MA-1284).
func stopsByName(t *testing.T, which string, inputs []string, out, in, away, name string) {
	t.Helper()
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	before, _ := os.ReadFile(mpath)
	res, err, logText := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out, Unattended: true})
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3
		}
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("input "+in+" does not exist", away, "Thunderbird still has the account that used it"),
		assure.Forbid(name),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(mpath)
			return bytes.Equal(before, after) && res.Stats.Exported == 0
		}))
	attention, _ := os.ReadFile(filepath.Join(out, state.AttentionName))
	for what, text := range map[string]string{"log": logText.String(), "attention notice": string(attention)} {
		if strings.Contains(text, name) {
			t.Errorf("%s: the %s holds the account's name (MA-1288)", which, what)
		}
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Status != state.RunFailed || autoplace.RecordedName(lr, in) != name {
		t.Errorf("%s: the refused record must keep the folder's account name: status %q, name %q", which, lr.Status, autoplace.RecordedName(lr, in))
	}
}

// covers: MA-1481, MA-1472, MA-1288, R12, R18
// The re-verify, round 3's RV3-01 (MEDIUM; the lead's call): the record a stopped backup writes
// keeps each Thunderbird folder's account name, carried from the previous record, so a moved account
// stops the first scheduled backup, the next one, and Run now after them, never skipped quietly by
// the server-name rule after one stop. The reviewer's three setups: a third account on the same
// server that has always kept its mail off the profile; two of three accounts on one server moved;
// an account whose server name was changed after Thunderbird made its folder. Positive twin first: in
// the owner's setup (three accounts on one server, all in the profile) a removed account is skipped
// on two scheduled backups in a row, named from the record both times.
func TestAMovedAccountStopsEveryRunNotOnlyTheFirst(t *testing.T) {
	const host = "imap.qzg.example"
	three := func(t *testing.T) (goneFix, []tbAcct, []string, string, string) {
		f := goneFixture(t, false)
		var accts []tbAcct
		var inputs []string
		for i, fd := range []string{"imap.qzg.example", "imap.qzg-1.example", "imap.qzg-2.example"} {
			dir := filepath.Join(f.profile, "ImapMail", fd)
			tbStoreAt(t, dir, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs(fd, 2)})
			accts = append(accts, named(tbAcct{n: i + 1, host: host, typ: "imap", dirRel: "ImapMail/" + fd}, "qz"+string(rune('a'+i))+"@qzg.example"))
			inputs = append(inputs, dir)
		}
		prefs := filepath.Join(f.profile, "prefs.js")
		tbWrite(t, prefs, tbPrefsJS(accts...))
		out := tmpDir(t)
		if _, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out}); err != nil {
			t.Fatal(err)
		}
		return f, accts, inputs, prefs, out
	}

	t.Run("positive twin: removed, skipped on both runs", func(t *testing.T) {
		_, accts, inputs, prefs, out := three(t)
		if err := os.RemoveAll(inputs[1]); err != nil {
			t.Fatal(err)
		}
		tbWrite(t, prefs, tbPrefsJS(accts[0], accts[2]))
		for run := 1; run <= 2; run++ {
			res, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out, Unattended: true})
			if err != nil || len(res.Skipped) != 1 || res.Skipped[0].Path != inputs[1] || res.Skipped[0].Name != "qzb@qzg.example" {
				t.Fatalf("run %d: a removed account is skipped, named from the record: %+v %v", run, res.Skipped, err)
			}
		}
		// An ok run with completeness entries of its own keeps the names there only: its record is
		// written as before, with no carried list beside them.
		if lr, _, _ := state.ReadLastRun(out); len(lr.Thunderbird) == 0 || len(lr.ThunderbirdNames) != 0 {
			t.Errorf("an ok run with its own entries carried names beside them: %d entries, %+v", len(lr.Thunderbird), lr.ThunderbirdNames)
		}
	})

	t.Run("a third account off the profile", func(t *testing.T) {
		f := goneFixture(t, false)
		a := filepath.Join(f.profile, "ImapMail", "imap.qzg.example")
		b := filepath.Join(f.profile, "ImapMail", "imap.qzg-1.example")
		tbStoreAt(t, a, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("qza", 2)})
		tbStoreAt(t, b, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("qzb", 2)})
		acctA := named(tbAcct{n: 1, host: host, typ: "imap", dirRel: "ImapMail/imap.qzg.example"}, "qza@qzg.example")
		acctB := named(tbAcct{n: 2, host: host, typ: "imap", dirRel: "ImapMail/imap.qzg-1.example"}, "qzb@qzg.example")
		acctC, _ := movedOff(t, named(tbAcct{n: 3, host: host, typ: "imap"}, "qzc@qzg.example"), "QZGmail3")
		prefs := filepath.Join(f.profile, "prefs.js")
		tbWrite(t, prefs, tbPrefsJS(acctA, acctB, acctC))
		inputs := []string{a, b}
		out := tmpDir(t)
		if _, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out}); err != nil {
			t.Fatal(err)
		}
		if lr, _, _ := state.ReadLastRun(out); len(lr.ThunderbirdNames) != 0 || autoplace.RecordedName(lr, b) != "qzb@qzg.example" {
			t.Fatalf("an ok run with its own entries keeps the names there, as before: %+v", lr.ThunderbirdNames)
		}
		if err := os.RemoveAll(b); err != nil {
			t.Fatal(err)
		}
		movedB, away := movedOff(t, acctB, "QZGmail2")
		tbWrite(t, prefs, tbPrefsJS(acctA, movedB, acctC))
		for _, which := range []string{"the first scheduled run", "the next scheduled run", "Run now"} {
			stopsByName(t, which, inputs, out, b, away, "qzb@qzg.example")
		}
	})

	t.Run("two of three moved", func(t *testing.T) {
		_, accts, inputs, prefs, out := three(t)
		for _, in := range inputs[1:] {
			if err := os.RemoveAll(in); err != nil {
				t.Fatal(err)
			}
		}
		mb, away := movedOff(t, accts[1], "QZGmail2")
		mc, _ := movedOff(t, accts[2], "QZGmail3")
		tbWrite(t, prefs, tbPrefsJS(accts[0], mb, mc))
		for _, which := range []string{"the first scheduled run", "the next scheduled run", "Run now"} {
			stopsByName(t, which, inputs, out, inputs[1], away, "qzb@qzg.example")
		}
	})

	t.Run("its server name changed", func(t *testing.T) {
		f := goneFixture(t, false, "imap.qza.example")
		a := f.stores["imap.qza.example"]
		b := filepath.Join(f.profile, "ImapMail", "imap.qzold.example")
		tbStoreAt(t, b, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("qzb", 2)})
		acctA := named(tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example"}, goneAddress("imap.qza.example"))
		acctB := named(tbAcct{n: 2, host: "imap.qznew.example", typ: "imap", dirRel: "ImapMail/imap.qzold.example"}, "qzb@qz.example")
		prefs := filepath.Join(f.profile, "prefs.js")
		tbWrite(t, prefs, tbPrefsJS(acctA, acctB))
		inputs := []string{a, b}
		out := tmpDir(t)
		if _, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out}); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(b); err != nil {
			t.Fatal(err)
		}
		mb, away := movedOff(t, acctB, "QZWork")
		tbWrite(t, prefs, tbPrefsJS(acctA, mb))
		for _, which := range []string{"the first scheduled run", "the next scheduled run", "Run now"} {
			stopsByName(t, which, inputs, out, b, away, "qzb@qz.example")
		}
	})
}

// covers: MA-1481, MA-734, R18, R37
// Every record a plaintext run writes keeps the names (RV3-01): the running record, a refused run's,
// a failed run's, a cancelled run's and an ok run's that assessed no account of its own carry the
// previous record's names (from its completeness entries, or from the names it carried); an ok run
// with entries of its own keeps them there only, as before. An encrypted archive's records keep none
// (the owner's Q3, Q5 and Q6): its running record carries none, and its writer drops any it is handed.
// Positive twin first: the names of an ok record are read back by store id.
func TestEveryPlaintextRecordCarriesTheFolderNames(t *testing.T) {
	out := tmpDir(t)
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := state.LastRun{Status: state.RunOK, Thunderbird: []state.TBAccount{
		{Account: "imap.qzg-1.example", Name: "qzb@qzg.example", StoreID: "00112233445566778899aabbccddeeff", Verdict: "unknown"},
		{Account: "imap.qzg-2.example", StoreID: "ffeeddccbbaa99887766554433221100", Verdict: "unknown"},
	}}
	if err := state.WriteLastRun(out, ok); err != nil {
		t.Fatal(err)
	}
	want := []state.TBName{{StoreID: "00112233445566778899aabbccddeeff", Name: "qzb@qzg.example"}}
	has := func(t *testing.T, what string) {
		t.Helper()
		lr, st, _ := state.ReadLastRun(out)
		if st != state.LastRunPresent || len(lr.ThunderbirdNames) != 1 || lr.ThunderbirdNames[0] != want[0] {
			t.Errorf("%s: the record must carry the names %+v: %q %+v", what, want, lr.Status, lr.ThunderbirdNames)
		}
	}
	if got := state.TBNameOf(ok, want[0].StoreID); got != want[0].Name {
		t.Fatalf("positive twin: an ok record's name by store id: %q", got)
	}
	opts := Options{Out: out}
	lr := beginRun(opts)
	has(t, "the running record")
	writeRefusedRun(opts, errors.New("input qz does not exist"))
	has(t, "a refused run's record")
	failed := errors.New("qz failed")
	recordRun(opts, beginRun(opts), &Result{}, &failed, nil)
	has(t, "a failed run's record")
	cancelled := error(context.Canceled)
	recordRun(opts, beginRun(opts), &Result{}, &cancelled, nil)
	has(t, "a cancelled run's record")
	var none error
	recordRun(opts, lr, &Result{}, &none, nil)
	has(t, "an ok run's record that assessed no account")

	// An encrypted archive's records keep no names.
	if enc := newRunRecord(Options{Out: out, Key: &vault.Key{}}); len(enc.ThunderbirdNames) != 0 {
		t.Errorf("an encrypted archive's running record carries names: %+v", enc.ThunderbirdNames)
	}
	encOut := tmpDir(t)
	if err := state.WriteLastRun(encOut, state.LastRun{Status: state.RunFailed, Encrypted: true, ThunderbirdNames: want}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(encOut, state.LastRunName)); bytes.Contains(b, []byte("thunderbird_names")) || bytes.Contains(b, []byte("qzb@")) {
		t.Errorf("an encrypted record was written with names:\n%s", b)
	}
}

// covers: MA-1481, R12
// The re-verify, round 3's RV3-02 (LOW; the reviewer's choice 2, Q7's direction): an account moved
// and renamed at once (both are on one settings page in Thunderbird) matches no live account by the
// record's name, so the server-name rule is asked, and it stops the backup, in its own words, as
// round 2's rule did. Positive twin first: moved with its name unchanged stops, by name.
func TestAMovedAndRenamedAccountStops(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	prefs := filepath.Join(f.profile, "prefs.js")
	acctA := named(tbAcct{n: 1, host: "imap.qza.example", typ: "imap", dirRel: "ImapMail/imap.qza.example"}, goneAddress("imap.qza.example"))
	acctB := named(tbAcct{n: 2, host: "imap.qzb.example", typ: "imap", dirRel: "ImapMail/imap.qzb.example"}, goneAddress("imap.qzb.example"))
	inputs := []string{a, b}
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	mb, away := movedOff(t, acctB, "QZWork")
	tbWrite(t, prefs, tbPrefsJS(acctA, mb))
	stopsByName(t, "positive twin: moved, its name unchanged", inputs, out, b, away, goneAddress("imap.qzb.example"))

	tbWrite(t, prefs, tbPrefsJS(acctA, named(mb, "QZ Work (archive drive)")))
	res, err, _ := goneRun(t, Options{Inputs: inputs, Auto: true, Out: out, Unattended: true})
	code, msg := 0, ""
	var sue *sourceUnreachableError
	if err != nil {
		code, msg = 1, err.Error()
		if !errors.As(err, &sue) {
			code = 3
		}
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("input "+b+" does not exist", away, "a Thunderbird account on the same mail server"),
		assure.Forbid("QZ Work (archive drive)"),
		assure.NoSideEffect(func() bool { return res.Stats.Exported == 0 && len(res.Skipped) == 0 }))
}
