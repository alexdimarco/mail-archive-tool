package tbcomplete

import (
	"testing"
)

// covers: MA-1176
// Gmail with All Mail that cannot set Y (§3's TC-1 and M5 folds, owner ruling O3, foldcheck N4):
// All Mail sets Y only when it is the one folder with 0x8000, kept, checked (lastSyncTimeInSec > 0
// and not in the future, and listed by the server: the ImapBox flag, C1A-07) and listing at least
// the largest other kept folder's count less that folder's tolerance (N4). Otherwise Y is a floor
// ("at least"): the largest kept, checked folder other than Spam and Trash, plus Spam and Trash when
// kept. A floor is never caught up: X < floor − the floor's tolerance is behind, anything else
// unknown with reason gmail_floor. Positive twin first: All Mail full and checked sets Y.
func TestGmailFloorIsNeverCaughtUp(t *testing.T) {
	r := Assess(gmail(gmailStore(t, 1000, 500, 300), 1000))
	want(t, "the positive twin", r, CaughtUp, NoReason)
	if r.Floor || r.AllMail != AllMailSetsY || r.Listed != 1000 {
		t.Fatalf("the positive twin: %+v", r)
	}

	floor := func(what string, s Store, x int64, use AllMailUse, floorY int64) {
		t.Helper()
		r := Assess(gmail(s, x))
		if r.Verdict == CaughtUp || r.MaySayComplete() {
			t.Fatalf("%s: caught up on a floor: %+v", what, r)
		}
		if !r.Floor || r.AllMail != use || r.Listed != floorY {
			t.Fatalf("%s: floor %v, All Mail %v, Y %d; want a floor of %d, %v", what, r.Floor, r.AllMail, r.Listed, floorY, use)
		}
		want(t, what, r, Unknown, ReasonGmailFloor)
	}
	noAllMail := vstore(t, at("INBOX", folder(FlagInbox, 500, 500)), at("Label", folder(0, 300, 300)))
	floor("All Mail hidden, everything listed on disk", noAllMail, 1000, AllMailNotShown, 500)
	floor("All Mail not kept", gmailStore(t, 1000, 500, 300).withFlags(t, "[Gmail]/All Mail", FlagAllMail|FlagArchive|FlagImapBox), 1000, AllMailNotKept, 500)
	floor("All Mail never checked", gmailStore(t, 1000, 500, 300).withSync(t, "[Gmail]/All Mail", 0), 1000, AllMailNotChecked, 500)
	floor("All Mail checked in the future", gmailStore(t, 1000, 500, 300).withSync(t, "[Gmail]/All Mail", vNow.Unix()+1), 1000, AllMailNotChecked, 500)
	floor("All Mail not listed by the server (no ImapBox) though checked", gmailStore(t, 1000, 500, 300).withFlags(t, "[Gmail]/All Mail", FlagAllMail|FlagArchive|FlagOffline), 1000, AllMailNotChecked, 500)
	floor("All Mail smaller than a label by more than the label's tolerance", gmailStore(t, 400, 500, 300), 1000, AllMailBelowLargest, 500)
	floor("two folders with 0x8000", gmailStore(t, 1000, 500, 300, at("Other", folder(FlagAllMail, 900, 900))), 1000, AllMailNotShown, 1000)

	t.Run("an Archives label with 0x4000 only never stands in for a hidden All Mail (M5)", func(t *testing.T) {
		s := vstore(t, at("Archives", folder(FlagArchive, 1000, 1000)), at("[Gmail]/Sent Mail", folder(FlagSentMail, 200, 200)))
		floor("Archives only, all its mail on disk, Sent nothing", s, 1000, AllMailNotShown, 1000)
	})

	t.Run("X above the floor with a large label empty on disk is unknown, never caught up", func(t *testing.T) {
		s := vstore(t, at("INBOX", folder(FlagInbox, 6000, 6000)), at("Big label", folder(0, 5000, 5000)))
		floor("INBOX 6,000 on disk, a label of 5,000 empty", s, 6000, AllMailNotShown, 6000)
	})

	t.Run("a folder never checked never raises the floor (R43)", func(t *testing.T) {
		s := vstore(t, at("INBOX", folder(FlagInbox, 500, 500)), at("Big label", folder(0, 5000, 5000).with("lastSyncTimeInSec", nil)))
		floor("All Mail hidden, a label of 5,000 never checked", s, 500, AllMailNotShown, 500)
		r := Assess(gmail(gmailStore(t, 1200, 500, 300, at("Big label", folder(0, 5000, 5000).with("lastSyncTimeInSec", nil))), 1200))
		want(t, "All Mail full, a label of 5,000 never checked", r, Unknown, ReasonNeverChecked)
		if r.Floor || r.AllMail != AllMailSetsY || r.Listed != 1200 {
			t.Fatalf("All Mail full, a label never checked: %+v", r)
		}
	})

	t.Run("N4: the tolerance is the largest other folder's", func(t *testing.T) {
		// The largest other folder lists 150 (tolerance 15); All Mail 135 sets Y. All Mail's own
		// tolerance (13) would not let 135 reach 150 − 13.
		r := Assess(gmail(gmailStore(t, 135, 150, 10), 150))
		want(t, "All Mail 135, INBOX 150", r, CaughtUp, NoReason)
		if r.AllMail != AllMailSetsY || r.Listed != 150 {
			t.Fatalf("All Mail 135, INBOX 150: %+v", r)
		}
		floor("All Mail 134, INBOX 150", gmailStore(t, 134, 150, 10), 150, AllMailBelowLargest, 150)
		// Spam and Trash are never the largest other folder.
		s := gmailStore(t, 1000, 500, 300, at("[Gmail]/Trash", folder(FlagTrash, 5000, 5000)))
		r = Assess(gmail(s, 6000))
		want(t, "Trash larger than All Mail", r, CaughtUp, NoReason)
		if r.AllMail != AllMailSetsY || r.Listed != 6000 {
			t.Fatalf("Trash larger than All Mail: %+v", r)
		}
	})

	t.Run("the floor's own tolerance decides behind", func(t *testing.T) {
		s := vstore(t, at("INBOX", folder(FlagInbox, 150, 150)), at("[Gmail]/Spam", folder(FlagJunk, 10, 10)))
		// Floor 150 + kept Spam 10 = 160, tolerance 16.
		floor("X 144 of at least 160", s, 144, AllMailNotShown, 160)
		r := Assess(gmail(s, 143))
		want(t, "X 143 of at least 160", r, Behind, NoReason)
		if !r.Floor || r.Listed != 160 || r.Tolerance != 16 || r.Missing != 17 {
			t.Fatalf("X 143 of at least 160: %+v", r)
		}
		want(t, "nothing on disk, a floor", Assess(gmail(s, 0)), Behind, NoReason)
	})

	t.Run("an All Mail entry that is kept but not valid is never counted", func(t *testing.T) {
		s := vstore(t, at("INBOX", folder(FlagInbox, 500, 500)), at("[Gmail]/All Mail", folder(FlagAllMail|FlagArchive, 1000, 1000).with("serverTotal", "1000")))
		if s.AllMail.Kind != AllMailOne || s.AllMail.Entry.Valid() {
			t.Fatalf("the fixture: %+v", s.AllMail)
		}
		want(t, "All Mail invalid", Assess(gmail(s, 1000)), Unknown, ReasonInvalidEntry)
	})
}

// withFlags is s with the folder at path given flags.
func (s Store) withFlags(t *testing.T, path string, flags uint32) Store {
	t.Helper()
	return s.change(t, path, func(e *Entry) { e.Flags.Value = int64(flags) })
}

// withSync is s with the folder at path given lastSyncTimeInSec n.
func (s Store) withSync(t *testing.T, path string, n int64) Store {
	t.Helper()
	return s.change(t, path, func(e *Entry) { e.LastSyncTimeInSec = Field{State: OK, Value: n} })
}

// change is s, a copy, with the folder at path changed by f, its All Mail read again as the reader
// reads it.
func (s Store) change(t *testing.T, path string, f func(*Entry)) Store {
	t.Helper()
	out := s
	out.Folders = append([]Entry(nil), s.Folders...)
	found := false
	for i := range out.Folders {
		if out.Folders[i].Path == path {
			f(&out.Folders[i])
			found = true
		}
	}
	if !found {
		t.Fatalf("no folder %q", path)
	}
	out.AllMail = allMail(out.Folders, nil)
	return out
}
