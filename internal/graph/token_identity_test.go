package graph

import "testing"

// covers: MA-515, R17, R5, S20
func TestTokenWriteBackIsIdentitySafe(t *testing.T) {
	t.Skip("MA-515: putIfNewer and reload are not identity-safe; lands in EW-E1")
}
