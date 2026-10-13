package app

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-533, MA-534, MA-424, R1
// Re-verification of EW-E6's friction fixes (F3, F11): the card of an export from an archive says what became of
// the attachments of a record the archive kept no original of. In zip mode that record's zip is the archive's
// own, copied as it is (MA-534), and the card said "none of these messages had an attachment" while the README
// of the same export counted the zip; in loose mode its entries are extracted from the archive's zip, and the
// card said the same beside the README's "2 files from 1 message". A mixed export counted only the zips made
// from originals. The card and the README must not disagree about what the export holds.
func TestTheCardOfAnArchiveExportCountsTheZipsItCopiedAndTheFilesItExtracted(t *testing.T) {
	zb := e6Zip(t, e6ZipEntry{name: "ok.pdf", data: []byte("%PDF fine")}, e6ZipEntry{name: "data.bin", data: []byte("some data")})
	// run exports the archive's "zip only" record (and, with both, the record that has an original and two attachments)
	run := func(t *testing.T, asZip, both bool) (CopyRunInfo, string) {
		t.Helper()
		a := buildE6Archive(t)
		_, _, zipPath := a.files("zip-only")
		if err := os.WriteFile(zipPath, zb, 0o644); err != nil {
			t.Fatal(err)
		}
		a.setRec("zip-only", func(r *state.Record) { r.Fixity.Zip = digestOf(zb) })
		s := a.open()
		keys := []string{a.key("zip-only")}
		if both {
			keys = append(keys, a.key("alice-attach"))
		}
		e := newCEEnv(t)
		p := a.plan(e, s, keys)
		p.Options.Attachments, p.Options.AttachmentsAsZip = true, asZip
		res := a.run(e, p, s)
		if res.Status != CopyStatusComplete || res.Counts.Exported != len(keys) {
			t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
		}
		return e6fricCard(t, e, res.RunID), readText(t, res.Root, copyReadmeName)
	}

	cases := []struct {
		name        string
		asZip, both bool
		line        string
		readme      string // what the README of the same export counts
	}{
		{"zip, the copied zip alone", true, false,
			"Attachments, one zip per message: 1 zip the archive kept, copied as it is.",
			"1 message with an attachments zip the archive kept, copied as it is"},
		{"loose, the extracted zip alone", false, false,
			"Attachments, loose files: 2 files from 1 message.",
			"Extract attachments: 2 files from 1 message"},
		{"zip, a copied zip and a zip made from an original", true, true,
			"Attachments, one zip per message: 2 attachments in 1 zip, and 1 zip the archive kept, copied as it is.",
			"1 message with an attachments zip the archive kept, copied as it is"},
		{"loose, extracted from the zip and from an original", false, true,
			"Attachments, loose files: 4 files from 2 messages.",
			"Extract attachments: 4 files from 2 messages"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			card, readme := run(t, c.asZip, c.both)
			line := assure.Reached(t, card.AttachLine, "the card's attachments line")
			if line != c.line {
				t.Errorf("the card's attachments line is %q, want %q", line, c.line)
			}
			if strings.Contains(line, "none of these messages had an attachment") {
				t.Errorf("the card says nothing was there, and the export holds attachments: %q", line)
			}
			if !strings.Contains(readme, c.readme) {
				t.Errorf("the README of the same export does not say %q (the card and the README must agree)", c.readme)
			}
			if card.AttachWarn != "" {
				t.Errorf("an export that lost nothing has a warning: %q", card.AttachWarn)
			}
		})
	}
}
