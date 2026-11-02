package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-1011, R36, R37
// The deletion record of an encrypted archive whose key is open on this page states its sealed
// values (design-archive-delete §8.1 [O2-05]; del-s3a conformance D2): the saved plan and the
// record read from the open archive's sealed record give the message count, the mail's dates and
// the first run, each "as recorded in the archive", never "not known: encrypted, key not open",
// which was false while the key was open here. Positive twin first: with the archive locked on
// the page, the plan says "not known: encrypted, key not open" for all three.
func TestARecordOfAnArchiveOpenHereStatesItsSealedValues(t *testing.T) {
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(out)
	const notOpen = "not known: encrypted, key not open"

	p := e.plan(out)
	text, _ := p["planText"].(string)
	for _, l := range []string{"Messages: " + notOpen, "Mail dates: " + notOpen, "First run: " + notOpen} {
		if !strings.Contains(text, l+"\n") {
			t.Fatalf("positive twin: locked on the page, the plan should say %q:\n%s", l, text)
		}
	}

	if _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	p = e.plan(out)
	text, _ = p["planText"].(string)
	r := e.deleteWith(p["token"].(string), p["name"].(string), false)
	if r["ok"] != true || r["status"] != "complete" {
		t.Fatalf("the delete: %v", r)
	}
	record, _ := r["record"].(string)
	for what, s := range map[string]string{"the saved plan": text, "the deletion record": record} {
		if strings.Contains(s, notOpen) {
			t.Errorf("%s says %q while the key was open here:\n%s", what, notOpen, s)
		}
		// This fixture's backup leaves its sealed timeline empty, so its first run is "not known",
		// as for a plain archive with no history (archdelete's MA-1011 test reads a timeline).
		for _, l := range []string{"Messages: 1 (as recorded in the archive)\n", "Mail dates: 2024-01-01 to 2024-01-01 (as recorded in the archive)\n", "First run: not known\n"} {
			if !strings.Contains(s, l) {
				t.Errorf("%s does not say %q:\n%s", what, strings.TrimSpace(l), s)
			}
		}
	}
}
