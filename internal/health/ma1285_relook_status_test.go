package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1285
// R5: `status` and Status & health say a behind account's remedy with "Click Run now again" when
// the archive's How often is "Only when I click Run now" (its manual_backup note already knows it),
// so the page's Run now note and Status & health name one action one way. Positive twin first: an
// archive with no schedule recorded says "Run the backup again"; a daily one, "later backups".
func TestStatusSaysClickRunNowForARunNowOnlyArchive(t *testing.T) {
	behind := state.TBAccount{Account: "imap.fastmail.com", Name: "sam@fastmail.com", Verdict: "behind", OnDisk: 300, Listed: 1200}
	const head = "WARN: Thunderbird has downloaded about 300 of 1,200 messages in sam@fastmail.com so far. Leave Thunderbird open (it downloads mostly while the computer is left alone)"
	in := tbIn(state.LastRun{Thunderbird: []state.TBAccount{behind}})
	if got := reasonsByCode(Assess(in, time.Now()), "thunderbird_behind"); len(got) != 1 || got[0] != head+". Run the backup again once Thunderbird has downloaded more, to add the rest." {
		t.Fatalf("positive twin, no schedule: %q", got)
	}
	in.HasDescriptor, in.Desc.Interval = true, string(schedule.Daily)
	if got := reasonsByCode(Assess(in, time.Now()), "thunderbird_behind"); len(got) != 1 || got[0] != head+"; later backups add the rest." {
		t.Fatalf("positive twin, daily: %q", got)
	}
	in.Desc.Interval = string(schedule.Manual)
	if got := reasonsByCode(Assess(in, time.Now()), "thunderbird_behind"); len(got) != 1 || got[0] != head+". Click Run now again once Thunderbird has downloaded more, to add the rest." {
		t.Errorf("Run now only: %q", got)
	}
}

// covers: MA-1285
// R1 on `status` and Status & health: every WARN is said before the notes, so the Gmail floor's
// note says which line keeps All Mail, never "see below" (the not-kept folder's WARN is above it).
func TestStatusNamesTheAllMailLine(t *testing.T) {
	floor := state.TBAccount{Account: "imap.gmail.com", Name: "sam.walker@gmail.com", Verdict: "unknown", Reason: "gmail_floor", Gmail: true, Floor: true,
		AllMail: "not_kept", OnDisk: 50, Listed: 40, NotKept: []state.TBFolder{{Folder: "[Gmail]/All Mail", Listed: 900}}, Folders: []state.TBFolder{{Folder: "INBOX", Listed: 50, OnDisk: 50}}}
	rep := Assess(tbIn(state.LastRun{Thunderbird: []state.TBAccount{floor}}), time.Now())
	note, warn := reasonsByCode(rep, "thunderbird_unknown"), reasonsByCode(rep, "thunderbird_not_kept")
	if len(warn) != 1 || !strings.Contains(warn[0], "[Gmail]/All Mail (900 messages)") {
		t.Fatalf("the not-kept WARN: %q", warn)
	}
	if len(note) != 1 || !strings.HasSuffix(note[0], "is not kept on this computer (the line that names [Gmail]/All Mail says how to keep it).") || strings.Contains(note[0], "below") {
		t.Errorf("the floor note: %q", note)
	}
}
