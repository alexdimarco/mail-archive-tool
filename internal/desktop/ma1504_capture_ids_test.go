package desktop

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1504, R18
// A scheduled Microsoft 365 capture refused at start (recordStartFailure) keeps the Thunderbird
// folders' account identities of the plaintext record it replaces (docs/design-per-mailbox-archives.md
// revision 3: carried as the names are, RV4-01), so a later Thunderbird backup of the same folder is
// still judged by who its account is. A record that replaced an encrypted one has none. Positive twin
// first: the planted record keeps the identity.
func TestACaptureRefusedAtStartKeepsTheIdentities(t *testing.T) {
	out := t.TempDir()
	ids := []state.TBID{{StoreID: "00112233445566778899aabbccddeeff", ID: "ffeeddccbbaa99887766554433221100"}}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: time.Now().UTC(), ThunderbirdIDs: ids}); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := state.ReadLastRun(out); !reflect.DeepEqual(lr.ThunderbirdIDs, ids) {
		t.Fatalf("positive twin: the planted record keeps the identity: %+v", lr.ThunderbirdIDs)
	}
	recordStartFailure(out, errors.New(notConfiguredMsg))
	if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunFailed || !reflect.DeepEqual(lr.ThunderbirdIDs, ids) {
		t.Errorf("recordStartFailure must keep the identities: %q %+v", lr.Status, lr.ThunderbirdIDs)
	}

	enc := t.TempDir()
	if err := state.WriteLastRun(enc, state.LastRun{Status: state.RunOK, Encrypted: true, ThunderbirdIDs: ids}); err != nil {
		t.Fatal(err)
	}
	recordStartFailure(enc, errors.New(notConfiguredMsg))
	if b, _ := os.ReadFile(filepath.Join(enc, state.LastRunName)); bytes.Contains(b, []byte("thunderbird_ids")) {
		t.Errorf("a record that replaced an encrypted one has identities:\n%s", b)
	}
}
