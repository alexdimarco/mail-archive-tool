package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-348, R1
// MS2-AO4 (round 2). A report row is one line of seven fields whatever the kind holds: the
// kind goes through tsv() like every other field, so a kind with a tab or a line break
// (none exists today; the check that a parse-gap label names a known kind is the first
// line of defence, this is the second) cannot add a column or a row.
func TestARecordRowIsOneLineWhateverTheKindHolds(t *testing.T) {
	r := state.Record{Folder: "Inbox", Date: "2023-01-01", Subject: "Quarterly results", Path: "x.html"}
	row := recordRow("empty-attachment\tterminal\tInbox\n2023-01-01", "fillable", r, "a.pdf")
	if strings.Count(row, "\n") != 1 || !strings.HasSuffix(row, "\n") {
		t.Fatalf("the row is %d lines: %q", strings.Count(row, "\n"), row)
	}
	if f := strings.Split(strings.TrimSuffix(row, "\n"), "\t"); len(f) != 7 || f[1] != "fillable" {
		t.Errorf("the row has %d fields (%q), want 7 with the class in the second", len(f), f)
	}
	// The positive twin: an ordinary kind is written as it is.
	if f := strings.Split(recordRow("empty-attachment", "fillable", r, "a.pdf"), "\t"); f[0] != "empty-attachment" || len(f) != 7 {
		t.Errorf("an ordinary row is %q", f)
	}
}
