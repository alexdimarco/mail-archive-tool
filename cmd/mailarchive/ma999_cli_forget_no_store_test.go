package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// covers: MA-999, R37
// forget-archive of an archive's last recorded folder when the secret store cannot be opened
// keeps this computer's record of the archive and says so, as delete-archive does: forgetting the
// whole archive then would leave a remembered key that nothing points to (del-s4 re-verify
// residual 4). Answering No still forgets only the folder, the record of the archive and its key
// kept.
func TestForgetArchiveKeepsTheRecordWhenTheSecretStoreCannotBeOpened(t *testing.T) {
	store := flowRecord(t)
	newArchive := func(name string) (string, string) {
		t.Helper()
		out := filepath.Join(t.TempDir(), name)
		mk := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "", "", keyAnswer}}
		u, err := createEncrypted(mk, out, noKeyFlags(), false, nil)
		if err != nil {
			t.Fatalf("make the archive: %v\n%s", err, mk.transcript())
		}
		u.Destroy()
		return out, recordedAt(t, out)
	}
	// archiveRecorded: this computer records the archive (a forgotten one is kept as a
	// tombstone, Forgotten).
	archiveRecorded := func(id string) bool {
		rec, err := encrecord.Load()
		return err == nil && rec.Archives[id] != nil && !rec.Archives[id].Forgotten
	}
	keyKept := func(id string) bool {
		_, err := store.Get("archive:" + id)
		return err == nil
	}

	// Positive twin: with the store, the whole archive and its remembered key are forgotten.
	twin, twinID := newArchive("Twin")
	assure.Reached(t, twinID != "" && keyKept(twinID), "the archive recorded and its key remembered")
	a := &scripted{isAttended: true, answers: []string{"y", "y", "y"}}
	if err := forgetArchive(a, twin); err != nil {
		t.Fatalf("forget-archive with the store: %v\n%s", err, a.transcript())
	}
	if recordedAt(t, twin) != "" || archiveRecorded(twinID) || keyKept(twinID) {
		t.Fatal("with the store, the whole archive and its key were not forgotten")
	}

	// No store: forgetting the whole archive is refused, the record and the key kept, said so.
	out, id := newArchive("Sealed")
	secretStore = func() (vault.Store, error) { return nil, errors.New("the Secret Service is not running") }
	a = &scripted{isAttended: true, answers: []string{"y", "y"}}
	err := forgetArchive(a, out)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("secret store could not be opened (the Secret Service is not running)", "keeps its record of the archive "+id, "nothing was changed", "Run forget-archive again once the secret store opens", "answer No to forget only this folder"),
		assure.NoSideEffect(func() bool { return recordedAt(t, out) == id && archiveRecorded(id) && keyKept(id) }))
	if !strings.Contains(a.transcript(), "the whole archive cannot be forgotten now") {
		t.Fatalf("the question is not preceded by why the whole archive cannot be forgotten:\n%s", a.transcript())
	}

	// Answering No: only the folder is forgotten; the record of the archive and its key stay.
	a = &scripted{isAttended: true, answers: []string{"y", "n"}}
	if err := forgetArchive(a, out); err != nil {
		t.Fatalf("forget-archive of the folder only, with no store: %v\n%s", err, a.transcript())
	}
	if recordedAt(t, out) != "" || !archiveRecorded(id) || !keyKept(id) {
		t.Fatal("No to the whole archive did not forget only the folder")
	}
}
