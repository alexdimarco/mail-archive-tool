package keyring

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/vault"
)

// The adversarial pass over the P3-8 fixes, group B (keys and words).

// covers: MA-854, R37
// A remembered key the store holds but refuses to read — its file readable by other
// users — is held, not "nothing to forget", and ForgetKey still deletes it (the P3-8
// pass, F1). Positive twin: a key file only its owner reads is held and readable.
func TestAKeyTheStoreWillNotReadIsStillForgotten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the file store's mode-bit refusal is not how Windows guards a secret file")
	}
	freshRecord(t)
	dir := t.TempDir()
	s := graphconfig.NewFileSecretStore(dir)
	out, _, a := made(t, s)
	held, unreadable, err := KeyRemembered(out, s)
	if !held || unreadable != nil || err != nil {
		t.Fatalf("positive twin: an owner-only key file is held and readable: %v %v %v", held, unreadable, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.secret"))
	if len(files) != 1 {
		t.Fatalf("want one stored key file, got %v", files)
	}
	if err := os.Chmod(files[0], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, gerr := s.Get(vault.Account(a.ArchiveID())); gerr == nil || errors.Is(gerr, os.ErrNotExist) {
		t.Fatalf("the store should refuse a key file others can read, not call it absent: %v", gerr)
	}
	held, unreadable, err = KeyRemembered(out, s)
	assure.Reached(t, held && unreadable != nil && err == nil, "a key the store will not read is held, with why")
	// Forgetting the whole archive offers that key too (F1's twin in PlanForget).
	if p, perr := PlanForget(out, s); perr != nil || !p.Remembered {
		t.Fatalf("the forget plan offers the unreadable key: %v %+v", perr, p)
	}
	if _, err := ForgetKey(out, s); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, !exists(files[0]), "the unreadable key file is deleted by forgetting")
}

// covers: MA-854, R37
// RememberedForRestore looks the remembered key up by the same archive id
// RestoreFromRemembered uses (the P3-8 pass, ORDER-1): an unproven record entry
// with both key files gone, and an unrecorded folder whose damaged key files still
// name the archive, both have a remembered key the restore can use, and the restore
// is prepared. Positive twin: a plain folder has none.
func TestTheRestoresOwnLookupFindsTheRememberedKey(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	plain := t.TempDir()
	if ok, err := RememberedForRestore(plain, s); ok || err != nil {
		t.Fatalf("positive twin: a plain folder has no remembered key: %v %v", ok, err)
	}

	// (a) An unproven record entry (as repair-record leaves it), key files gone.
	out, _, a := made(t, s)
	backedUp(t, a)
	id := a.ArchiveID().String()
	change(t, func(r *encrecord.Record) error { delete(r.Archives, id); return r.Add(id, out, true, now()) })
	dropKeyFiles(t, out)
	ok, err := RememberedForRestore(out, s)
	assure.Reached(t, ok && err == nil, "an unproven entry's id finds the remembered key")
	p, err := RestoreFromRemembered(out, s, restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatalf("the restore it answers for: %v", err)
	}
	p.Discard()

	// (b) Not recorded here, both key files damaged but naming the archive.
	freshRecord(t)
	out2, _, a2 := made(t, s)
	backedUp(t, a2)
	freshRecord(t)
	named := []byte(`{"archive_id":"` + a2.ArchiveID().String() + `","entries":"damaged"}`)
	for _, name := range []string{vault.FileName, vault.BackupName} {
		if err := os.WriteFile(filepath.Join(out2, name), named, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ok, err = RememberedForRestore(out2, s)
	assure.Reached(t, ok && err == nil, "damaged key files' claimed id finds the remembered key")
	p, err = RestoreFromRemembered(out2, s, restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatalf("the restore it answers for: %v", err)
	}
	p.Discard()
}
