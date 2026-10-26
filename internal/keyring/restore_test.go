package keyring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

const restoredPassword = "a restored passphrase"

// madeWithFile makes an archive through Create and keeps its recovery file.
func madeWithFile(t *testing.T, s vault.Store) (out, rk string, file []byte, u *Unlocked) {
	t.Helper()
	out = filepath.Join(t.TempDir(), "archive")
	p, err := Create(out, testPassword, CreateOptions{Remember: s != nil, Store: s})
	if err != nil {
		t.Fatal(err)
	}
	rk = p.RecoveryKey()
	if file, err = p.RecoveryFile(); err != nil {
		t.Fatal(err)
	}
	if u, err = p.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(u.Destroy)
	return out, rk, file, u
}

func dropKeyFiles(t *testing.T, out string) {
	t.Helper()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		if err := os.Remove(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
}

// backedUp runs a keyed backup into u's archive, so it has a sealed record.
func backedUp(t *testing.T, u *Unlocked) {
	t.Helper()
	opts := app.Options{Inputs: []string{fixture}, Out: u.Out()}
	done, err := Apply(&opts, u)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, err := app.Run(context.Background(), opts, quiet, nil); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-792, R37
// A recovery file and its recovery key put back a recorded folder's lost key files
// (its id lends the proof when no backup made a sealed record yet), set the new
// password and a new recovery key, and keep the folder recorded; the recovery file
// opens nothing on its own.
func TestARecoveryFileRestoresARecordedArchive(t *testing.T) {
	freshRecord(t)
	out, rk, file, a := madeWithFile(t, nil)
	if strings.Contains(string(file), rk) {
		t.Fatal("the recovery file holds the recovery key")
	}
	dropKeyFiles(t, out)
	p, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	newRK := p.RecoveryKey()
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	u, err := Open(out, Password(restoredPassword), OpenOptions{})
	assure.Reached(t, u, "opened by the new password")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	if v, err := Open(out, RecoveryKey(newRK), OpenOptions{}); err != nil {
		t.Fatalf("the new recovery key: %v", err)
	} else {
		v.Destroy()
	}
	if recordedID(t, out) != a.ArchiveID().String() {
		t.Fatal("the folder is no longer recorded after the restore")
	}
}

// covers: MA-792, R37
// A restore is proven by the archive's sealed record at a folder this computer has
// not recorded; with no record there, or at a path a repair left unproven, it is
// refused (§9.4: the envelope's claim alone is never accepted, A34).
func TestARestoreNeedsTheSealedRecordWhereNothingRecordedIt(t *testing.T) {
	freshRecord(t)
	out, rk, file, a := madeWithFile(t, nil)
	id := a.ArchiveID().String()
	change(t, func(r *encrecord.Record) error { delete(r.Archives, id); return nil })
	dropKeyFiles(t, out)
	_, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if !errors.Is(err, vault.ErrNotProven) {
		t.Fatalf("an unrecorded folder with no sealed record: %v", err)
	}
	change(t, func(r *encrecord.Record) error { return r.Add(id, out, true, now()) })
	_, err = RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
	if !errors.Is(err, vault.ErrNotProven) {
		t.Fatalf("an unproven path with no sealed record: %v", err)
	}

	// With a sealed record, an unrecorded folder is proven by it.
	freshRecord(t)
	out, rk, file, b := madeWithFile(t, nil)
	backedUp(t, b)
	change(t, func(r *encrecord.Record) error { delete(r.Archives, b.ArchiveID().String()); return nil })
	dropKeyFiles(t, out)
	p, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatalf("an unrecorded folder proven by its sealed record: %v", err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	if recordedID(t, out) != b.ArchiveID().String() {
		t.Fatal("the restored folder was not recorded")
	}
}

// covers: MA-792, R37
// The key this computer remembers rebuilds the lost key files (§9.5, K15).
func TestTheRememberedKeyRebuildsAnArchivesKeyFiles(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, _, _ := madeWithFile(t, s)
	dropKeyFiles(t, out)
	p, err := RestoreFromRemembered(out, s, restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	u, err := Open(out, Password(restoredPassword), OpenOptions{})
	assure.Reached(t, u, "opened after the rebuild")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
}

// covers: MA-792, R37
// A restore is a write: refused while the record cannot be used, at a recorded
// folder that now holds other files, for a key-shaped password, and — unless the
// person confirms — for an archive they told MailArchive to forget; each writes
// nothing.
func TestARestoreIsRefusedWhereAWriteIs(t *testing.T) {
	dir := freshRecord(t)
	out, rk, file, a := madeWithFile(t, nil)
	dropKeyFiles(t, out)
	noKeyFile := func() bool { return !exists(filepath.Join(out, vault.FileName)) }

	os.WriteFile(filepath.Join(out, "index.html"), []byte("planted"), 0o600)
	_, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if !errors.Is(err, ErrNotTheArchive) || !noKeyFile() {
		t.Fatalf("a recorded folder holding other files: %v", err)
	}
	os.Remove(filepath.Join(out, "index.html"))

	if _, err := RestoreFromFile(out, file, RecoveryKey(rk), "mango umbrella umbrella hammer", RestoreOptions{}); !errors.Is(err, ErrPasswordLooksLikeKey) {
		t.Fatalf("a key-shaped password: %v", err)
	}

	id := a.ArchiveID().String()
	change(t, func(r *encrecord.Record) error {
		r.ForgetID(id)
		return r.Add(id, out, false, now())
	})
	change(t, func(r *encrecord.Record) error { r.Archives[id].Forgotten = true; return nil })
	p, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{ConfirmReRecord: func(string) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	err = p.Commit()
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("forget"), assure.NoSideEffect(noKeyFile))

	damageRecord(t, dir)
	_, err = RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if !errors.Is(err, encmark.ErrRecordUnusable) || !noKeyFile() {
		t.Fatalf("an unusable record: %v", err)
	}
}
