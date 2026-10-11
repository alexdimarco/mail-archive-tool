package app

import (
	"testing"

	"mail-archive-tool/internal/assure"
)

// The friction review's fixes in the engine's own words (docs/review-extraction-workspace-friction.md).

// covers: MA-438, R12, S52
// F2, F3: the stop notes of a run cut off by the sign-in name the button that is on Export
// activity (Sign in), the account to sign in as, and the button to click next; a retry's R-l
// says the same, and RetryButton names the button the retry was started from. (A retry's R-l
// reaches the owner through the desktop, which words it for the page: the engine's own R-l
// stays the design's §7.6 text, which TestRetryReadsTheRunRecord pins.)
func TestFricSignInStopsAndRetryRefusalNameTheButtons(t *testing.T) {
	plan := &CopyPlan{Dest: `E:\Exports`, Source: CopySourceInfo{Kind: "m365", Mailbox: "alex@contoso.com"}}
	forbid := assure.Forbid("Load folders again", "panic", "goroutine", "https://")
	assure.Refused(t, 1, refusalText("R-c", 1203, 2088, "", plan, "state"), assure.Code(1),
		assure.Names("click Sign in on Export activity", "Export the remaining messages"), forbid)
	assure.Refused(t, 1, refusalText("R-z", 900, 2088, "bob@contoso.com", plan, "state"), assure.Code(1),
		assure.Names("Sign in as alex@contoso.com (click Sign in on Export activity)", "Export the remaining messages"), forbid)
	if RetryButton("rest") != "Export the remaining messages" || RetryButton("failed") != "Export failed messages again" {
		t.Errorf("the retry buttons: %q %q", RetryButton("rest"), RetryButton("failed"))
	}
}
