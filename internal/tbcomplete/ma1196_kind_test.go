package tbcomplete

import (
	"testing"

	"mail-archive-tool/internal/thunderbird"
)

// covers: MA-1196
// The kind of account decides first (§2.2's TC-5, M6 and M6-residual folds, by the matched
// account's type, never by the path): a matched imap account is checked; pop3, none (Local Folders)
// and rss are not_applicable whatever else is known; ews, any other type and an empty type are
// unknown (account_type); no matched account is unknown (no_match), except a plain mail folder, with
// no profile found, no ImapMail part in its path and no .msf file, which is not_applicable. Then a
// partial input is unknown (partial_input, TC-11), and a Gmail answer that cannot be told is
// unknown (no_match: GmailForStore can't tell only when it has no account). An Input nobody filled
// is never caught up and never not_applicable. Positive twin first.
func TestTheAccountsTypeDecidesFirst(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)))
	disk := map[string]int64{"INBOX": 10}
	want(t, "the positive twin: a matched imap account", Assess(healthy(s, disk)), CaughtUp, NoReason)

	for _, typ := range []string{"pop3", "none", "rss"} {
		in := healthy(s, nil)
		in.Type, in.WholeStore, in.Gmail, in.CacheErr = typ, false, thunderbird.GmailCantTell, &CacheError{Kind: CacheMissing}
		r := Assess(in)
		want(t, typ, r, NotApplicable, NoReason)
		if !r.MaySayComplete() {
			t.Fatalf("%s: not_applicable may say complete", typ)
		}
	}
	for _, typ := range []string{"ews", "", "nntp", "IMAP", "imap "} {
		in := healthy(s, disk)
		in.Type = typ
		want(t, "type "+typ, Assess(in), Unknown, ReasonAccountType)
	}

	t.Run("no matched account", func(t *testing.T) {
		in := healthy(s, disk)
		in.Matched, in.Type = false, ""
		want(t, "no account, a profile", Assess(in), Unknown, ReasonNoMatch)
		in.Unmatched = Unmatched{NoProfile: true, NoImapMailPart: true, NoMsf: true}
		want(t, "a plain mail folder", Assess(in), NotApplicable, NoReason)
		for name, u := range map[string]Unmatched{
			"a profile above it":        {NoImapMailPart: true, NoMsf: true},
			"an ImapMail part":          {NoProfile: true, NoMsf: true},
			"a .msf file (M6 residual)": {NoProfile: true, NoImapMailPart: true},
		} {
			in.Unmatched = u
			want(t, name, Assess(in), Unknown, ReasonNoMatch)
		}
		in = healthy(s, disk)
		in.Type = "pop3"
		in.Matched = false
		want(t, "a type with no account is not an account's type", Assess(in), Unknown, ReasonNoMatch)
	})

	t.Run("then the partial input and Gmail's answer", func(t *testing.T) {
		in := healthy(s, disk)
		in.WholeStore = false
		want(t, "a partial input", Assess(in), Unknown, ReasonPartialInput)
		in = healthy(s, disk)
		in.Gmail = thunderbird.GmailCantTell
		want(t, "Gmail can't tell", Assess(in), Unknown, ReasonNoMatch)
		in.Gmail = thunderbird.Gmail(7)
		want(t, "a Gmail answer nobody defined", Assess(in), Unknown, ReasonNoMatch)
	})

	t.Run("an Input nobody filled", func(t *testing.T) {
		r := Assess(Input{})
		if r.Verdict != Unknown || r.MaySayComplete() {
			t.Fatalf("the zero Input: %+v", r)
		}
		var zero Result
		if zero.Verdict != Unknown || zero.MaySayComplete() || RunLead([]Result{zero}) == LeadComplete {
			t.Fatalf("the zero Result says complete")
		}
	})

	t.Run("the identifiers are the design's", func(t *testing.T) {
		for v, s := range map[Verdict]string{Unknown: "unknown", CaughtUp: "caught_up", Behind: "behind", Limited: "limited", NotApplicable: "not_applicable", Verdict(99): "unknown"} {
			if v.String() != s {
				t.Errorf("%d: %q, want %q", int(v), v.String(), s)
			}
		}
		for r, s := range map[Reason]string{
			NoReason: "", ReasonCacheUnreadable: "cache_unreadable", ReasonNoMatch: "no_match", ReasonDuplicateKey: "duplicate_key",
			ReasonInvalidEntry: "invalid_entry", ReasonPanorama: "panorama", ReasonAccountType: "account_type",
			ReasonNeverChecked: "never_checked", ReasonGmailFloor: "gmail_floor", ReasonExpungedUnsized: "expunged_unsized",
			ReasonPartialInput: "partial_input", ReasonOldPathOnly: "old_path_only", ReasonNotCountedYet: "not_counted_yet",
			// The C2 pass's codes (amended 2026-11-08).
			ReasonSplitDifferently: "split_differently", ReasonOtherReader: "other_reader", ReasonWalkFailed: "walk_failed",
			// Round 2's code (amended 2026-11-08, R2-04).
			ReasonCountUncertain: "count_uncertain", Reason(99): "", Reason(17): "", Reason(-1): "",
		} {
			if r.String() != s {
				t.Errorf("%d: %q, want %q", int(r), r.String(), s)
			}
		}
	})
}
