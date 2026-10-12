package app

import (
	"fmt"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// e6fricCard is the history card of run.
func e6fricCard(t *testing.T, e *CopyExportEnv, run string) CopyRunInfo {
	t.Helper()
	hist, err := e.History(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range hist {
		if c.Run == run {
			return c
		}
	}
	t.Fatalf("the history has no card for run %s", run)
	return CopyRunInfo{}
}

// covers: MA-533, MA-424, MA-430, R1, R29
// F3, F11, F1 (friction review of EW-E6): the cards of Export activity say what became of the attachments, and
// in which way they were made. A run cancelled while its first message's 40 attachments were being made
// is exported with that message's original and no attachments, and its note says "export this message
// again": the card says how many attachments were made and in which way (one zip per message, or loose
// files), says in a warning that 1 message was exported without all of its attachments, that its original is
// complete, how to get them (select it again in a new export: "Export the remaining messages" does not
// include it) and the manifest says so too. After the rest is exported in a later part of the set, the set's
// total no longer reads as whole: it says one message is without all its attachments. The part that made
// the rest says what it made and has no warning. (A retry that takes the message itself is the owner's:
// it changes what "exported" counts for a message, which TestZipModeChecksFreeSpaceAndObservesCancelAndAbandon asserts.)
func TestTheCardOfAnExportWithAttachmentsSaysWhatBecameOfThemAndWhoLostSome(t *testing.T) {
	for _, asZip := range []bool{true, false} {
		way, made2 := "loose files", "4 files from 2 messages"
		if asZip {
			way, made2 = "one zip per message", "4 attachments in 2 zips"
		}
		t.Run(way, func(t *testing.T) {
			msgs := ceMsgs(3)
			var many []e6Part
			for i := 0; i < 40; i++ {
				many = append(many, e6Part{name: fmt.Sprintf("f%02d.bin", i), body: []byte(fmt.Sprintf("part %d of the message", i))})
			}
			two := func(id string) []byte {
				return e6Mime(id, "two", e6Part{name: "a.bin", body: []byte("aaa")}, e6Part{name: "b.bin", body: []byte("bbb")})
			}
			body := map[string][]byte{msgs[0].SourceID: e6Mime(msgs[0].SourceID, "forty files", many...), msgs[1].SourceID: two(msgs[1].SourceID), msgs[2].SourceID: two(msgs[2].SourceID)}
			e := newCEEnv(t)
			src := newFakeSrc()
			src.body = func(id string) []byte { return body[id] }
			p := cePlan(t, e, src, msgs)
			p.Options.Attachments, p.Options.AttachmentsAsZip = true, asZip
			run := e.NewCopyRun(p, src)
			made := 0
			e.fsWrap = func(f ceFS) ceFS {
				return &e6zOnTemp{ceFS: f, onTemp: func() {
					if made++; made == 2 { // the first message's original is the first temp, its zip (or first file) the second
						run.Cancel()
					}
				}}
			}
			res, err := run.Run(t.Context())
			e.fsWrap = nil
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != CopyStatusCancelled || res.Counts.Exported != 1 || res.Counts.NotAttempted != 2 {
				t.Fatalf("the cancelled run: %s, %+v", res.Label, res.Counts)
			}

			// the manifest says the message's attachments are to be exported again
			att := assure.Reached(t, e6fricAtt(t, manifestOf(t, res.Root), msgs[0].SourceID), "the cut message's attachments entry")
			if att["found"] != float64(40) || att["export_again"] != true {
				t.Errorf("the cut message's attachments: %v, want found 40 and export_again", att)
			}

			card := e6fricCard(t, e, res.RunID)
			if !strings.Contains(card.AttachLine, "Attachments, "+way+":") || !strings.Contains(card.AttachLine, "none of the 40 found") || !strings.Contains(card.AttachLine, "1 message has a note about an attachment (README.txt lists them)") {
				t.Errorf("the card's attachments line is %q", card.AttachLine)
			}
			warn := assure.Reached(t, card.AttachWarn, "the card's warning about the message that lost its attachments")
			for _, want := range []string{"1 message was exported without all of its attachments", "the export was stopped, the disk was full or a file could not be written",
				"Its .eml is complete", "select that message again in a new export", "'Export the remaining messages' does not include it", "export-manifest.json"} {
				if !strings.Contains(warn, want) {
					t.Errorf("the warning lacks %q: %q", want, warn)
				}
			}

			// the rest, as a later part of the set
			in, err := e.RetryInput(res.RunID, "rest", "alice@contoso.org")
			if err != nil {
				t.Fatal(err)
			}
			in.Dest = p.Dest
			p2, err := e.PlanCopyExport(*in)
			if err != nil {
				t.Fatal(err)
			}
			res2 := mustRun(t, e, p2, src)
			if res2.Status != CopyStatusComplete || res2.Counts.Exported != 2 {
				t.Fatalf("part 2: %s, %+v", res2.Label, res2.Counts)
			}
			first, second := e6fricCard(t, e, res.RunID), e6fricCard(t, e, res2.RunID)
			for i, c := range []CopyRunInfo{first, second} {
				if !strings.Contains(c.SetNote, "All 3 messages are exported") || !strings.Contains(c.SetNote, "1 of them without all its attachments") {
					t.Errorf("part %d's set total reads %q: it must not read as whole", i+1, c.SetNote)
				}
			}
			if first.AttachWarn == "" {
				t.Errorf("part 1's card lost its warning after the rest was exported")
			}
			if second.AttachWarn != "" || !strings.Contains(second.AttachLine, "Attachments, "+way+": "+made2) {
				t.Errorf("part 2's card: line %q, warning %q, want %q and no warning", second.AttachLine, second.AttachWarn, made2)
			}
		})
	}

	// the option off, or a run that had nothing to say: a card with no attachments line, as before
	e, src := newCEEnv(t), newFakeSrc()
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	if c := e6fricCard(t, e, res.RunID); c.AttachLine != "" || c.AttachWarn != "" {
		t.Errorf("a run without Extract attachments has the card line %q / %q", c.AttachLine, c.AttachWarn)
	}
}
