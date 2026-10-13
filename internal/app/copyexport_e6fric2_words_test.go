package app

import (
	"strings"
	"testing"
)

// covers: MA-535, MA-534, R1
// Re-verification of EW-E6's friction fixes F2 and F4 (words): what macOS's unzip does with the archive's own zip
// depends on how it is run. Walked here on a real archive's zip (DOS host, UTF-8 flag) with stdin a pipe or
// /dev/null: it printed "cannot create ... Illegal byte sequence", skipped the two non-ASCII entries, made the
// plain one and exited 50; the first walk, run from a terminal, saw "write error (disk full?)". The README
// quoted only the second. It names both, says that only some of the entries are made, and keeps the way out
// (Finder, 7-Zip, ditto, Python) and that the zip is not damaged and the disk is not full. And for a message
// the archive kept no original of, the README says that the page the archive wrote can send a reader to the
// archive's attachments-report.tsv, which is the archive's and not in the export (the page is copied as it is,
// MA-534, so it cannot be changed).
func TestReadmeNamesWhatMacUnzipPrintsOnTheArchivesZipAndWhereAnArchivedPagesPointerLeads(t *testing.T) {
	zb := e6Zip(t, e6ZipEntry{name: "Rapport février.pdf", data: []byte("%PDF fine")})
	for _, asZip := range []bool{true, false} {
		res, _, _ := e6zArchiveZip(t, newCEEnv(t), zb, asZip)
		r := readText(t, res.Root, copyReadmeName)
		for _, want := range []string{"cannot create", "Illegal byte sequence", "write error (disk full?)", "only some of the entries are made", "(exit 50)",
			"not damaged", "the disk is not full", "double-click it in Finder", "ditto -x -k <zip> <folder>", "7-Zip",
			"attachments-report.tsv", "is the archive's and is not in this export"} {
			if !strings.Contains(r, want) {
				t.Errorf("an archive export's README (zip mode %v) lacks %q", asZip, want)
			}
		}
	}
	// an export whose records all have an original has no archived page to send anyone anywhere
	a := buildE6Archive(t)
	s := a.open()
	key := a.key("alice-attach")
	p := a.plan(newCEEnv(t), s, []string{key})
	p.Options.Attachments, p.Options.AttachmentsAsZip = true, true
	if r := readText(t, a.run(newCEEnv(t), p, s).Root, copyReadmeName); strings.Contains(r, "attachments-report.tsv") || strings.Contains(r, "Illegal byte sequence") {
		t.Errorf("the README of an export with no archived page or zip names the archive's report or macOS's unzip")
	}
}

// covers: MA-535, MA-533, R1
// Re-verification of EW-E6's friction fix F12: a message whose only attachment is empty has no zip (nothing to
// put in one), and the README's first line about Extract attachments said "0 messages with attachments, each
// with its attachments in one zip..." beside the new count "none of the 1 found". It now says that no message has
// a zip, and keeps the sentence the one-message test asserts when a zip was made.
func TestReadmeOfAZipExportWithNoZipMadeDoesNotCountZeroMessagesWithAttachments(t *testing.T) {
	msgs := ceMsgs(1)
	body := map[string][]byte{msgs[0].SourceID: e6Mime(msgs[0].SourceID, "only an empty one", e6Part{name: "empty.txt", ctype: "text/plain"})}
	res, _ := e6zRun(t, newCEEnv(t), msgs, true, func(id string) []byte { return body[id] })
	r := readText(t, res.Root, copyReadmeName)
	for _, want := range []string{"Extract attachments: no message has an attachments zip (a message's zip is named like its file with .attachments.zip added, beside it).",
		"Attachments in the zips: none of the 1 found in these messages; each has a note below."} {
		if !strings.Contains(r, want) {
			t.Errorf("README lacks %q", want)
		}
	}
	if strings.Contains(r, "0 messages with attachments") {
		t.Errorf("README counts 0 messages with attachments beside a count of 1 attachment found")
	}
}
