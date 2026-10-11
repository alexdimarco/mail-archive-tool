package app

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// bidiIn is the first Unicode bidirectional formatting control in s, or 0.
func bidiIn(s string) rune {
	for _, r := range s {
		if (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return r
		}
	}
	return 0
}

// covers: MA-435, MA-420, R29, S52
// Text an archive handed over supplies (a subject, a folder name, a store's mailbox) reaches the export's
// README and manifest with no bidirectional formatting control in it (E4-AO7; friction F16, util.StripBidi):
// StripControl keeps U+202E, and a right-to-left override in a subject or a folder name made the rest of
// a README line, the reason beside a failed message, read reversed or reordered. The text itself is still
// there (the positive twin), only the controls are not.
func TestArchiveTextReachesTheReadmeAndTheManifestWithoutBidiControls(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	a.edit(func(m *state.Manifest) { // a Graph mailbox is a store whose source id is "mailbox:<address>"
		for id, tok := range m.Stores {
			if tok == "alice" {
				delete(m.Stores, id)
				m.Stores["mailbox:alice\u202e@contoso.org"] = "alice"
			}
		}
	})
	a.setRec("hello", func(r *state.Record) { r.Folder = "Inbox\u202eevil\u2066" })
	hello := a.key("hello")
	s := a.open()
	meta := indexMeta(t, s)
	row := meta[hello]
	row.Subject, row.SenderName, row.SenderEmail = "\u202eexe.gnp invoice", "A\u202e\u2067B", "x\u202e@example.com"
	meta[hello] = row
	p, _ := a.planWith(e, s, []string{hello, a.key("second")}, meta)
	for _, st := range s.Stores() {
		if bidiIn(st.Mailbox) != 0 || bidiIn(st.Label()) != 0 {
			t.Errorf("the store label %q carries a bidi control", st.Label())
		}
	}
	for _, n := range s.Tree() {
		if bidiIn(n.Name) != 0 || bidiIn(n.Mailbox) != 0 {
			t.Errorf("the tree node %q carries a bidi control", n.Name)
		}
	}
	_, eml, _ := a.files("hello")
	if err := os.Remove(eml); err != nil { // after the preview: the failed message the README lists beside its reason
		t.Fatal(err)
	}
	res := a.run(e, p, s)
	if res.Counts.Failed != 1 || res.Counts.Exported != 1 {
		t.Fatalf("the run: %+v, want one message exported and the removed one listed", res.Counts)
	}
	readme := readText(t, res.Root, "README.txt")
	for _, ln := range strings.Split(readme, "\n") {
		if r := bidiIn(ln); r != 0 {
			t.Errorf("README.txt carries U+%04X in the line %q", r, ln)
		}
	}
	for _, want := range []string{"exe.gnp invoice", "Inboxevil", "alice (alice@contoso.org)"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.txt lost the text %q (the controls only are stripped)", want)
		}
	}
	for _, name := range []string{"export-manifest.json"} {
		if r := bidiIn(readText(t, res.Root, name)); r != 0 {
			t.Errorf("%s carries U+%04X", name, r)
		}
	}
}
