package source

import "testing"

// UseFidelity turns slice S2's content on (fidelityContent) for the duration of a test
// of another package that runs the real pipeline over the source package: the walker is
// not on by default (mimeroute.go says why), so such a test asks for it.
func UseFidelity(t testing.TB) {
	t.Helper()
	old := fidelityContent
	fidelityContent = true
	t.Cleanup(func() { fidelityContent = old })
}
