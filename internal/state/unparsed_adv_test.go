package state

import (
	"strings"
	"testing"
)

// covers: MA-318, R1, R3, S46
// S2B-AC7: the same unparsable bytes in two folders are two stubs with one digest, and each
// is settled by the healthy capture of its own copy. The digest index held ONE key per
// digest (whichever stub was indexed last), so the first healthy capture superseded the
// wrong folder's stub (RecoveredBy pointing at another folder's record) and the other stub
// was never settled: it stayed "unparsed" in the status and the report though its message
// had a healthy page. Each stub is superseded by the capture of its own folder, the others
// stay until their own copies are read, and a capture in a folder with no stub of those
// bytes (the message moved) settles the first stub in key order.
func TestTwoStubsOfTheSameBytesAreEachSettledByTheirOwnCopy(t *testing.T) {
	m, _, tok := gapManifest(t)
	digest := strings.Repeat("0f", 32)
	inbox, archive := Key(tok, "Inbox", "sha:stub"), Key(tok, "Archive", "sha:stub")
	m.Add(inbox, Record{Path: tok + "/Inbox/stub.html", Folder: "Inbox", Terminal: []string{LabelUnparsed}, ParserGen: 1, RawDigest: digest})
	if !m.HasUnparsedStubs() { // the index is built before the second stub arrives
		t.Fatal("the manifest holds a stub with a digest but says it holds none")
	}
	m.Add(archive, Record{Path: tok + "/Archive/stub.html", Folder: "Archive", Terminal: []string{LabelUnparsed}, ParserGen: 1, RawDigest: digest})
	healthyInbox, healthyArchive := Key(tok, "Inbox", "mid:m1@x"), Key(tok, "Archive", "mid:m1@x")
	m.Add(healthyInbox, Record{Path: tok + "/Inbox/m1.html", Folder: "Inbox"})
	m.Add(healthyArchive, Record{Path: tok + "/Archive/m1.html", Folder: "Archive"})

	if !m.SupersedeUnparsed(tok, digest, healthyInbox) {
		t.Fatal("the healthy copy in Inbox settled no stub")
	}
	if r, _ := m.Get(inbox); len(r.Terminal) != 1 || r.Terminal[0] != LabelSuperseded || r.RecoveredBy != healthyInbox {
		t.Errorf("the Inbox stub = %+v; want it superseded by the Inbox copy, %q", r, healthyInbox)
	}
	if r, _ := m.Get(archive); len(r.Terminal) != 1 || r.Terminal[0] != LabelUnparsed {
		t.Errorf("the Archive stub = %+v; want it left unparsed until its own copy is read", r)
	}
	if !m.HasUnparsedStubs() {
		t.Error("the Archive stub is no longer found: the supersede of the other dropped its index entry")
	}
	if !m.SupersedeUnparsed(tok, digest, healthyArchive) {
		t.Fatal("the healthy copy in Archive settled no stub")
	}
	if r, _ := m.Get(archive); len(r.Terminal) != 1 || r.Terminal[0] != LabelSuperseded || r.RecoveredBy != healthyArchive {
		t.Errorf("the Archive stub = %+v; want it superseded by the Archive copy, %q", r, healthyArchive)
	}
	if m.HasUnparsedStubs() {
		t.Error("a stub is still found after both were superseded")
	}

	// A capture in a folder with no stub of its bytes (the message moved) settles the first.
	m2, _, tok2 := gapManifest(t)
	a, b := Key(tok2, "A", "sha:stub"), Key(tok2, "B", "sha:stub")
	m2.Add(b, Record{Path: tok2 + "/B/s.html", Folder: "B", Terminal: []string{LabelUnparsed}, ParserGen: 1, RawDigest: digest})
	m2.Add(a, Record{Path: tok2 + "/A/s.html", Folder: "A", Terminal: []string{LabelUnparsed}, ParserGen: 1, RawDigest: digest})
	moved := Key(tok2, "C", "mid:m@x")
	m2.Add(moved, Record{Path: tok2 + "/C/m.html", Folder: "C"})
	if !m2.SupersedeUnparsed(tok2, digest, moved) {
		t.Fatal("a capture in another folder settled no stub")
	}
	if r, _ := m2.Get(a); len(r.Terminal) != 1 || r.Terminal[0] != LabelSuperseded {
		t.Errorf("the stub of folder A = %+v; want the first stub in key order settled", r)
	}
}
