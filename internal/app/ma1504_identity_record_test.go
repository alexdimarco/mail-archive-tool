package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
	"mail-archive-tool/internal/vault"
)

// idUser is the user name the identity tests give the account on host: never its shown name.
func idUser(host string) string { return "qz-user-" + host + "@example.com" }

// idAcctFor is the fixture's account on host, numbered n, with its folder setting, its name and a
// user name (so it has an identity: docs/design-per-mailbox-archives.md revision 3).
func idAcctFor(n int, host, dirRel, name string) tbAcct {
	return tbAcct{n: n, host: host, typ: "imap", dirRel: dirRel, extra: map[string]string{"name": `"` + name + `"`, "userName": `"` + idUser(host) + `"`}}
}

// recordedID is the identity the record of the archive at out keeps for the folder in.
func recordedTBID(t *testing.T, out, in string) string {
	t.Helper()
	lr, _, _ := state.ReadLastRun(out)
	return autoplace.RecordedID(lr, in)
}

// covers: MA-1504, MA-734, R12, R18
// Revision 3 of docs/design-per-mailbox-archives.md: a plaintext record keeps, for each Thunderbird
// folder its run read, the identity of the account that owned it (thunderbird_ids: the store's id and
// a hash of the account's type, user name and server, read with the name), never the name or the user
// name itself. It is carried across a no-owner run as the name is (revision 2.2): the account removed
// with its folder kept, an ok backup of that one folder keeps the identity, again at the next; an
// auto-discovery backup never keeps one; a refused run's record carries it. An encrypted archive's
// record never holds it (MA-734). Positive twin first: an account with no user name has no identity,
// and its record is written as before, with no thunderbird_ids.
func TestTheRecordKeepsEachFoldersAccountIdentity(t *testing.T) {
	f := goneFixture(t, true, "imap.qza.example")
	a := f.stores["imap.qza.example"]
	name := goneAddress("imap.qza.example")
	prefs := filepath.Join(f.profile, "prefs.js")
	local := tbAcct{n: 2, host: "Local Folders", typ: "none", dirRel: "Mail/Local Folders"}

	// Positive twin: no user name, no identity, the record as before.
	plain := oneFolderArchive(t, a, name)
	if data, _ := os.ReadFile(filepath.Join(plain, state.LastRunName)); bytes.Contains(data, []byte("thunderbird_ids")) {
		t.Fatalf("positive twin: an account with no user name has no identity to keep:\n%s", data)
	}

	acct := idAcctFor(1, "imap.qza.example", "ImapMail/imap.qza.example", name)
	tbWrite(t, prefs, tbPrefsJS(acct, local))
	facts, err := thunderbird.ReadStoreFacts(f.profile, a)
	id := facts.Identity
	if err != nil || id == "" {
		t.Fatalf("the account's identity: %q %v", id, err)
	}
	out := oneFolderArchive(t, a, name)
	autoOut := tmpDir(t)
	runsOK(t, "an auto-discovery archive", autoOut, true, a)
	for _, o := range []string{out, autoOut} {
		lr, _, _ := state.ReadLastRun(o)
		if recordedTBID(t, o, a) != id || len(lr.ThunderbirdIDs) != 1 {
			t.Fatalf("an ok run keeps the folder's account identity: %+v, want %q", lr.ThunderbirdIDs, id)
		}
		if data, _ := os.ReadFile(filepath.Join(o, state.LastRunName)); bytes.Contains(data, []byte(idUser("imap.qza.example"))) {
			t.Errorf("the record holds the user name itself:\n%s", data)
		}
	}

	// The account removed, its folder kept: a one-folder archive keeps the identity, an auto-discovery
	// one does not.
	tbWrite(t, prefs, tbPrefsJS(local))
	for i := 1; i <= 2; i++ {
		runsOK(t, "a backup in the gap", out, false, a)
		if got := recordedTBID(t, out, a); got != id {
			t.Fatalf("gap backup %d: the one-folder archive's record must keep the identity: %q", i, got)
		}
	}
	runsOK(t, "an auto-discovery backup in the gap", autoOut, true, a)
	if got := recordedTBID(t, autoOut, a); got != "" {
		t.Errorf("an auto-discovery run must never keep an identity: %q", got)
	}

	// A refused run's record carries it.
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	if _, err, _ := goneRun(t, Options{Inputs: []string{a}, Out: out, Unattended: true}); err == nil {
		t.Fatal("the run of a gone folder must refuse")
	}
	if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunFailed || recordedTBID(t, out, a) != id {
		t.Errorf("a refused run's record carries the identity: %q %+v", lr.Status, lr.ThunderbirdIDs)
	}

	// Never beside an encrypted archive, whatever the record before it kept.
	enc := tmpDir(t)
	lr, _, _ := state.ReadLastRun(out)
	if err := state.WriteLastRun(enc, lr); err != nil {
		t.Fatal(err)
	}
	writeRefusedRun(Options{Out: enc, Key: &vault.Key{}}, &sourceUnreachableError{path: a})
	if data, err := os.ReadFile(filepath.Join(enc, state.LastRunName)); err != nil || bytes.Contains(data, []byte("thunderbird_ids")) || bytes.Contains(data, []byte(id)) {
		t.Errorf("an encrypted archive's record gained the identities: %v\n%s", err, data)
	}
}
