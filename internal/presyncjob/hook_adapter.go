//go:build darwin || windows

package presyncjob

import "mail-archive-tool/internal/presync"

// On macOS and Windows the pre-sync has a platform adapter, so the shared hook's
// seam runs the real session (design-presync-attended §2.2, C-41).
func init() {
	supported = true
	runSession = presync.Run
}
