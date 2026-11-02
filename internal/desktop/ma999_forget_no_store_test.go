package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/localweb"
)

// covers: MA-999, R37
// Forget this encrypted archive here, from the archive's last recorded folder, when this
// computer's secret store could not be opened: forgetting the whole archive is refused, since a
// key remembered for it would be left with nothing pointing to it; the record and the key are
// kept, and the plan says so first. Forgetting only the folder still works. The same rule as
// forget-archive at the terminal (the "cli" group's adversarial pass, finding 6).
func TestARowKeepsTheRecordWhenTheSecretStoreCannotBeOpened(t *testing.T) {
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
	archiveRecorded := func(id string) bool {
		rec, err := encrecord.Load()
		return err == nil && rec.Archives[id] != nil && !rec.Archives[id].Forgotten
	}
	folderRecorded := func(out string) bool {
		rec, err := encrecord.Load()
		if err != nil {
			return false
		}
		_, ok := rec.Lookup(out)
		return ok
	}
	keyKept := func(id string) bool {
		_, err := store.Get("archive:" + id)
		return err == nil
	}

	// Positive twin: with the store, the whole archive and its remembered key are forgotten.
	twin, twinID := newArchive("Twin")
	e := newKeysEnvWith(t, twin, store)
	assure.Reached(t, keyKept(twinID) && archiveRecorded(twinID), "the archive recorded and its key remembered")
	_, m := e.post(t, "api/encryption/forget/plan", map[string]any{"path": twin})
	if m["ok"] != true || m["last"] != true || m["remembered"] != true {
		t.Fatalf("the plan with the store: %v", m)
	}
	_, m = e.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "whole": true, "keys": true})
	if m["ok"] != true || archiveRecorded(twinID) || keyKept(twinID) {
		t.Fatalf("with the store, the whole archive and its key were not forgotten: %v", m)
	}

	// No store: the plan says so, and the whole archive is refused, the record and the key kept.
	out, id := newArchive("Sealed")
	n := newKeysEnvWith(t, out, nil)
	_, m = n.post(t, "api/encryption/forget/plan", map[string]any{"path": out})
	if m["ok"] != true || m["last"] != true || !strings.Contains(strings.Join(anyStrings(m["lines"]), "\n"), noStoreWholeLine) {
		t.Fatalf("the plan with no store: %v", m)
	}
	_, m = n.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "whole": true, "keys": true})
	msg, _ := m["error"].(string)
	code := 0
	if m["ok"] != true {
		code = 1
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("secret store could not be opened", "keeps its record of the archive", "nothing was changed", "click Forget again"),
		assure.NoSideEffect(func() bool { return folderRecorded(out) && archiveRecorded(id) && keyKept(id) }))

	// Only the folder: forgotten; the record of the archive and its key stay.
	_, m = n.post(t, "api/encryption/forget/plan", map[string]any{"path": out})
	_, m = n.post(t, "api/encryption/forget", map[string]any{"token": m["token"], "whole": false})
	if m["ok"] != true || folderRecorded(out) || !archiveRecorded(id) || !keyKept(id) {
		t.Fatalf("forgetting only the folder with no store: %v", m)
	}
}

// newKeysEnvWith is newKeysEnv with the secret store given: nil is a Desktop whose store could not
// be opened (as launch leaves it).
func newKeysEnvWith(t *testing.T, out string, store graphconfig.SecretStore) *keysEnv {
	t.Helper()
	dir := t.TempDir()
	settings := filepath.Join(dir, "desktop-settings.json")
	if err := SaveSettings(settings, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SettingsPath: settings, IndexPath: filepath.Join(dir, "known-archives.json"), Store: store, Keys: newArchiveKeys(store, readerLockedNote)}
	sess := localweb.NewSession()
	h := dashboardHandler(cfg, loopback(8097), sess, nil)
	m := dashCSRFRe.FindStringSubmatch(sessionPage(t, h))
	if m == nil {
		t.Fatal("no CSRF token on the page")
	}
	t.Cleanup(func() { cfg.Keys.lockAll(true) })
	return &keysEnv{h: h, csrf: m[1], cfg: cfg, out: out}
}

// anyStrings is a JSON array of strings as decoded into any.
func anyStrings(v any) []string {
	var out []string
	if a, ok := v.([]any); ok {
		for _, s := range a {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
	}
	return out
}
