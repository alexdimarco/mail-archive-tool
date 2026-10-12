package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// Extract attachments: the friction review's fixes (docs/review-extraction-workspace-e6-friction.md,
// EW-E6 friction pass). Each test states what the owner or the inheritor now sees; each was shown red
// against the code of 0d7ef67 before its fix.

// e6fricMsgs is three messages: one with five parts (a name twice, a name that climbs out of a
// folder, an empty part), one with none, one with two plain parts.
func e6fricMsgs() ([]CopyMessage, func(id string) []byte) {
	msgs := ceMsgs(3)
	body := map[string][]byte{
		msgs[0].SourceID: e6Mime(msgs[0].SourceID, "five parts",
			e6Part{name: "ok.pdf", ctype: "application/pdf", body: []byte("%PDF fine")},
			e6Part{name: "notes.txt", ctype: "text/plain", body: []byte("one")},
			e6Part{name: "notes.txt", ctype: "text/plain", body: []byte("two")},
			e6Part{name: "../../evil.txt", ctype: "text/plain", body: []byte("three")},
			e6Part{name: "empty.txt", ctype: "text/plain"}),
		msgs[1].SourceID: e6Mime(msgs[1].SourceID, "no attachment"),
		msgs[2].SourceID: e6Mime(msgs[2].SourceID, "two parts",
			e6Part{name: "a.bin", body: []byte("aaa")}, e6Part{name: "b.bin", body: []byte("bbb")}),
	}
	return msgs, func(id string) []byte { return body[id] }
}

// e6fricAtt is the "attachments" object of one message's manifest entry, as numbers.
func e6fricAtt(t *testing.T, man map[string]any, id string) map[string]any {
	t.Helper()
	a, _ := entryOf(t, man, id)["attachments"].(map[string]any)
	return a
}

// covers: MA-533, MA-535, R1
// F12, F7 (friction review of EW-E6): the cautious owner and the support person can see, without opening a
// zip, how many attachments each message had and how many are in its zip, and that a name was changed. The
// manifest gives each message with attachments {found, in_zip, renamed}, the README counts the attachments
// in the zips against the ones found, and says that a name in a zip can differ from the sender's and where
// the sender's is. A message with no attachment has no such entry; an empty part is found and not in the zip.
func TestZipModeCountsEachMessagesAttachmentsAndSaysNamesMayChange(t *testing.T) {
	msgs, body := e6fricMsgs()
	res, _ := e6zRun(t, newCEEnv(t), msgs, false, body)
	man := manifestOf(t, res.Root)
	first := assure.Reached(t, e6fricAtt(t, man, msgs[0].SourceID), "the first message's attachments entry")
	if first["found"] != float64(5) || first["in_zip"] != float64(4) || first["renamed"] != float64(2) {
		t.Errorf("the first message's attachments: %v, want found 5, in_zip 4, renamed 2 (notes (2).txt and evil.txt)", first)
	}
	if first["export_again"] != nil {
		t.Errorf("a zero-byte part is not a reason to export the message again: %v", first)
	}
	if third := assure.Reached(t, e6fricAtt(t, man, msgs[2].SourceID), "the third message's attachments entry"); third["found"] != float64(2) || third["in_zip"] != float64(2) || third["renamed"] != nil {
		t.Errorf("the third message's attachments: %v, want found 2, in_zip 2 and no renamed", third)
	}
	if got := e6fricAtt(t, man, msgs[1].SourceID); got != nil {
		t.Errorf("a message with no attachment has an attachments entry: %v", got)
	}
	readme := readText(t, res.Root, copyReadmeName)
	for _, want := range []string{
		"Attachments in the zips: 6 of the 7 found in these messages, in 2 zips. The other 1 has a note below.",
		"An attachment's name in a zip can differ from the one the sender gave it", "\"notes (2).txt\"", "The .eml beside the zip holds the sender's names",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q", want)
		}
	}

	// every attachment made: the README says so, in the singular for one zip
	one := ceMsgs(1)
	two := e6Mime(one[0].SourceID, "two", e6Part{name: "a.bin", body: []byte("aaa")}, e6Part{name: "b.bin", body: []byte("bbb")})
	res1, _ := e6zRun(t, newCEEnv(t), one, false, func(string) []byte { return two })
	if want := "Attachments in the zips: 2, in 1 zip: every attachment found in these messages."; !strings.Contains(readText(t, res1.Root, copyReadmeName), want) {
		t.Errorf("README of a clean run lacks %q", want)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}

	// loose mode says the same of its files, with its own word for them
	lres, _ := e6Run(t, newCEEnv(t), msgs, false, body)
	lman := manifestOf(t, lres.Root)
	lfirst := assure.Reached(t, e6fricAtt(t, lman, msgs[0].SourceID), "the first message's attachments entry in loose mode")
	if lfirst["found"] != float64(5) || lfirst["extracted"] != float64(4) || lfirst["renamed"] != float64(2) || lfirst["in_zip"] != nil {
		t.Errorf("loose mode's attachments: %v, want found 5, extracted 4, renamed 2", lfirst)
	}
	if lr := readText(t, lres.Root, copyReadmeName); !strings.Contains(lr, "A file's name can differ from the one the sender gave it") || strings.Contains(lr, "Attachments in the zips") {
		t.Errorf("README of a loose run: the names sentence is missing or the zip count is there")
	}
}
