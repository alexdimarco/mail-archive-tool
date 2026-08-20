//go:build !windows

package outlookcom

import "log"

// Detect reports no Outlook automation on non-Windows platforms.
func Detect() (version string, available bool) { return "", false }

// CreatePSTs is unsupported off Windows; it refuses cleanly so callers surface a
// legible message rather than a crash.
func CreatePSTs(outDir string, logger *log.Logger) ([]Store, error) {
	return nil, ErrUnsupported
}
