package health

import (
	"errors"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

func hasCode(rep Report, code string) bool {
	for _, c := range rep.Codes {
		if c == code {
			return true
		}
	}
	return false
}

func summaryHas(in Input, rep Report, needle string) bool {
	for _, line := range Summary(in, rep) {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// covers: MA-213, R21, R18, S37
// History-log recovery legibility on the status side (X6): status reports the
// go-back timeline's coverage and flags a torn tail / corruption as a WARN whose
// remedy names the log and `reindex`, and a hard read failure as a RED — while a
// clean or an absent log never warns (a one-shot local import legitimately has
// none). status -json carries a `history {exists,runs,events,bad_lines,
// torn_tail}` object.
func TestHistoryCoveragePosture(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	// Positive twin 1: an absent log is not a problem — no history WARN/RED, and
	// the Summary says so plainly.
	in := healthyInput(now)
	rep := Assess(in, now)
	if hasCode(rep, "history_torn_tail") || hasCode(rep, "history_corrupt") || hasCode(rep, "history_unreadable") {
		t.Errorf("an absent history log must not warn: %v", rep.Codes)
	}
	if !summaryHas(in, rep, "History:") || !summaryHas(in, rep, "none recorded") {
		t.Errorf("Summary should report an absent timeline:\n%v", Summary(in, rep))
	}

	// Positive twin 2: a clean log reads "go-back available" and never warns.
	in = healthyInput(now)
	in.History = state.HistoryStat{Exists: true, Runs: 3, Events: 12}
	rep = Assess(in, now)
	if rep.Posture != "GREEN" {
		t.Errorf("a clean history log should stay GREEN, got %s (%v)", rep.Posture, rep.Reasons)
	}
	if !summaryHas(in, rep, "go-back available") {
		t.Errorf("Summary should read go-back available:\n%v", Summary(in, rep))
	}

	// Torn tail: a recoverable WARN whose remedy names reindex; Summary says
	// "go-back partial".
	in = healthyInput(now)
	in.History = state.HistoryStat{Exists: true, Runs: 2, Events: 5, TornTail: true, BadLines: 1}
	rep = Assess(in, now)
	if rep.Posture != "WARN" || !hasCode(rep, "history_torn_tail") {
		t.Errorf("a torn tail should WARN with history_torn_tail, got %s %v", rep.Posture, rep.Codes)
	}
	if !reasonNames(rep, "history_torn_tail", "reindex") {
		t.Errorf("the torn-tail remedy must name reindex: %v", rep.Reasons)
	}
	if !summaryHas(in, rep, "go-back partial") {
		t.Errorf("Summary should read go-back partial for a torn tail:\n%v", Summary(in, rep))
	}

	// Mid-log corruption (bad lines beyond a torn tail): history_corrupt WARN.
	in = healthyInput(now)
	in.History = state.HistoryStat{Exists: true, Runs: 2, Events: 5, BadLines: 3}
	rep = Assess(in, now)
	if rep.Posture != "WARN" || !hasCode(rep, "history_corrupt") {
		t.Errorf("mid-log corruption should WARN with history_corrupt, got %s %v", rep.Posture, rep.Codes)
	}
	if !reasonNames(rep, "history_corrupt", "reindex") {
		t.Errorf("the corruption remedy must name reindex: %v", rep.Reasons)
	}

	// A hard read failure is a RED naming the log.
	in = healthyInput(now)
	in.History = state.HistoryStat{Exists: true}
	in.HistoryReadErr = errors.New("read history log: unexpected EOF")
	rep = Assess(in, now)
	if rep.Posture != "RED" || !hasCode(rep, "history_unreadable") {
		t.Errorf("an unreadable history log should RED with history_unreadable, got %s %v", rep.Posture, rep.Codes)
	}

	// JSON carries the history object with the two damage signals.
	in = healthyInput(now)
	in.History = state.HistoryStat{Exists: true, Runs: 4, Events: 9, BadLines: 2, TornTail: true}
	doc := JSON(in, Assess(in, now))
	if doc.History == nil {
		t.Fatal("status -json lacks the history object")
	}
	if !doc.History.Exists || doc.History.Runs != 4 || doc.History.Events != 9 || doc.History.BadLines != 2 || !doc.History.TornTail {
		t.Errorf("history JSON = %+v, want the gathered coverage", *doc.History)
	}
}

// reasonNames reports whether the reason parallel to code contains needle.
func reasonNames(rep Report, code, needle string) bool {
	for i, c := range rep.Codes {
		if c == code && i < len(rep.Reasons) && strings.Contains(rep.Reasons[i], needle) {
			return true
		}
	}
	return false
}
