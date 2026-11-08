package health

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// Slice C3 on `mailarchive status` and MailArchive Desktop's Status & health
// (docs/design-thunderbird-completeness.md §5, §7, §6; TC-19's WARN once then a note).

func tbIn(lr state.LastRun) Input {
	if lr.Status == "" {
		lr.Status = state.RunOK
	}
	if lr.Finished.IsZero() {
		lr.Finished = time.Now().UTC()
		lr.Started = lr.Finished.Add(-time.Minute)
	}
	return Input{Out: "/archive", LastRun: lr, LastRunState: state.LastRunPresent, Surface: "desktop"}
}

func reasonsByCode(rep Report, code string) []string {
	var out []string
	for i, c := range rep.Codes {
		if c == code {
			out = append(out, rep.Reasons[i])
		}
	}
	return out
}

// covers: MA-1182, MA-1183
// status and Status & health (§5's Q3, §7): WARN thunderbird_behind from the first behind backup,
// with its remedy in the words (X6); a caught-up account is a note; one MailArchive cannot tell
// about is a note (thunderbird_unknown), never a WARN; Spam and Trash not kept, and Gmail's labels,
// notes only. The Last run line ends with the run's lead, built from the record through
// RunLeadWith with its thunderbird_more (R2-06). A record without completeness shows nothing of
// it. Positive twin first: the owner-shaped record is GREEN and its Last run line says "Export
// complete.".
func TestStatusSaysThunderbirdIsBehind(t *testing.T) {
	gmail := state.TBAccount{Account: "imap.gmail.com", Verdict: "caught_up", Complete: true, Gmail: true, OnDisk: 238169, Listed: 238169,
		DefaultNotKept: []state.TBFolder{{Folder: "[Gmail]/Spam", Listed: 3}}, LabelsNotKept: 40}
	other := state.TBAccount{Account: "imap.example.com", Verdict: "caught_up", Complete: true, OnDisk: 3402, Listed: 3402}
	in := tbIn(state.LastRun{Thunderbird: []state.TBAccount{gmail, other}})
	rep := Assess(in, time.Now())
	if w := tbWarns(rep); len(w) != 0 || len(reasonsByCode(rep, "thunderbird_caught_up")) == 0 {
		t.Fatalf("positive twin: the owner-shaped record WARNs %q", w)
	}
	if got := reasonsByCode(rep, "thunderbird_caught_up"); len(got) != 2 || got[0] != "Thunderbird has downloaded everything it lists for imap.gmail.com (238,169 messages)." {
		t.Errorf("the caught-up notes: %q", got)
	}
	if got := reasonsByCode(rep, "thunderbird_default_not_kept"); len(got) != 1 || !strings.HasPrefix(got[0], "Thunderbird does not keep Spam and Trash of imap.gmail.com") {
		t.Errorf("Spam and Trash: %q", got)
	}
	if got := reasonsByCode(rep, "thunderbird_labels_not_kept"); len(got) != 1 {
		t.Errorf("Gmail's labels: %q", got)
	}
	if line := lastRunLine(in, rep); !strings.HasSuffix(line, " · Export complete.") {
		t.Errorf("the Last run line: %q", line)
	}

	behind := state.TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1670, Listed: 14201}
	unknown := state.TBAccount{Account: "imap.other.example", Verdict: "unknown", Reason: "panorama"}
	in = tbIn(state.LastRun{Thunderbird: []state.TBAccount{gmail, behind, unknown}})
	rep = Assess(in, time.Now())
	if w := tbWarns(rep); len(w) != 1 || rep.Posture != "WARN" {
		t.Errorf("one behind: posture %s, WARNs %q", rep.Posture, w)
	}
	if got := reasonsByCode(rep, "thunderbird_behind"); len(got) != 1 || got[0] != "WARN: Thunderbird has downloaded about 1,670 of 14,201 messages in imap.example.com so far. Leave Thunderbird open (it downloads mostly while the computer is left alone). Run the backup again once Thunderbird has downloaded more, to add the rest." {
		t.Errorf("thunderbird_behind: %q", got)
	}
	if got := reasonsByCode(rep, "thunderbird_unknown"); len(got) != 1 || strings.HasPrefix(got[0], "WARN") || !strings.Contains(got[0], "newer storage") {
		t.Errorf("thunderbird_unknown: %q", got)
	}
	if line := lastRunLine(in, rep); !strings.HasSuffix(line, " · Backed up what Thunderbird has so far.") {
		t.Errorf("the Last run line: %q", line)
	}

	in = tbIn(state.LastRun{Thunderbird: []state.TBAccount{gmail}, ThunderbirdMore: 2})
	rep = Assess(in, time.Now())
	if got := reasonsByCode(rep, "thunderbird_more"); len(got) != 1 || !strings.Contains(got[0], "2 more Thunderbird accounts") {
		t.Errorf("accounts left out: %q", got)
	}
	if line := lastRunLine(in, rep); !strings.HasSuffix(line, " · Backup finished.") {
		t.Errorf("the Last run line with accounts left out: %q", line)
	}

	none := tbIn(state.LastRun{})
	rep = Assess(none, time.Now())
	for _, c := range rep.Codes {
		if strings.HasPrefix(c, "thunderbird_") {
			t.Errorf("a record without completeness says %s", c)
		}
	}
	if line := lastRunLine(none, rep); strings.Contains(line, "Thunderbird") || strings.Contains(line, "complete") {
		t.Errorf("a record without completeness: %q", line)
	}
	failed := tbIn(state.LastRun{Status: state.RunFailed, Error: "x", Thunderbird: []state.TBAccount{behind}})
	if got := reasonsByCode(Assess(failed, time.Now()), "thunderbird_behind"); len(got) != 0 {
		t.Errorf("a failed run's record says %q", got)
	}
	enc := tbIn(state.LastRun{Encrypted: true, Thunderbird: []state.TBAccount{behind}})
	for _, c := range Assess(enc, time.Now()).Codes {
		if strings.HasPrefix(c, "thunderbird_") {
			t.Errorf("an encrypted archive's record says %s", c)
		}
	}
}

// covers: MA-1182, MA-1183
// A setting the person chose (TC-19, L3): a limited account and a not-kept folder are a WARN on
// the run that first shows them (the record's since equals the run's end), then a note while it is
// unchanged; a changed setting WARNs again (the record's since moves to that run, C2's
// carry-forward). Each names its remedy (X6). Positive twin first: the first run WARNs.
func TestAChosenSettingWarnsOnceThenNotes(t *testing.T) {
	fin := time.Now().UTC()
	a := state.TBAccount{Account: "imap.example.com", Verdict: "limited", OnDisk: 900, Listed: 5000, Limit: &state.TBLimit{MaxAgeDays: 30},
		LimitSince: fin, Folders: []state.TBFolder{{Folder: "INBOX", Listed: 5000, OnDisk: 900}},
		NotKept: []state.TBFolder{{Folder: "Projects", Listed: 772}}, NotKeptSince: fin, ThunderbirdVersion: 153}
	first := Assess(tbIn(state.LastRun{Finished: fin, Started: fin.Add(-time.Minute), Thunderbird: []state.TBAccount{a}}), time.Now())
	lim, nk := reasonsByCode(first, "thunderbird_limited"), reasonsByCode(first, "thunderbird_not_kept")
	if len(lim) != 1 || !strings.HasPrefix(lim[0], "WARN: Thunderbird has downloaded about 900 of 5,000 messages in imap.example.com. Thunderbird is set to keep only the most recent 30 days") ||
		!strings.Contains(lim[0], `choose "Synchronize all messages locally regardless of age".`) {
		t.Fatalf("positive twin: the first run's limited WARN: %q", lim)
	}
	if len(nk) != 1 || !strings.HasPrefix(nk[0], "WARN: This folder of imap.example.com is not kept on this computer") || !strings.Contains(nk[0], "To keep it:") {
		t.Fatalf("positive twin: the first run's not-kept WARN: %q", nk)
	}
	a.LimitSince, a.NotKeptSince = fin.Add(-24*time.Hour), fin.Add(-24*time.Hour)
	next := Assess(tbIn(state.LastRun{Finished: fin, Started: fin.Add(-time.Minute), Thunderbird: []state.TBAccount{a}}), time.Now())
	lim, nk = reasonsByCode(next, "thunderbird_limited"), reasonsByCode(next, "thunderbird_not_kept")
	if len(lim) != 1 || strings.HasPrefix(lim[0], "WARN") || len(nk) != 1 || strings.HasPrefix(nk[0], "WARN") {
		t.Errorf("the same setting on the next run: %q %q", lim, nk)
	}
	if w := tbWarns(next); len(w) != 0 {
		t.Errorf("a chosen setting seen before WARNs: %q", w)
	}
	a.LimitSince = time.Time{}
	if lim := reasonsByCode(Assess(tbIn(state.LastRun{Finished: fin, Thunderbird: []state.TBAccount{a}}), time.Now()), "thunderbird_limited"); len(lim) != 1 || !strings.HasPrefix(lim[0], "WARN") {
		t.Errorf("a record with no first-seen time: %q", lim)
	}
}

// covers: MA-1183
// `status -json` (§7's -json folds): the plaintext record's accounts as thunderbird_completeness,
// with complete and stale in every entry (M9, always present), thunderbird_more, and the run's lead
// as thunderbird_lead; the new reason codes in reason_codes; nothing renamed. A record without
// completeness, a failed run's and an encrypted archive's show none of these keys. Positive twin
// first: the keys are there for a plaintext ok record.
func TestStatusJSONCarriesTheCompleteness(t *testing.T) {
	behind := state.TBAccount{Account: "imap.example.com", Verdict: "behind", OnDisk: 1670, Listed: 14201, Missing: 12531, Tolerance: 28}
	in := tbIn(state.LastRun{Thunderbird: []state.TBAccount{behind}, ThunderbirdMore: 1})
	doc := marshal(t, JSON(in, Assess(in, time.Now())))
	tc, ok := doc["thunderbird_completeness"].([]any)
	if !ok || len(tc) != 1 {
		t.Fatalf("positive twin: thunderbird_completeness: %v", doc["thunderbird_completeness"])
	}
	e := tc[0].(map[string]any)
	for k, want := range map[string]any{"account": "imap.example.com", "verdict": "behind", "complete": false, "stale": false, "on_disk": 1670.0, "listed": 14201.0, "missing": 12531.0, "tolerance": 28.0} {
		if e[k] != want {
			t.Errorf("entry %s: %v, want %v", k, e[k], want)
		}
	}
	if doc["thunderbird_more"] != 1.0 || doc["thunderbird_lead"] != "so_far" {
		t.Errorf("thunderbird_more %v, thunderbird_lead %v", doc["thunderbird_more"], doc["thunderbird_lead"])
	}
	if codes, _ := doc["reason_codes"].([]any); !containsAny(codes, "thunderbird_behind") || !containsAny(codes, "thunderbird_more") {
		t.Errorf("reason_codes: %v", codes)
	}
	for name, lr := range map[string]state.LastRun{
		"no completeness": {},
		"a failed run":    {Status: state.RunFailed, Error: "x", Thunderbird: []state.TBAccount{behind}},
		"encrypted":       {Encrypted: true, Thunderbird: []state.TBAccount{behind}},
	} {
		in := tbIn(lr)
		doc := marshal(t, JSON(in, Assess(in, time.Now())))
		for _, k := range []string{"thunderbird_completeness", "thunderbird_more", "thunderbird_lead"} {
			if _, has := doc[k]; has {
				t.Errorf("%s: -json carries %s", name, k)
			}
		}
	}
}

// covers: MA-1182, MA-1198
// Status & health is read on a phone: its completeness words say no "store", "IMAP", "cache" or
// "msf" (the slice's brief; the account's own name aside), and a planted record's names reach it
// cleaned and capped (TC-7). Positive twin first: ordinary names as they are.
func TestStatusWordsArePlainAndNamesAreCleaned(t *testing.T) {
	accts := []state.TBAccount{
		{Account: "ACCOUNT", Verdict: "behind", OnDisk: 1, Listed: 9, Folders: []state.TBFolder{{Folder: "INBOX", Listed: 9, OnDisk: 1}}},
		{Account: "ACCOUNT", Verdict: "limited", OnDisk: 1, Listed: 9, Limit: &state.TBLimit{MaxAgeDays: 3, MaxSizeKB: 9, NoBackgroundDownload: true}},
		{Account: "ACCOUNT", Verdict: "caught_up", CapNote: 1000, OnDisk: 1000, Listed: 1000, NotKept: []state.TBFolder{{Folder: "Projects", Listed: 2}}, DefaultNotKept: []state.TBFolder{{Folder: "Trash", Listed: 1}}, LabelsNotKept: 2},
	}
	for _, reason := range []string{"cache_unreadable", "no_match", "duplicate_key", "invalid_entry", "panorama", "account_type", "never_checked", "gmail_floor",
		"expunged_unsized", "partial_input", "old_path_only", "not_counted_yet", "split_differently", "other_reader", "walk_failed", "count_uncertain"} {
		accts = append(accts, state.TBAccount{Account: "ACCOUNT", Verdict: "unknown", Reason: reason})
	}
	rep := Assess(tbIn(state.LastRun{Thunderbird: accts, ThunderbirdMore: 1}), time.Now())
	n := 0
	for i, c := range rep.Codes {
		if !strings.HasPrefix(c, "thunderbird_") {
			continue
		}
		n++
		for _, w := range []string{"store", "IMAP", "imap", "cache", "msf"} {
			if strings.Contains(rep.Reasons[i], w) {
				t.Errorf("%s says %q: %q", c, w, rep.Reasons[i])
			}
		}
	}
	if n < len(accts) {
		t.Fatalf("positive twin: only %d completeness reasons for %d accounts", n, len(accts))
	}

	hostile := state.TBAccount{Account: "imap.\u202eevil\x1b.com", Verdict: "behind", OnDisk: 1, Listed: 9,
		Folders: []state.TBFolder{{Folder: "IN\u202eBOX\x07" + strings.Repeat("q", 10*1024), Listed: 9, OnDisk: 1}}, NotKept: []state.TBFolder{{Folder: "\u2066Pro\u200bjects", Listed: 3}}}
	all := strings.Join(Assess(tbIn(state.LastRun{Thunderbird: []state.TBAccount{hostile}}), time.Now()).Reasons, "\n")
	for _, bad := range []string{"\u202e", "\x1b", "\x07", "\u2066", "\u200b", strings.Repeat("q", 80)} {
		if strings.Contains(all, bad) {
			t.Errorf("status holds %q", bad)
		}
	}
	for _, good := range []string{"imap.evil.com", "INBOX" + strings.Repeat("q", 75) + "… 1 of 9", "Projects (3 messages)"} {
		if !strings.Contains(all, good) {
			t.Errorf("status misses %q:\n%s", good, all)
		}
	}
}

// tbWarns are the report's completeness WARNs.
func tbWarns(rep Report) []string {
	var out []string
	for i, c := range rep.Codes {
		if strings.HasPrefix(c, "thunderbird_") && strings.HasPrefix(rep.Reasons[i], "WARN") {
			out = append(out, rep.Reasons[i])
		}
	}
	return out
}

// lastRunLine is the Summary's Last run line.
func lastRunLine(in Input, rep Report) string {
	for _, l := range Summary(in, rep) {
		if strings.HasPrefix(l, "Last run:") {
			return l
		}
	}
	return ""
}

func marshal(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func containsAny(xs []any, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
