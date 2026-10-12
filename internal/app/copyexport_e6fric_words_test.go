package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

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
