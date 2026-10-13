package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-535, MA-534, R1
// F4 and F2 (friction review of EW-E6): a Microsoft 365 export's README does not send the reader to an archive
// that is not there ("the archive's own zip, described above", with nothing above), and an export that
// holds a zip copied from an archive says what that zip is and what to do when the unzip command of macOS
// stops on it ("write error (disk full?)", exit 50, at the first accented name): it is not damaged and the
// disk is not full; Finder, 7-Zip, ditto -x -k and Python read it. An export whose messages all have their
// own originals, and a Microsoft 365 one, say nothing of it. (The sentences that name an archived zip stay
// in a Microsoft 365 README, in words that say only an export from an archive has one: the tests of MA-535
// assert them.)
func TestReadmeNamesTheArchivesZipOnlyWhereThereIsOneAndSaysHowToOpenIt(t *testing.T) {
	const macWords = "write error (disk full?)"
	zb := e6Zip(t, e6ZipEntry{name: "Rapport février.pdf", data: []byte("%PDF fine")})

	// a Microsoft 365 export: no archive to describe, and nothing about macOS unzip
	msgs := ceMsgs(1)
	mime := e6Mime(msgs[0].SourceID, "one", e6Part{name: "a.bin", body: []byte("aaa")})
	for _, asZip := range []bool{true, false} {
		src := newFakeSrc()
		src.body = func(string) []byte { return mime }
		e := newCEEnv(t)
		p := cePlan(t, e, src, msgs)
		p.Options.Attachments, p.Options.AttachmentsAsZip = true, asZip
		r := readText(t, mustRun(t, e, p, src).Root, copyReadmeName)
		for _, banned := range []string{"described above", "(or, where the archive kept no original", macWords, "ditto"} {
			if strings.Contains(r, banned) {
				t.Errorf("a Microsoft 365 README (zip mode %v) says %q", asZip, banned)
			}
		}
		if asZip && !strings.Contains(r, "no second zip is made") {
			t.Errorf("a Microsoft 365 README (zip mode) no longer says that no second zip is made")
		}
	}

	// an archive export with a record that has no original: its zip is copied, and the README says what to do with it
	for _, asZip := range []bool{true, false} {
		res, _, _ := e6zArchiveZip(t, newCEEnv(t), zb, asZip)
		r := readText(t, res.Root, copyReadmeName)
		for _, want := range []string{macWords, "exit 50", "not damaged", "the disk is not full", "double-click it in Finder", "ditto -x -k <zip> <folder>", "7-Zip", "python3 -m zipfile -e <zip> <folder>", "accented or non-Latin"} {
			if !strings.Contains(r, want) {
				t.Errorf("an archive export's README (zip mode %v) lacks %q", asZip, want)
			}
		}
		if strings.Contains(r, "described above") {
			t.Errorf("an archive export's README (zip mode %v) still says \"described above\"", asZip)
		}
	}

	// an archive export in which every message has its own original: no archived zip, no such sentence
	a := buildE6Archive(t)
	s := a.open()
	key := a.key("alice-attach")
	p := a.plan(newCEEnv(t), s, []string{key})
	p.Options.Attachments, p.Options.AttachmentsAsZip = true, true
	if r := readText(t, a.run(newCEEnv(t), p, s).Root, copyReadmeName); strings.Contains(r, macWords) {
		t.Errorf("a README of an export with no archived zip tells how to open one")
	}
}

// e6fricPage is the readable page of one message line of a run, as text.
func e6fricPage(t *testing.T, root string, line *ceMsgLine) string {
	t.Helper()
	for _, f := range line.Files {
		if f.Role == roleDerivedHTML {
			return string(e6Read(t, root, f.Path))
		}
	}
	t.Fatalf("no readable page among %+v", line.Files)
	return ""
}

// covers: MA-417, MA-424, MA-533, R1, R7
// F5 and F6 (friction review of EW-E6): the readable page of an export points the reader of a part kept as received
// at the files an export has (its README.txt and export-manifest.json), not at the archive's
// attachments-report.tsv "in the archive's top folder", which an export does not have, in every way of
// making attachments and with the option off; and in zip mode a part that has no entry in the zip (an
// empty one) is listed as "not in the zip", where it was listed beside "download zip" as if it were in it.
// A part that is in the zip is listed by its name in the zip, as before.
func TestTheReadablePageOfAnExportPointsAtTheExportsOwnFilesAndMarksWhatIsNotInTheZip(t *testing.T) {
	msgs := ceMsgs(2)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "one empty", e6Part{name: "ok.pdf", ctype: "application/pdf", body: []byte("%PDF fine")}, e6Part{name: "empty.txt", ctype: "text/plain"}),
		msgs[1].SourceID: e6Corpus(t, "f24-cte-x-uuencode.eml"),
	}
	for _, mode := range []string{"zip", "loose", "off"} {
		t.Run(mode, func(t *testing.T) {
			e, src := newCEEnv(t), newFakeSrc()
			src.body = func(id string) []byte { return body[id] }
			p := cePlan(t, e, src, msgs)
			p.Options.ReadableHTML = true
			p.Options.Attachments, p.Options.AttachmentsAsZip = mode != "off", mode == "zip"
			res := mustRun(t, e, p, src)
			lines := e5Files(t, e, res)
			uu := e6fricPage(t, res.Root, assure.Reached(t, lines[2], "the second message's line"))
			if strings.Contains(uu, "attachments-report.tsv") || strings.Contains(uu, "archive's top folder") ||
				!strings.Contains(uu, "The README.txt and export-manifest.json, in this export's top folder, say which part and why.") {
				t.Errorf("the page of a part kept as received points the reader at %q", uu[strings.Index(uu, "mailarchive-partnote"):])
			}
			if mode != "zip" {
				return
			}
			page := e6fricPage(t, res.Root, assure.Reached(t, lines[1], "the first message's line"))
			if !strings.Contains(page, "ok.pdf, empty.txt (not in the zip)") || strings.Contains(page, "ok.pdf (not in the zip)") {
				t.Errorf("the page's list of attachments: %q", page[strings.Index(page, "Attachments:"):strings.Index(page, "Attachments:")+120])
			}
		})
	}
}

// covers: MA-424, MA-533, R1
// F13 (friction review of EW-E6): a part left out for want of room says the same thing in both ways of
// making attachments, ending with what holds the part: "free some space there and export this message again;
// the .eml beside it holds the message as it was sent". Loose mode's note lost the second half. And the README's
// Readable HTML line is not a plural for one message ("1 of the exported messages has a readable copy").
func TestANoteForAPartWithoutRoomEndsTheSameInBothWays(t *testing.T) {
	const ending = "free some space there and export this message again; the .eml beside it holds the message as it was sent"
	big1, big2 := e5Image(100000), append([]byte("second"), e5Image(99994)...)
	for _, asZip := range []bool{false, true} {
		name := "loose"
		if asZip {
			name = "zip"
		}
		t.Run(name, func(t *testing.T) {
			msgs := ceMsgs(1)
			mime := e6Mime(msgs[0].SourceID, "two big files", e6Part{name: "first.bin", body: big1}, e6Part{name: "second.bin", body: big2})
			e := newCEEnv(t)
			src := newFakeSrc()
			src.body = func(string) []byte { return mime }
			p := cePlan(t, e, src, msgs)
			p.Options.Attachments, p.Options.AttachmentsAsZip = true, asZip
			e.freeDest = e6Disk(p.Dest, p.Reserve+int64(len(mime))+160000)
			res := mustRun(t, e, p, src)
			line := assure.Reached(t, e5Files(t, e, res)[1], "the message's line")
			var note string
			for _, n := range line.Notes {
				if strings.Contains(n, `"second.bin"`) {
					note = n
				}
			}
			if !strings.Contains(assure.Reached(t, note, "the note of the part that did not fit"), "no space was left") || !strings.HasSuffix(note, ending) {
				t.Errorf("the note is %q, want it to end %q", note, ending)
			}
		})
	}
	one := ceMsgs(1)
	res, _ := e6zRun(t, newCEEnv(t), one, true, func(string) []byte { return e6Mime(one[0].SourceID, "one") })
	if r := readText(t, res.Root, copyReadmeName); !strings.Contains(r, "Readable HTML: 1 of the exported messages has a readable copy") {
		t.Errorf("README's Readable HTML line for one message does not read \"1 of the exported messages has a readable copy\" (%d bytes of README)", len(r))
	}
}
