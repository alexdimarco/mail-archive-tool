package tbcomplete

import (
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1181
// Slice C3's half of the lead: its words, and the lead of a run read back from its record
// (RecordLead, through RunLeadWith with the record's thunderbird_more, the C2 pass round 2's
// R2-06). Positive twin first: the owner-shaped record (Gmail caught up, another account caught
// up; Local Folders never recorded) leads "Export complete.". Then: one behind leads "Backed up
// what Thunderbird has so far." whatever else; caught up beside unknown, or a caught-up account
// that may not say complete, leads "Backup finished."; an account left out of the record is never
// complete; a record that says complete over its own numbers, or holds not_applicable or a verdict
// nobody defined, never leads complete; no Thunderbird account keeps today's "Export complete.".
func TestTheLeadOfARecord(t *testing.T) {
	ok := func(name string, n int64) state.TBAccount {
		return state.TBAccount{Account: name, Verdict: "caught_up", Complete: true, OnDisk: n, Listed: n}
	}
	owner := []state.TBAccount{ok("imap.gmail.com", 238169), ok("imap.example.com", 3402)}
	if l := RecordLead(owner, 0); l != LeadComplete || l.Sentence() != "Export complete." {
		t.Fatalf("positive twin: the owner-shaped record: %v %q", l, l.Sentence())
	}
	if l := RecordLead(nil, 0); l.Sentence() != "Export complete." {
		t.Errorf("no Thunderbird account: %q", l.Sentence())
	}
	behind := state.TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1670, Listed: 14201}
	limited := state.TBAccount{Account: "imap.example.com", Verdict: "limited"}
	unknown := state.TBAccount{Account: "imap.example.com", Verdict: "unknown", Reason: "cache_unreadable"}
	missing := ok("imap.example.com", 3402)
	missing.Missing, missing.Complete = 1, false
	planted := ok("imap.example.com", 3402)
	planted.Missing = 7 // complete:true over its own numbers
	notApplicable := ok("Local Folders", 0)
	notApplicable.Verdict = "not_applicable"
	odd := ok("imap.example.com", 1)
	odd.Verdict = "caught-up"

	for _, c := range []struct {
		what  string
		accts []state.TBAccount
		more  int
		want  string
	}{
		{"one behind", []state.TBAccount{owner[0], behind}, 0, "Backed up what Thunderbird has so far."},
		{"behind before an unknown", []state.TBAccount{behind, unknown}, 0, "Backed up what Thunderbird has so far."},
		{"limited", []state.TBAccount{owner[0], limited}, 0, "Backed up what Thunderbird has so far."},
		{"caught up beside unknown", []state.TBAccount{owner[0], unknown}, 0, "Backup finished."},
		{"caught up with 1 missing", []state.TBAccount{owner[0], missing}, 0, "Backup finished."},
		{"complete over its own numbers", []state.TBAccount{planted}, 0, "Backup finished."},
		{"not_applicable in a record", []state.TBAccount{notApplicable}, 0, "Backup finished."},
		{"a verdict nobody defined", []state.TBAccount{odd}, 0, "Backup finished."},
		{"accounts left out", owner, 1, "Backup finished."},
		{"only accounts left out", nil, 3, "Backup finished."},
		{"behind and accounts left out", []state.TBAccount{behind}, 2, "Backed up what Thunderbird has so far."},
	} {
		if got := RecordLead(c.accts, c.more).Sentence(); got != c.want {
			t.Errorf("%s: %q, want %q", c.what, got, c.want)
		}
	}
	if Lead(99).Sentence() != "Backup finished." || Lead(-1).Sentence() != "Backup finished." {
		t.Error("a lead nobody defined says more than Backup finished.")
	}
}

// covers: MA-1185
// The version that last opened the profile reaches the result, for the fix-it words' labels
// (§6's L5 fold); a version that cannot be told is 0, and the words then quote both sets.
// Positive twin first: a known version is carried.
func TestTheVersionReachesTheResult(t *testing.T) {
	in := Input{Account: "imap.example.com", Matched: true, Type: "imap", Profile: Profile{VersionMajor: 153, VersionKnown: true}}
	if r := Assess(in); r.Version != 153 {
		t.Fatalf("positive twin: version %d, want 153", r.Version)
	}
	in.Profile = Profile{VersionMajor: 160, VersionKnown: false}
	if r := Assess(in); r.Version != 0 {
		t.Errorf("a version that cannot be told: %d, want 0", r.Version)
	}
}
