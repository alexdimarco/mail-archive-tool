//go:build darwin

package instance

import "testing"

// noLogindForTest does nothing on macOS: its sign-in is the audit session, not logind's.
func noLogindForTest(t *testing.T) {}
