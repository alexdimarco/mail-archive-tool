package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// covers: MA-999, R36, R37
// delete-archive of a recorded encrypted archive when the secret store cannot be opened deletes
// the files but keeps the record, the remembered key and the row, and says so: forgetting the
// record without the store would leave a remembered key nothing points to (slice 4's adversarial
// pass, del-s4-A1-04).
func TestDeleteArchiveKeepsTheKeyWhenTheSecretStoreCannotBeOpened(t *testing.T) {
	e := newCLIDel(t)
	store := flowRecord(t)
	out := filepath.Join(t.TempDir(), "Sealed archive")
	mk := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "", "", keyAnswer}}
	u, err := createEncrypted(mk, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("make the archive: %v\n%s", err, mk.transcript())
	}
	u.Destroy()
	e.list(out)
	id := recordedAt(t, out)
	// Positive twin: recorded here, its key remembered (MA-994 forgets both when the store opens).
	assure.Reached(t, id, "this computer's record of the archive")
	if _, err := store.Get("archive:" + id); err != nil {
		t.Fatalf("the key is not remembered before the delete: %v", err)
	}

	secretStore = func() (vault.Store, error) { return nil, errors.New("the Secret Service is not running") }
	a := &scripted{isAttended: true, answers: []string{"Sealed archive"}}
	var res bytes.Buffer
	if err := deleteArchive(a, out, "", "", &res); err != nil {
		t.Fatalf("delete-archive with no secret store: %v\n%s\n%s", err, a.transcript(), res.String())
	}
	if plan := strings.Join(a.said, "\n"); !strings.Contains(plan, "secret store could not be opened (the Secret Service is not running)") {
		t.Fatalf("the plan does not say the secret store could not be opened:\n%s", plan)
	}
	r := res.String()
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatalf("the files were not deleted: %v\n%s", err, r)
	}
	for _, want := range []string{"Deleted.", "this computer still remembers its key", "At this terminal, Forget this encrypted archive here is: mailarchive forget-archive -out"} {
		if !strings.Contains(r, want) {
			t.Fatalf("the result does not say %q:\n%s", want, r)
		}
	}
	if strings.Contains(r, "This computer forgot the archive's key") {
		t.Fatalf("the result says the key was forgotten though it was kept:\n%s", r)
	}
	if recordedAt(t, out) != id {
		t.Fatal("this computer's record of the archive was forgotten without its key")
	}
	if _, err := store.Get("archive:" + id); err != nil {
		t.Fatalf("the remembered key is gone: %v", err)
	}
	if !e.listed(out) {
		t.Fatal("the row was removed though this computer still records the archive")
	}
}
