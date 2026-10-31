//go:build darwin

package main

import (
	"fmt"

	"mail-archive-tool/internal/presync"
)

// presyncInstallNote is printed when a scheduled sync is installed (preview=false)
// or previewed (preview=true), design §8 step 2: the §6 notice of what the sync
// does, then the macOS permission note the wizard and MailArchive Desktop show
// (presync.MacAutomationNote): answer macOS's question the first time a scheduled
// backup closes Thunderbird, or allow it in System Settings. Both variants are the
// same since 2026-10-31 (the wording pass before 1.5.2, MA-939): the install note no
// longer tells the owner to start the installed job from Terminal (`launchctl
// kickstart`) while at the computer — a scheduled run skips the sync while the
// keyboard or mouse was used in the last 2 minutes (presync session S4), so
// Thunderbird would never be started and macOS would never ask. Neither variant prints
// a raw status code. Empty off macOS (the schedule-time refusal means a non-macOS
// schedule can never carry the sync).
func presyncInstallNote(_ string, _ bool) string {
	return fmt.Sprintf("%s\n\n%s", presync.SyncNotice, presync.MacAutomationNote)
}
