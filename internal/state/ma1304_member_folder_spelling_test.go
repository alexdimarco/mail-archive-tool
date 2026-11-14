package state

import (
	"testing"
)

// covers: MA-1304, R45
// The manifest's load validation keeps a Gmail membership only when each segment of its folder is
// what the exporter's name rule (util.SanitizeSegment, which the local walk applies to every
// folder) writes (the B3 adversarial pass's A2): a folder Windows spells another way ("INBOX.",
// ".. ", "Sent ", a reserved device name) or one the exporter would have renamed ("a:b") is
// dropped, with a note, never the record. Positive twin first: Gmail's own folders as the walk
// spells them are kept.
func TestAMembershipFolderMustBeSpelledAsTheExporterSpellsIt(t *testing.T) {
	m := &Manifest{}
	for _, f := range []string{"[Gmail]/All Mail", "Receipts", "Labels/Work", "Q1 Reports~3fa2b1c0"} {
		if !validMemberFolder(f) {
			t.Fatalf("positive twin: %q is refused", f)
		}
	}
	for _, f := range []string{"INBOX.", ".. ", "x/.. ", "Sent ", "CON", "a:b", "a/b."} {
		if validMemberFolder(f) {
			t.Fatalf("%q, which Windows spells another way or the exporter renames, is kept", f)
		}
	}
	r := Record{Folder: "[Gmail]/All Mail", Folders: []Membership{{Folder: "INBOX.", Kind: MemberListed}, {Folder: "Receipts", Kind: MemberListed}}}
	kept := m.validMembers("k", r)
	if len(kept) != 1 || kept[0].Folder != "Receipts" || len(m.DedupNotes) != 1 {
		t.Fatalf("kept %+v, notes %v", kept, m.DedupNotes)
	}
}
