package health

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-142, R18, S31
// status prints a "Fixity coverage: N of M records recorded" line from manifest
// fields alone (it hashes nothing — that is `verify`), so the wording says
// coverage not integrity and points at `mailarchive verify` to check the bytes.
// When every record carries a digest the line offers no baseline remedy; when
// some records lack one it also names the `mailarchive verify -record` remedy.
func TestStatusFixityLine(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	// Full coverage: N == M, coverage wording + verify-to-check-bytes hint, no
	// baseline remedy.
	full := healthyInput(now)
	full.Messages, full.WithFixity = 100, 100
	lines := strings.Join(Summary(full, Assess(full, now)), "\n")
	if !strings.Contains(lines, "Fixity coverage: 100 of 100 records recorded") {
		t.Errorf("full-coverage fixity line missing:\n%s", lines)
	}
	if !strings.Contains(lines, "verify -out") || !strings.Contains(lines, "to check the bytes") {
		t.Errorf("fixity line must point at `verify -out` to check the bytes:\n%s", lines)
	}
	if strings.Contains(lines, "verify -record") {
		t.Errorf("full coverage should offer no baseline remedy:\n%s", lines)
	}
	// The line must not imply integrity: the old "carry digests" phrasing is gone.
	if strings.Contains(lines, "carry digests") {
		t.Errorf("fixity line still reads as integrity, not coverage:\n%s", lines)
	}

	// Partial coverage: N < M names the baseline remedy as well.
	partial := healthyInput(now)
	partial.Messages, partial.WithFixity = 100, 40
	pl := strings.Join(Summary(partial, Assess(partial, now)), "\n")
	if !strings.Contains(pl, "Fixity coverage: 40 of 100 records recorded") {
		t.Errorf("partial-coverage fixity line missing:\n%s", pl)
	}
	if !strings.Contains(pl, "verify -record") {
		t.Errorf("partial coverage must name the `verify -record` remedy:\n%s", pl)
	}
}
