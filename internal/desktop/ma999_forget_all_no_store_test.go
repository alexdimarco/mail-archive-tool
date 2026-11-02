package desktop

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-999, R37
// Forget every encrypted archive on this computer (FORGET typed), on a Desktop whose secret store
// could not be opened, is refused with nothing changed: it would empty the record and say the keys
// were forgotten while every remembered key stayed in the store with nothing pointing to it (the
// delete residuals' key-use pass, finding 1; the single archive's Forget already refuses, MA-999).
// Positive twin first: with the store, the record and the key are forgotten.
func TestForgetEveryArchiveIsRefusedWhenTheSecretStoreCannotBeOpened(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	store := graphconfig.NewFileSecretStore(t.TempDir())
	newArchive := func(name string) (string, string) {
		t.Helper()
		out := filepath.Join(t.TempDir(), name)
		p, err := keyring.Create(out, "desktop test passphrase", keyring.CreateOptions{Remember: true, Store: store})
		if err != nil {
			t.Fatal(err)
		}
		u, err := p.Commit()
		if err != nil {
			t.Fatal(err)
		}
		u.Destroy()
		rec, err := encrecord.Load()
		if err != nil {
			t.Fatal(err)
		}
		m, ok := rec.Lookup(out)
		if !ok {
			t.Fatalf("%s is not recorded", out)
		}
		return out, m.ID
	}
	recorded := func(id string) bool {
		rec, err := encrecord.Load()
		return err == nil && rec.Archives[id] != nil && !rec.Archives[id].Forgotten
	}
	keyKept := func(id string) bool { _, err := store.Get("archive:" + id); return err == nil }

	twin, twinID := newArchive("Twin")
	e := newKeysEnvWith(t, twin, store)
	if _, m := e.post(t, "api/encryption/forget-all", map[string]any{"typed": forgetAllWord}); m["ok"] != true || recorded(twinID) || keyKept(twinID) {
		t.Fatalf("positive twin: with the store, forget-all forgets the record and the key: %v", m)
	}

	out, id := newArchive("Sealed")
	n := newKeysEnvWith(t, out, nil)
	// A forget waiting for its answer: a refusal that changes nothing leaves it waiting.
	_, plan := n.post(t, "api/encryption/forget/plan", map[string]any{"path": out})
	if plan["ok"] != true {
		t.Fatalf("the forget plan: %v", plan)
	}
	_, m := n.post(t, "api/encryption/forget-all", map[string]any{"typed": forgetAllWord})
	msg, _ := m["error"].(string)
	code := 0
	if m["ok"] != true {
		code = 1
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("secret store could not be opened", "nothing was forgotten", "try again once the secret store opens"),
		assure.Forbid("This computer forgot"),
		assure.NoSideEffect(func() bool { return recorded(id) && keyKept(id) }))
	if _, f := n.post(t, "api/encryption/forget", map[string]any{"token": plan["token"], "whole": false}); f["ok"] != true {
		t.Fatalf("the forget waiting before the refusal was discarded by it: %v", f)
	}
}
