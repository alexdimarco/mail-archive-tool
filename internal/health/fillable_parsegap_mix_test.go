package health

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-348, R1, R18, X2, X6
// F3 of the MF-S2 friction review, re-verification (cells OW-3, OW-11, CA-2), the `status` half of a
// MIX: some messages the source has not delivered, some cut short in the mailbox file. The WARN and the
// Incomplete block kept only the download remedy and never said what cures the cut ones. They keep it
// for the first and say, for the others, that a download does not change a mailbox file and a complete
// export does. The positive twins: with no parse gap, and with only parse gaps, nothing of this is said.
func TestStatusOfAMixSaysWhatCuresTheCutOnes(t *testing.T) {
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
	const cure = "replace the file with a complete export from your mail app"

	plain := healthyInput(now)
	plain.Fillable = 3
	warn, sum := text(plain)
	if strings.Contains(warn, "complete export") || strings.Contains(sum, "complete export") {
		t.Fatalf("positive twin: an ordinary missing-content status mentions a complete export:\n%s\n%s", warn, sum)
	}

	mixed := healthyInput(now)
	mixed.Fillable, mixed.Undecodable, mixed.FillableParseGap, mixed.FillableOnlyParseGap = 3, 1, 1, 1
	warn, sum = text(mixed)
	if !strings.Contains(warn, "download for offline use in your mail app") {
		t.Errorf("a mix lost the download remedy in the WARN: %s", warn)
	}
	if !strings.Contains(warn, "1 of them is cut short, or has parts that could not be told apart, in the mailbox file") || !strings.Contains(warn, cure) {
		t.Errorf("the WARN of a mix never says what cures the cut message: %s", warn)
	}
	// The Incomplete block is what precedes the Posture lines (which repeat the WARN).
	block := sum
	if i := strings.Index(sum, "Posture:"); i >= 0 {
		block = sum[:i]
	}
	if !strings.Contains(block, "1 of them is cut short, or has parts that could not be told apart, in the mailbox file") || !strings.Contains(block, cure) {
		t.Errorf("the Incomplete block of a mix never says what cures the cut message:\n%s", sum)
	}
}
