package desktop

import (
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-1010, R36, R37
// The Desktop's plan panel says what step 8 will do with an encrypted archive's key, from this
// computer's real record (the keyring adapter's forget plan, read at the plan): with this the
// last copy recorded, the key is forgotten if the folder is removed; once a second copy of the
// archive is recorded here, the key is kept for that copy, named, and only this copy is forgotten.
// Positive twin first: the last recorded copy.
func TestThePlanPanelSaysWhetherTheKeyIsForgottenOrKept(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	t.Cleanup(func() { e.cfg.Keys.lockAll(true) })
	e.list(out)

	p := e.plan(out)
	const last = "If the folder is removed, this computer will forget this archive's key: this is the last copy of it this computer recorded. Any other copy of this archive will then need its password or recovery key to open."
	if p["ok"] != true || p["keyLine"] != last {
		t.Fatalf("positive twin: the last recorded copy's plan says %q (%v)", p["keyLine"], p["error"])
	}

	// A copy of the archive in another folder, opened here for a write: recorded under the same id.
	other := filepath.Join(t.TempDir(), "Enc copy")
	copyTree(t, out, other)
	u, err := keyring.OpenTyped(other, keysPassword, keyring.OpenOptions{Write: true})
	if err != nil {
		t.Fatalf("open the copy: %v", err)
	}
	u.Destroy()
	rec, err := encrecord.Load()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := rec.Lookup(other)
	if !ok {
		t.Fatalf("the copy is not recorded: %v", rec.Archives)
	}
	if mo, _ := rec.Lookup(out); mo.ID != m.ID {
		t.Fatalf("the copy is recorded under another archive id: %s, not %s", m.ID, mo.ID)
	}

	p = e.plan(out)
	want := "This computer keeps this archive's key for its other recorded copies: " + m.Path.Spelling + ". If the folder is removed, it forgets only this copy."
	if p["ok"] != true || p["keyLine"] != want {
		t.Fatalf("with a second recorded copy the plan says\n %q\nwant\n %q", p["keyLine"], want)
	}
}
