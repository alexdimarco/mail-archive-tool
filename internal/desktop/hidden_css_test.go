package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-852, R37
// An element the page hides is hidden: the pages style buttons with their own display,
// which beats the browser's own rule for the hidden attribute, so without a rule of
// their own a hidden button showed anyway — Run now's "Skip the program download" and
// "Cancel the backup" stayed on the page after the job ended (P3-8's live check of B7).
// The overview page and the shared page style both carry the rule.
func TestHiddenElementsStayHidden(t *testing.T) {
	const rule = "[hidden]{display:none!important}"
	page := sessionPage(t, newKeysEnv(t, filepath.Join(t.TempDir(), "archive")).h)
	assure.Reached(t, strings.Contains(page, rule) && strings.Contains(pageCSS, rule), "the hidden rule on every page style")
}
