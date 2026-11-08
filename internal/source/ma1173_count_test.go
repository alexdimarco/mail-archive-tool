package source

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// countMsg is one message as Thunderbird keeps it in an offline store: status "" leaves
// X-Mozilla-Status out, id "" leaves the Message-ID out.
func countMsg(subject, id, status string) string {
	h := "From: a@example.com\nTo: b@example.com\nSubject: " + subject + "\nDate: Mon, 03 Mar 2025 09:00:00 +0000\n"
	if id != "" {
		h += "Message-ID: <" + id + ">\n"
	}
	if status != "" {
		h += "X-Mozilla-Status: " + status + "\nX-Mozilla-Status2: 00000000\n"
	}
	return h + "\nbody of " + subject + "\n"
}

func countMbox(msgs ...string) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString("From - Mon Mar  3 09:00:00 2025\n")
		b.WriteString(m)
		b.WriteString("\n")
	}
	return b.String()
}

func countWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// countStore is a Thunderbird-shaped IMAP store: INBOX and [Gmail]/All Mail as mbox files
// beside their .msf summaries, and a maildir folder. It holds deleted copies not yet
// compacted (X-Mozilla-Status 0009 and 0008), a message filed in two folders by one
// Message-ID, and messages with no Message-ID.
func countStore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "imap.gmail.com")
	countWrite(t, filepath.Join(dir, "INBOX"), countMbox(
		countMsg("one", "one@x", "0001"),
		countMsg("two", "two@x", "0000"),
		countMsg("deleted", "gone@x", "0009"),
		// No X-Mozilla-Status and no Date (not well-formed: the owner's ruling of 2026-11-08), so
		// it stays a loose piece.
		strings.Replace(countMsg("plain", "plain@x", ""), "Date: Mon, 03 Mar 2025 09:00:00 +0000\n", "", 1)))
	countWrite(t, filepath.Join(dir, "INBOX.msf"), "// a summary file, never read\n")
	countWrite(t, filepath.Join(dir, "[Gmail].sbd", "All Mail"), countMbox(
		countMsg("one again", "one@x", "0001"),
		countMsg("nameless", "", "0001"),
		countMsg("nameless too", "", "0000"),
		countMsg("deleted too", "two@x", "0008")))
	countWrite(t, filepath.Join(dir, "[Gmail].sbd", "All Mail.msf"), "")
	countWrite(t, filepath.Join(dir, "Kept", "cur", "m1"), countMsg("md one", "md1@x", "0001"))
	countWrite(t, filepath.Join(dir, "Kept", "cur", "m2"), countMsg("md deleted", "md2@x", "0009"))
	countWrite(t, filepath.Join(dir, "Kept", "new", "3"), countMsg("md three", "", ""))
	return dir
}

// walkCounting walks dir with counts (nil: none) and gives the subjects the handler saw.
func walkCounting(t *testing.T, dir string, counts *MailboxCounts) []string {
	t.Helper()
	r, err := openMboxDir(dir, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	err = r.WalkWithFaults(func(folder []string, m *model.Message) error {
		got = append(got, strings.Join(folder, "/")+": "+m.Subject)
		return nil
	}, WalkOptions{Faults: func(f Fault) { t.Errorf("unexpected fault: %+v", f) }, Counts: counts})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func idKey(id string) [16]byte {
	sum := sha256.Sum256([]byte(id))
	var k [16]byte
	copy(k[:], sum[:16])
	return k
}

// covers: MA-1173, MA-1175
// The walk's per-folder count (the completeness design §2.4, slice C2): each mailbox the walk
// reads is counted as its messages are read, by its place under the store as on disk; a copy
// Thunderbird marks deleted (the Expunged bit 0x0008 of X-Mozilla-Status) is left out of the
// count but still handed on, so it is still archived (Q7); with Distinct each counted message
// keeps its Message-ID key, and one with none counts once (Gmail's X, §3). A ".msf" entry in any
// letter case is noted by name. Positive twin first: without counts the walk hands on exactly
// the same messages, and with Count off it notes the ".msf" only.
func TestTheWalkCountsEachMailboxLeavingOutDeletedCopies(t *testing.T) {
	dir := countStore(t)
	plain := walkCounting(t, dir, nil)
	if len(plain) != 11 {
		t.Fatalf("positive twin: the store's 11 messages were not all read: %q", plain)
	}

	c := &MailboxCounts{Count: true, Distinct: true}
	counted := walkCounting(t, dir, c)
	if strings.Join(counted, "\n") != strings.Join(plain, "\n") {
		t.Fatalf("counting changed what the walk hands on:\nwith    %q\nwithout %q", counted, plain)
	}
	if !c.Msf {
		t.Error("the store's .msf files were not noted")
	}
	got := map[string]*MailboxCount{}
	for _, mb := range c.Mailboxes {
		if got[mb.Rel] != nil {
			t.Errorf("mailbox %q counted twice", mb.Rel)
		}
		got[mb.Rel] = mb
	}
	// Amended 2026-11-08 (the C2 pass's identity rule): a message with no Message-ID has a
	// content key ("" below), never a per-copy count; NoKey is 0 for every real message.
	// Amended 2026-11-08 (round 2's R2-04): INBOX's "plain", an mbox piece with no
	// X-Mozilla-Status, is loose (the lenient count only), its key kept apart; a maildir file
	// with none ("md three") is counted, as a maildir file has no separator to mistake.
	for _, w := range []struct {
		rel                       string
		messages, expunged, loose int64
		ids, looseIDs             []string
	}{
		{"INBOX", 2, 1, 1, []string{"one@x", "two@x"}, []string{"plain@x"}},
		{"[Gmail].sbd/All Mail", 3, 1, 0, []string{"one@x", "", ""}, nil},
		{"Kept", 2, 1, 0, []string{"", "md1@x"}, nil}, // new/ is read before cur/
	} {
		mb := got[w.rel]
		if mb == nil {
			t.Errorf("no count for %q (got %d mailboxes)", w.rel, len(c.Mailboxes))
			continue
		}
		if mb.Messages != w.messages || mb.Expunged != w.expunged || mb.Loose != w.loose || mb.NoKey != 0 || mb.LooseNoKey != 0 || mb.Unseparated != 0 {
			t.Errorf("%s: messages %d expunged %d loose %d noKey %d unseparated %d, want %d %d %d 0 0", w.rel, mb.Messages, mb.Expunged, mb.Loose, mb.NoKey, mb.Unseparated, w.messages, w.expunged, w.loose)
		}
		if len(mb.LooseKeys) != len(w.looseIDs) || (len(w.looseIDs) == 1 && mb.LooseKeys[0] != idKey(w.looseIDs[0])) {
			t.Errorf("%s: loose keys %d, want %q", w.rel, len(mb.LooseKeys), w.looseIDs)
		}
		if len(mb.Keys) != len(w.ids) {
			t.Errorf("%s: %d keys, want %d", w.rel, len(mb.Keys), len(w.ids))
			continue
		}
		for i, id := range w.ids {
			switch {
			case id != "" && mb.Keys[i] != idKey(id):
				t.Errorf("%s: key %d is not %q's key", w.rel, i, id)
			case id == "" && (mb.Keys[i] == [16]byte{} || mb.Keys[i] == idKey("")):
				t.Errorf("%s: key %d is no content key", w.rel, i)
			}
		}
	}
	if len(c.Mailboxes) != 3 {
		t.Errorf("%d mailboxes counted, want 3", len(c.Mailboxes))
	}

	// Count off: the ".msf" is noted, nothing is counted.
	only := &MailboxCounts{}
	walkCounting(t, dir, only)
	if !only.Msf || len(only.Mailboxes) != 0 {
		t.Errorf("with Count off: msf %v, %d mailboxes; want true, 0", only.Msf, len(only.Mailboxes))
	}

	// Without Distinct no key is kept.
	nd := &MailboxCounts{Count: true}
	walkCounting(t, dir, nd)
	for _, mb := range nd.Mailboxes {
		if len(mb.Keys) != 0 || mb.NoKey != 0 || len(mb.LooseKeys) != 0 || mb.LooseNoKey != 0 {
			t.Errorf("%s: keys kept without Distinct", mb.Rel)
		}
	}
}

// covers: MA-1173
// The Message-ID and status are read as header fields: a field name in any letter case, a
// value folded onto a second line, a status that is not hex (counted, as Thunderbird would not
// call it deleted), and "X-Mozilla-Status2" never read as X-Mozilla-Status. A summary file named
// in capitals (".MSF") is noted as ".msf" is (the M6-residual fold); a store with none is not.
func TestTheCountReadsHeaderFieldsAsFields(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	countWrite(t, filepath.Join(dir, "Box"), countMbox(
		"From: a@x\nmessage-id:\n <folded@x>\n\nbody\n",
		"From: a@x\nMessage-ID: <st2@x>\nX-Mozilla-Status2: 00000008\n\nbody\n",
		"From: a@x\nMessage-ID: <bad@x>\nX-Mozilla-Status: zz\n\nbody\n",
		"From: a@x\nMESSAGE-ID: <gone@x>\nx-mozilla-status: 0008\n\nbody\n"))
	none := &MailboxCounts{Count: true, Distinct: true}
	walkCounting(t, dir, none)
	if none.Msf {
		t.Error("a store with no .msf file was taken to hold one")
	}
	if len(none.Mailboxes) != 1 {
		t.Fatalf("%d mailboxes, want 1", len(none.Mailboxes))
	}
	// Amended 2026-11-08 (round 2's R2-04): the two with no X-Mozilla-Status (X-Mozilla-Status2
	// is not it) are loose, in the lenient count only; the one with a status that is not hex is
	// counted for certain.
	mb := none.Mailboxes[0]
	if mb.Rel != "Box" || mb.Messages != 1 || mb.Loose != 2 || mb.Expunged != 1 || mb.NoKey != 0 || mb.LooseNoKey != 0 {
		t.Errorf("Box: %+v; want 1 counted, 2 loose, 1 expunged, every one with an id", *mb)
	}
	if len(mb.Keys) != 1 || mb.Keys[0] != idKey("bad@x") || len(mb.LooseKeys) != 2 || mb.LooseKeys[0] != idKey("folded@x") || mb.LooseKeys[1] != idKey("st2@x") {
		t.Errorf("Box: the ids are not the unfolded folded@x, st2 and bad")
	}

	countWrite(t, filepath.Join(dir, "Box.MSF"), "")
	caps := &MailboxCounts{}
	walkCounting(t, dir, caps)
	if !caps.Msf {
		t.Error(`a summary file named "Box.MSF" was not noted`)
	}
}
