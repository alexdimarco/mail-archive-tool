package app

import (
	"context"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-854, R37
// verify -full names a damaged message by its subject, sender and folder, which come
// from mail: right-to-left overrides and isolates in them are stripped, so the line
// reads in order (F16; the P3-8 pass, LEAK-B19-BIDI). Positive twin: the words of
// the subject and folder are kept.
func TestADamagedMessageLabelStripsDirectionOverrides(t *testing.T) {
	rec := state.Record{Subject: "Invoice ‮fdp.exe", Folder: "In⁦box⁩"}
	label := (&fullCheck{}).messageLabel(context.Background(), nil, rec)
	from := plainLabel("Bob ‪Smith‬ ⁧x⁩", 80)
	bidi := func(s string) bool { return strings.ContainsAny(s, "‪‫‬‭‮⁦⁧⁨⁩") }
	assure.Reached(t, strings.Contains(label, "Invoice fdp.exe") && strings.Contains(label, "in Inbox") && !bidi(label) &&
		from == "Bob Smith x", "no direction overrides in the label: "+label+" / "+from)
}
