package main

import "testing"

// covers: MA-217, R17, S38
// The GUI's Deleted-Items/Junk decision defaults to EXCLUDED on a click-through:
// includeDeletedJunk maps the default (exclude) item — and any answer that is not
// the explicit include item — to (false, false), and only the explicit include
// item to (true, true), so accepting the dialog default never silently archives
// the trash/junk folders (T7). deletedJunkApplies is false for the GUI's local
// source types, so the question is not put where the walk cannot honor the
// well-known-id exclusion (the dialog itself is lab-tier).
func TestIncludeDeletedJunkDefaultsExcluded(t *testing.T) {
	// Click-through: the dialog's default (exclude) item excludes both.
	if d, j := includeDeletedJunk(deletedJunkExcludeItem); d || j {
		t.Errorf("click-through must exclude both; got deleted=%v junk=%v", d, j)
	}
	// Any non-include answer (a closed dialog, an unexpected string) excludes too
	// — the code never silently opts in.
	if d, j := includeDeletedJunk(""); d || j {
		t.Errorf("an empty/unexpected answer must exclude both; got deleted=%v junk=%v", d, j)
	}
	// Only the explicit include item opts in.
	if d, j := includeDeletedJunk(deletedJunkIncludeItem); !d || !j {
		t.Errorf("the explicit include item must include both; got deleted=%v junk=%v", d, j)
	}

	// The question is not put for the GUI's local sources: their one-shot import
	// has no well-known-id exclusion to honor, so asking would be a lie.
	for _, src := range []string{srcAuto, srcOutlook, srcOutlookCOM, srcThunderbird, srcEvolution, srcMbox} {
		if deletedJunkApplies(src) {
			t.Errorf("deletedJunkApplies must be false for the local source %q", src)
		}
	}
}
