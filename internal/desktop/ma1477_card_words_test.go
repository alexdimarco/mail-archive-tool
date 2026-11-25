package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/graphconfig"
)

// covers: MA-1477, MA-1004, R36
// The This archive card and its Delete… tooltip, for a plain and an encrypted archive (the 1.7.5
// re-verify's RF-07, which no committed test pinned, and round 2's RF2-02): a plain card names
// Delete when the check finishes; an encrypted card says Delete… erases its files after showing what
// it would remove, and its Delete… tooltip says the list is always shown first, never naming the
// button its panel does not offer. Positive twin first: the plain card and its tooltip.
func TestTheThisArchiveCardAndTooltipFitTheArchive(t *testing.T) {
	e := newDeleteEnv(t)
	only := e.plainArchive("Only")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: only}); err != nil {
		t.Fatal(err)
	}
	card := cardOf(e.page(), "sec-this-archive")
	if !strings.Contains(card, "you see what it would remove first, unless you choose <b>Delete when the check finishes</b> and nothing in it needs your decision") ||
		!strings.Contains(card, "unless you choose Delete when the check finishes and nothing in the list needs your decision") {
		t.Fatalf("positive twin: the plain card and its tooltip:\n%s", card)
	}
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e2 := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	enc := backedUpEncrypted(t)
	e2.list(enc)
	if err := SaveSettings(e2.cfg.SettingsPath, Settings{Out: enc}); err != nil {
		t.Fatal(err)
	}
	card2 := cardOf(e2.page(), "sec-this-archive")
	if !strings.Contains(card2, "<b>Delete…</b> erases its files, after showing what it would remove") {
		t.Errorf("the encrypted card's sentence:\n%s", card2)
	}
	if !strings.Contains(card2, "data-delete-encrypted") || strings.Contains(card2, "unless you choose Delete when the check finishes") ||
		!strings.Contains(card2, "For an encrypted archive, you always see that list before anything is deleted.") {
		t.Errorf("the encrypted archive's Delete… tooltip must say the list is always shown first, never the armed button:\n%s", card2)
	}
}
