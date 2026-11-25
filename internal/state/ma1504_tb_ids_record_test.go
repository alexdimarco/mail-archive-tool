package state

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// covers: MA-1504, MA-734, R37
// The record's account identities (docs/design-per-mailbox-archives.md revision 3): a plaintext
// record keeps thunderbird_ids, each a store id beside an identity, both 32 lowercase hex characters,
// and reads them back as written (TBIDsOf, TBIDOf). Read back, anything else is left out, so a planted
// or damaged entry decides nothing: an identity in capitals, too short, holding a path or an address,
// a second entry for one store, entries past TBAccountsMax. An encrypted record keeps its allow-list:
// it never writes them, and TBIDsOf gives none for it. Positive twin first: a plaintext record with
// one entry reads it back.
func TestTheRecordsIdentitiesAreReadBackSafely(t *testing.T) {
	const store, id = "00112233445566778899aabbccddeeff", "ffeeddccbbaa99887766554433221100"
	dir := t.TempDir()
	good := []TBID{{StoreID: store, ID: id}}
	if err := WriteLastRun(dir, LastRun{Status: RunOK, Started: time.Now().UTC(), ThunderbirdIDs: good}); err != nil {
		t.Fatal(err)
	}
	lr, st, _ := ReadLastRun(dir)
	if st != LastRunPresent || !reflect.DeepEqual(lr.ThunderbirdIDs, good) || TBIDOf(lr, store) != id || TBIDOf(lr, "") != "" {
		t.Fatalf("positive twin: a plaintext record reads its identities back: %+v", lr.ThunderbirdIDs)
	}

	planted := []TBID{
		{StoreID: "aa112233445566778899aabbccddeeff", ID: "FFEEDDCCBBAA99887766554433221100"},
		{StoreID: "bb112233445566778899aabbccddeeff", ID: "ffee"},
		{StoreID: "cc112233445566778899aabbccddeeff", ID: "/home/qz/ImapMail/imap.gmail.com"},
		{StoreID: "dd112233445566778899aabbccddeeff", ID: "qza@gmail.example0000000000000000"},
		{StoreID: "../../x", ID: id},
		{StoreID: store, ID: id},
		{StoreID: store, ID: "0000000000000000000000000000000a"},
	}
	for i := 0; i < TBAccountsMax; i++ {
		planted = append(planted, TBID{StoreID: store, ID: id})
	}
	planted = append(planted, TBID{StoreID: "ee112233445566778899aabbccddeeff", ID: id}) // past the cap
	if err := WriteLastRun(dir, LastRun{Status: RunOK, Started: time.Now().UTC(), ThunderbirdIDs: planted}); err != nil {
		t.Fatal(err)
	}
	lr, _, _ = ReadLastRun(dir)
	if !reflect.DeepEqual(lr.ThunderbirdIDs, good) {
		t.Errorf("a planted record's identities: read back %+v, want only %+v", lr.ThunderbirdIDs, good)
	}

	enc := t.TempDir()
	if err := WriteLastRun(enc, LastRun{Status: RunFailed, Started: time.Now().UTC(), Encrypted: true, ThunderbirdIDs: good}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(enc, LastRunName)); bytes.Contains(data, []byte("thunderbird_ids")) || bytes.Contains(data, []byte(id)) {
		t.Errorf("an encrypted record gained the identities:\n%s", data)
	}
	if TBIDsOf(LastRun{Encrypted: true, ThunderbirdIDs: good}) != nil {
		t.Error("an encrypted record hands on no identities")
	}
}
