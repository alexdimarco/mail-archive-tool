package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-348, R1, R18, X2, X6
// F3 of the MF-S2 friction review (cells OW-3, OW-11, CA-2), the `status` half. When every message still
// missing content is only so because a part is cut short or its parts could not be told apart in the file
// it was read from, the WARN names what cures that (a complete export of the mailbox file; for an IMAP
// cache, a download), not the "download for offline use" remedy of a message a source has not delivered,
// and the Incomplete block says once how many of them are also counted as having parts this version
// cannot decode. Positive twins: with no parse gap, and with a mix, the WARN and the explanation are what
// they were (a mix keeps the download remedy: some of the messages are ones it is for).
func TestStatusRemedyFitsWhatIsMissing(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	text := func(in Input) (warn string, summary string) {
		rep := Assess(in, now)
		for i, c := range rep.Codes {
			if c == "incomplete_content" {
				warn = rep.Reasons[i]
			}
		}
		return warn, strings.Join(Summary(in, rep), "\n")
	}

	plain := healthyInput(now)
	plain.Fillable = 3
	warn, sum := text(plain)
	if !strings.Contains(warn, "3 message(s) still missing content — download for offline use in your mail app, then re-run; incremental fills them") ||
		!strings.Contains(sum, "(still missing content = not downloaded yet; fills on the next run)") || strings.Contains(sum, "also counted") {
		t.Fatalf("positive twin: the ordinary missing-content text changed:\n%s\n%s", warn, sum)
	}

	mixed := healthyInput(now)
	mixed.Fillable, mixed.Undecodable, mixed.FillableParseGap, mixed.FillableOnlyParseGap = 3, 1, 1, 1
	warn, sum = text(mixed)
	if !strings.Contains(warn, "download for offline use in your mail app") || !strings.Contains(sum, "1 of the messages still missing content is also counted as having parts this version cannot decode") {
		t.Errorf("a mixed archive lost the download remedy or the overlap note:\n%s\n%s", warn, sum)
	}

	only := healthyInput(now)
	only.Fillable, only.Undecodable, only.FillableParseGap, only.FillableOnlyParseGap = 2, 2, 2, 2
	warn, sum = text(only)
	if strings.Contains(warn, "download for offline use in your mail app, then re-run") || strings.Contains(sum, "not downloaded yet") {
		t.Errorf("a cut or boundary-less message read in full is told to download:\n%s\n%s", warn, sum)
	}
	for _, want := range []string{"2 message(s) still missing content", "cut short", "complete export", "IMAP cache", "re-run", "malformed when it was sent cannot be completed by either, and stays listed"} {
		if !strings.Contains(warn, want) {
			t.Errorf("the WARN lacks %q: %s", want, warn)
		}
	}
	if !strings.Contains(sum, "2 of the messages still missing content are also counted as having parts this version cannot decode") ||
		!strings.Contains(sum, "still missing content = cut short") {
		t.Errorf("the status block lacks the overlap note or the true explanation:\n%s", sum)
	}
	if Assess(only, now).Posture != "WARN" {
		t.Error("the posture is not WARN: this change words the WARN, it does not clear it")
	}
}

// covers: MA-348, R1, R18
// F3, the real path: Gather reads the two counts from the manifest.
func TestGatherReadsTheFillableParseGapCounts(t *testing.T) {
	out := t.TempDir()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Add("k1", state.Record{Path: "s/a.html", Missing: []string{state.ParseGapLabel(state.GapKindCorruptPart, "c.partial")}})
	m.Add("k2", state.Record{Path: "s/b.html", Missing: []string{"body"}})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	in := Gather(out, "")
	if in.FillableParseGap != 1 || in.FillableOnlyParseGap != 1 || in.Fillable != 2 {
		t.Errorf("Gather: fillable %d, parse-gap %d, only %d; want 2, 1, 1", in.Fillable, in.FillableParseGap, in.FillableOnlyParseGap)
	}
}
