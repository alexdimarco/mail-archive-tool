package desktop

import "testing"

// covers: MA-384, R23, X11, S41
// Skip encoder (catalog-first): the security copy states the new boundary.
func TestSecurityCopyStatesTheBoundary(t *testing.T) {
	t.Skip("MA-384: the security copy, the account banner and the session count are not written; lands in LC-C2")
}

// covers: MA-384, R23
// Skip encoder (open by design): the extraction workspace's own sign-in surface
// (`/api/m365/signin`) carries the account banner and the "a sign-in code only in
// a window opened from the shortcut" copy [CC3, CC23].
func TestWorkspaceSignInSurfaceCarriesTheAccountBanner(t *testing.T) {
	t.Skip("MA-384: the extraction workspace's sign-in surface does not exist yet; its banner and sign-in-code copy land with slice E3 (rulings §9 row 24)")
}
