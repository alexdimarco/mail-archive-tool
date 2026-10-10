package state

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// covers: MA-323, R1, R5, S46
// F16 (friction review): a bidirectional override or isolate in mail text (a
// subject, a folder, an error, a locator) is stripped from the ledger, on the way in
// (ObserveGap) and on Load, so no report, JSON or status line can show a reversed
// file name; ordinary non-ASCII text (a right-to-left language without controls)
// is untouched.
func TestCaptureGapStripsBidiControls(t *testing.T) {
	const rtl = "‮" // right-to-left override
	hostile := func(g CaptureGap) CaptureGap {
		g.Subject = "invoice" + rtl + "fdp.exe‬ ⁦x⁩"
		g.Folder = "In" + rtl + "box"
		g.Detail = "boom" + rtl
		return g
	}
	check := func(where string, g CaptureGap) {
		t.Helper()
		for name, v := range map[string]string{"subject": g.Subject, "folder": g.Folder, "detail": g.Detail, "locator": g.Locator} {
			if strings.ContainsAny(v, "‪‫‬‭‮⁦⁧⁨⁩") {
				t.Errorf("%s: %s %q kept a bidi control", where, name, v)
			}
		}
		if g.Subject != "invoicefdp.exe x" || g.Folder != "Inbox" {
			t.Errorf("%s: subject %q folder %q; want the text with only the controls removed", where, g.Subject, g.Folder)
		}
	}

	m, path, tok := gapManifest(t)
	g := hostile(itemGap(tok, "graph:BIDI"+rtl))
	m.ObserveGap(g, gapBase)
	check("ObserveGap", assure.Reached(t, m.CaptureGapsSorted(), "the ledger")[0])

	// A hand-edited manifest: Load sanitises it.
	m.CaptureGaps = map[string]CaptureGap{}
	raw := hostile(itemGap(tok, "graph:RAW"))
	raw.FirstRun, raw.LastRun, raw.Runs = gapBase, gapBase, 1
	m.CaptureGaps[GapKey(raw.Token, raw.Scope, raw.Locator)] = raw
	m = reload(t, m, path)
	check("Load", assure.Reached(t, m.CaptureGapsSorted(), "the loaded ledger")[0])

	// Text in a right-to-left script survives; only the controls go.
	if got := util.StripBidi("שלום עולם"); got != "שלום עולם" {
		t.Errorf("StripBidi changed Hebrew text: %q", got)
	}
}
