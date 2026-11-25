//go:build !windows

package wizard

import "testing"

// setFolderACLWhy reads a folder's access list on Windows only (its Windows file); elsewhere a set
// folder's mode is asserted instead, so a call here is a mistake in the test.
func setFolderACLWhy(t *testing.T, _ string) string {
	t.Helper()
	t.Fatal("a folder's access list is read on Windows only; assert its mode here")
	return ""
}
