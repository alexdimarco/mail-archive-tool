package export

import (
	"bytes"
	"strings"
)

// The archived-page gate (extraction design §4.11, RR2-C9). The copy export copies an
// archived page, the only content of a record that kept no original, byte-exact. A page
// is inert because the renderer's ArchiveCSP meta comes first, not because scripts were
// removed ("the policy meta that precedes everything makes them inert"), so a page may
// keep its .html name only if its bytes begin with the renderer's exact prefix and no
// other hostile meta follows. Any other page (a page of an older renderer, or anything
// the author of another archive folder wrote, ruling W1) is copied as text instead.

// ArchivedPageMaxBytes is the largest page the gate reads. A page above it is not read at
// all and is quarantined: what cannot be checked is not trusted.
const ArchivedPageMaxBytes = 32 << 20

// LiveRefreshIn reports whether an archived page carries a hostile meta or a resource
// hint other than the tool's own leading metas: a refresh, a policy meta of its own, a
// competing charset, a set-cookie or content-type pragma, a preconnect or prefetch hint.
// The charset meta and the policy metas that every page of the renderer starts with are
// the exact match at the start of <head> and are not counted: hostileSurvives alone would
// flag every page. The page is read in every way a browser can read it (with scripting on
// and, when it holds a noscript, off), and one the parser cannot read at all is reported,
// so that it fails closed.
func LiveRefreshIn(page []byte) bool {
	s := string(page)
	if rest, ok := strings.CutPrefix(s, docPrefix); ok {
		s = "<!DOCTYPE html><html><head>" + rest
	}
	for _, scripting := range scriptingModes(s) {
		if hostileScan(s, scripting, false) != "" {
			return true
		}
	}
	return false
}

// ArchivedPageInert reports whether an archived page may keep its .html name: it begins
// with the renderer's exact prefix, so the policy meta governs everything after it, and
// LiveRefreshIn finds no other hostile meta.
func ArchivedPageInert(page []byte) bool {
	return bytes.HasPrefix(page, []byte(docPrefix)) && !LiveRefreshIn(page)
}
