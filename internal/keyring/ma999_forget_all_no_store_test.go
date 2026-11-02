package keyring

import (
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// covers: MA-999, R37
// Forget every encrypted archive with no secret store (nil: the store could not be opened) forgets
// nothing: no one can tell which keys are remembered, and emptying the record and the lost list
// would leave every remembered key with nothing pointing to it (the delete residuals' key-use
// pass, finding 1). The record, the lost list and the remembered keys are all kept. Positive twin
// first: with the store, the record, the lost list and the keys all go.
func TestForgetAllWithNoSecretStoreForgetsNothing(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	remembered := func() (string, vault.ID) {
		t.Helper()
		out, _, w := made(t, s)
		id := w.ArchiveID()
		if err := Remember(w, s); err != nil {
			t.Fatal(err)
		}
		w.Destroy()
		return out, id
	}
	keyKept := func(id vault.ID) bool { _, err := s.Get(vault.Account(id)); return err == nil }

	twin, twinID := remembered()
	if !HasRemembered(twin, s) {
		t.Fatal("fixture: the key is remembered")
	}
	if n, err := ForgetAll(s); err != nil || n != 1 {
		t.Fatalf("positive twin: forget-all with the store: %d, %v", n, err)
	}
	if rec, _ := encrecord.Load(); len(rec.Archives) != 0 || keyKept(twinID) {
		t.Fatal("positive twin: with the store, the record and the key are forgotten")
	}

	_, id := remembered()
	lostID := "0123456789abcdef0123456789abcdef"
	encrecord.MarkLost(lostID)
	n, err := ForgetAll(nil)
	msg := ""
	code := 0
	if err != nil {
		msg, code = err.Error(), 1
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("secret store could not be opened", "nothing was forgotten"),
		assure.NoSideEffect(func() bool {
			rec, rerr := encrecord.Load()
			lost, _ := encrecord.Lost()
			return n == 0 && rerr == nil && rec.Archives[id.String()] != nil && keyKept(id) && len(lost) == 1
		}))
}
