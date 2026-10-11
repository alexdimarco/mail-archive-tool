package localweb

import "testing"

// covers: MA-372, R23, X9
// Skip encoder (catalog-first): the session check, the locked page and the
// /s/<P>/ prefix.
func TestNoSessionGetsTheLockedPage(t *testing.T) {
	t.Skip("MA-372: sessions, the locked page and the /s/<P>/ prefix are not built; lands in LC-C2")
}

// covers: MA-373, R23, X8
// Skip encoder (catalog-first): one-time links, their expiry and the marker
// cookie.
func TestLinkRedeemsOnce(t *testing.T) {
	t.Skip("MA-373: one-time launch links and /_open are not built; lands in LC-C2")
}
