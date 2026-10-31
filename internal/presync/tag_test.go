package presync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Recent backups tag's phrases, written out from design-presync-attended §4.5.
var wantTag = map[string]string{
	"ok":                         "Thunderbird synced first",
	"watch-truncated":            "Thunderbird synced first",
	"ok-noactivity":              "Thunderbird started, but nothing arrived in 20 min",
	"watch-truncated-noactivity": "Thunderbird started, but nothing arrived in 20 min",
	"not-quiet":                  "Thunderbird synced (still fetching at 20 min)",
	"wait-ended":                 "Thunderbird sync stopped early, as you asked",
	"quit-not-permitted":         "Thunderbird synced, but was left open",
	"quit-timeout":               "Thunderbird synced, but was left open",
	"quit-skipped-others":        "Thunderbird synced, but was left open",
	"quit-no-window":             "Thunderbird synced, but was left open",
	"owner-active":               "Thunderbird synced, but was left open",
	"identity-unavailable":       "Thunderbird synced, but was left open",
	"already-running":            "Thunderbird was already open",
	"owner-active-skipped":       "Thunderbird not synced: you were using the computer",
	"other-profile-running":      "Thunderbird not synced: open with another profile",
	"profile-busy":               "Thunderbird not synced: another backup was syncing it",
	"leftover-found":             "Thunderbird not synced: an earlier copy is still open",
	"app-not-found":              "Thunderbird not synced: it was not found", // the friction review's O6, applied 2026-10-31
	"refused-elevated":           "Thunderbird not synced: the backup ran as administrator",
	"profile-unusable":           "Thunderbird not synced: its profile could not be used",
	"cannot-start":               "Thunderbird not synced: it could not be started",
	"did-not-start":              "Thunderbird not synced: it could not be started",
	"exited-early":               "Thunderbird not synced: it closed before its mail stopped arriving", // the wording pass before 1.5.2: it did start
	"successor-found":            "Thunderbird not synced: the copy that was started went away",        // the wording pass before 1.5.2: it did start
	"start-blocked":              "Thunderbird not synced: it could not be started",
	"no-desktop-session":         "Thunderbird not synced: no one was logged in",
	"not-set-up":                 "Thunderbird not synced: the sync could not be prepared",
	"internal-error":             "Thunderbird not synced",
	"cancelled":                  "",
	"":                           "",
}

// sideOnly are the outcomes that are only ever side warnings, never a session's
// main outcome (A6, A9, C-45): deliberately not in the tag map, so a record that
// somehow names one reads as an unknown name.
var sideOnly = map[string]bool{"state-unreadable": true, "state-unsaved": true}

// covers: MA-905, MA-925, X10
// The tag's one closed map (design-presync-attended §4.5, C-28, C-37, C-60, C-65,
// C-68, OQ-6): every name gets §4.5's phrase — the no-change phrase for ok-noactivity
// and watch-truncated-noactivity (never "synced first"), quit-no-window, start-blocked
// and wait-ended included; cancelled and an absent name get no tag; every outcome the
// session can end in is in the map or deliberately has no tag (the side-only ones read
// as unknown); an unknown or hostile name reads "Thunderbird not synced" and is never
// echoed. No phrase says "see Status".
func TestTheTagPhrases(t *testing.T) {
	for name, want := range wantTag {
		if got := TagPhrase(name); got != want {
			t.Errorf("TagPhrase(%q) = %q, want %q", name, got, want)
		}
	}
	for _, o := range Outcomes() {
		if _, ok := wantTag[o.Name]; !ok && !sideOnly[o.Name] {
			t.Errorf("the outcome %q has no phrase in §4.5's map and is not side-only", o.Name)
		}
		if sideOnly[o.Name] && TagPhrase(o.Name) != "Thunderbird not synced" {
			t.Errorf("side-only %q must read as an unknown name, got %q", o.Name, TagPhrase(o.Name))
		}
	}
	if len(Outcomes()) != len(allOutcomes) {
		t.Fatalf("Outcomes() lists %d, the vocabulary %d", len(Outcomes()), len(allOutcomes))
	}
	for _, p := range []string{TagPhrase("ok-noactivity"), TagPhrase("watch-truncated-noactivity")} {
		if strings.Contains(p, "synced") {
			t.Errorf("a no-change phrase says synced: %q", p)
		}
	}
	for _, hostile := range []string{"evil-name", "\x1b[31mok", "ok ", "OK", "<script>alert(1)</script>", "quit-*", "presync_ok"} {
		got := TagPhrase(hostile)
		if got != "Thunderbird not synced" {
			t.Errorf("TagPhrase(%q) = %q, want the generic phrase", hostile, got)
		}
		if strings.Contains(got, strings.TrimSpace(hostile)) {
			t.Errorf("TagPhrase echoed %q", hostile)
		}
	}
	for name := range wantTag {
		if strings.Contains(strings.ToLower(TagPhrase(name)), "see status") {
			t.Errorf("the phrase for %q says see Status", name)
		}
	}
	// Outcomes() is a copy: changing it changes nothing.
	list := Outcomes()
	list[0] = Outcome{Name: "changed"}
	if Outcomes()[0].Name == "changed" {
		t.Error("Outcomes() hands out the vocabulary itself")
	}
}

// covers: MA-903
// state-unreadable is never a session's main outcome (A6, C-45): a session whose
// state record cannot be read goes on and ends with its own outcome, in both modes,
// so the record's result is never judged from it. Positive twin first: the same
// session with a readable (absent) record ends the same way.
func TestStateUnreadableIsNeverTheMainOutcome(t *testing.T) {
	f := syncingFake()
	res := runSession(context.Background(), syncingDeps(f, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeOK {
		t.Fatalf("twin: outcome %q, want ok", res.outcome.Name)
	}
	for _, attended := range []bool{false, true} {
		sp := filepath.Join(t.TempDir(), "s.json")
		if err := os.WriteFile(sp, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		f := syncingFake()
		d := syncingDeps(f, sp)
		d.attended, d.neverQuit = attended, attended
		res := runSession(context.Background(), d)
		if res.outcome == OutcomeStateUnreadable || res.result().Outcome == OutcomeStateUnreadable {
			t.Errorf("attended %v: the main outcome is state-unreadable", attended)
		}
		if got := res.result().Side; len(got) != 1 || got[0] != OutcomeStateUnreadable {
			t.Errorf("attended %v: side %v, want [state-unreadable]", attended, got)
		}
	}
}
