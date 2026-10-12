package health

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// covers: MA-348, R1, R18, X6
// status splits "source-empty" from "cannot decode" (design-mime-fidelity §4.9, MC15):
// the Incomplete line carries "M with parts this version cannot decode" between the
// source-empty count and the re-examination count, and says what it means; a fallback
// message count is its own line. Neither moves the posture (they are informational, as
// terminal is), both are notes with a code, and status -json carries them as
// `undecodable` and `fidelity_fallback`. With none, the line is exactly what it was.
func TestStatusSplitsSourceEmptyFromCannotDecode(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.Fillable, in.Terminal, in.Unknown = 3, 1, 2

	// Positive twin: no parse gap, the line is the one it always was.
	plain := strings.Join(Summary(in, Assess(in, now)), "\n")
	if !strings.Contains(plain, "Incomplete: 3 still missing content · 1 source-empty (never fillable) · 2 not yet re-examined") || strings.Contains(plain, "cannot decode") {
		t.Fatalf("the completeness line changed with nothing undecodable:\n%s", plain)
	}

	in.Undecodable, in.FidelityFallback = 4, 2
	rep := Assess(in, now)
	text := strings.Join(Summary(in, rep), "\n")
	for _, want := range []string{
		"Incomplete: 3 still missing content · 1 source-empty (never fillable) · 4 with parts this version cannot decode · 2 not yet re-examined",
		"with parts this version cannot decode = kept as received in the attachments zip",
		"Fallback:   2 message(s)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("status lacks %q:\n%s", want, text)
		}
	}
	if rep.Posture != Assess(healthyInputWith(now, 3, 1, 2), now).Posture {
		t.Errorf("posture %s: cannot-decode and fallback counts moved it", rep.Posture)
	}
	codes := strings.Join(rep.Codes, " ")
	if !strings.Contains(codes, "undecodable_messages") || !strings.Contains(codes, "fidelity_fallback") {
		t.Errorf("reason codes %q lack undecodable_messages and fidelity_fallback", codes)
	}
	for i, c := range rep.Codes {
		if (c == "undecodable_messages" || c == "fidelity_fallback") && (strings.HasPrefix(rep.Reasons[i], "WARN") || strings.HasPrefix(rep.Reasons[i], "RED")) {
			t.Errorf("%s is %q: an informational count must not warn", c, rep.Reasons[i])
		}
	}

	// Only undecodable messages, nothing else incomplete: the line still appears.
	only := healthyInput(now)
	only.Undecodable = 1
	if l := strings.Join(Summary(only, Assess(only, now)), "\n"); !strings.Contains(l, "0 still missing content · 0 source-empty (never fillable) · 1 with parts this version cannot decode") {
		t.Errorf("a record with only a part this version cannot decode is not reported:\n%s", l)
	}

	b, err := json.Marshal(JSON(in, rep))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["undecodable"] != float64(4) || doc["fidelity_fallback"] != float64(2) {
		t.Errorf("status -json undecodable %v fidelity_fallback %v, want 4 and 2", doc["undecodable"], doc["fidelity_fallback"])
	}
	if doc["terminal"] != float64(1) {
		t.Errorf("status -json terminal %v, want 1: the terminal count is the source-empty one", doc["terminal"])
	}
}

// healthyInputWith is healthyInput with the three completeness counts set.
func healthyInputWith(now time.Time, fillable, terminal, unknown int) Input {
	in := healthyInput(now)
	in.Fillable, in.Terminal, in.Unknown = fillable, terminal, unknown
	return in
}
