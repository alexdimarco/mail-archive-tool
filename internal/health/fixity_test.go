package health

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-142, R18, S31
// status prints "Fixity: N of M records carry digests" from manifest fields
// alone (it does not hash anything — that is `verify`). When every record
// carries a digest the line states full coverage and offers no remedy; when
// some records lack one it names the `mailarchive verify -record` remedy.
func TestStatusFixityLine(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	// Full coverage: N == M, no remedy.
	full := healthyInput(now)
	full.Messages, full.WithFixity = 100, 100
	lines := strings.Join(Summary(full, Assess(full, now)), "\n")
	if !strings.Contains(lines, "Fixity:     100 of 100 records carry digests") {
		t.Errorf("full-coverage fixity line missing:\n%s", lines)
	}
	if strings.Contains(lines, "verify -record") {
		t.Errorf("full coverage should offer no baseline remedy:\n%s", lines)
	}

	// Partial coverage: N < M names the baseline remedy.
	partial := healthyInput(now)
	partial.Messages, partial.WithFixity = 100, 40
	pl := strings.Join(Summary(partial, Assess(partial, now)), "\n")
	if !strings.Contains(pl, "Fixity:     40 of 100 records carry digests") {
		t.Errorf("partial-coverage fixity line missing:\n%s", pl)
	}
	if !strings.Contains(pl, "verify -record") {
		t.Errorf("partial coverage must name the `verify -record` remedy:\n%s", pl)
	}
}
