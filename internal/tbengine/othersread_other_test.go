//go:build !windows

package tbengine

import "testing"

// othersOnWindows is Windows-only (othersread_windows_test.go); never called elsewhere.
func othersOnWindows(*testing.T, string) string { return "" }
