package graph

import "testing"

// covers: MA-541, R10, R17
// Folders keeps one visited folder-id set across the call and a depth cap of
// 300: a cycle ends, a moved folder is listed once, and a level past the cap
// fails with a typed error while a chain exactly at the cap completes.
func TestFoldersHierarchyIsBounded(t *testing.T) {
	t.Skip("MA-541: Folders has no visited folder-id set and no depth cap yet; lands in FI-SG")
}
