package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-438, R12, S52
// Refusal wording, the engine's half (the desktop's own refusals are the workspace's
// TestRefusalsCarryWhatWhyAndRemedy): each refusal the engine raises at Start or ends a run
// with carries what it did, why, and what to do next, in the design's words (§7.6), and none
// contains a token, a Graph URL, `panic` or `goroutine`. R-u says nothing false about the
// message that was downloading, and R-u3 says the sign-in was never sent away.
func TestEngineRefusalsCarryWhatWhyAndRemedy(t *testing.T) {
	plan := &CopyPlan{Dest: `E:\Exports`, Source: CopySourceInfo{Kind: "m365", Mailbox: "alex@contoso.com"}}
	state := `C:\Users\alex\AppData\Local\MailArchive`
	forbid := assure.Forbid("panic", "goroutine", "http://", "https://", "access_token", "refresh_token", "Bearer", "SECRET")

	for _, c := range []struct {
		id    string
		text  string
		names []string // what, why, remedy
	}{
		{"R-c", refusalText("R-c", 1203, 2088, "", plan, state), []string{"Microsoft 365 refused the request", "sign-in expired or access was revoked", "1,203 of 2,088", "Sign in again", "Export the remaining messages"}},
		{"R-d", refusalD(state).Message, []string{"couldn't record this export in " + state, "disk full or access denied", "nothing was exported", "Free some space there", "Start export again"}},
		{"R-d2", refusalText("R-d2", 812, 2088, "", plan, state), []string{"couldn't record this export's progress in " + state, "812 of 2,088", "marked unfinished", "Finish as partial", "Export the remaining messages"}},
		{"R-g", refusalText("R-g", 812, 2088, `E:\ — no space left`, plan, state), []string{"destination is full or no longer available", "no space left", "812 of 2,088", "marked unfinished", "choose another destination", "Export the remaining messages"}},
		{"R-g2", refusalG2(`E:\`, 1_100_000_000, 1_400_000_000).Message, []string{"free, but this export needs", "plus room to finish (the manifest and checksums)", "nothing was written", "Free some space or choose another destination"}},
		{"R-t", refusalT(`C:\Archive Folder`, "MailExport-2026-10-09_142233").Message, []string{"Couldn't create a new export folder in", "through -99 already exist", "choose another destination"}},
		{"R-u", refusalText("R-u", 1500, 2088, "", plan, state), []string{"down for 5 minutes", "1,500 of 2,088", "Check your network", "Export the remaining messages", "tries again the message that was downloading"}},
		{"R-u2", refusalText("R-u2", 1500, 2088, "", plan, state), []string{"having problems", "25 messages in a row returned a server error", "1,500 of 2,088", "Try again later", "Export the remaining messages"}},
		{"R-u3", refusalText("R-u3", 812, 2088, "cdn.example.net", plan, state), []string{"redirected a download to another server (cdn.example.net)", "doesn't follow redirects away from Microsoft 365", "your sign-in was never sent there", "812 of 2,088", "check with your IT team", "Export the remaining messages"}},
		{"R-v2", refusalText("R-v2", 1500, 2088, "", plan, state), []string{"limiting this app's requests", "throttled", "nothing downloaded for 5 minutes", "1,500 of 2,088", "Try again in a few minutes", "Export the remaining messages"}},
		{"R-w", refusalW(`E:\Exports`, "access denied").Message, []string{"couldn't create a folder in E:\\Exports (access denied)", "choose another folder", "Controlled folder access", "Nothing was written"}},
		{"R-w2", refusalText("R-w2", 0, 0, `BUILTIN\Users`, plan, state), []string{"Other accounts can read what you export to", `E:\Exports`, `BUILTIN\Users`, "preview said only you could", "nothing was exported", "Preview copy export again"}},
		{"R-z", refusalText("R-z", 900, 2088, "bob@contoso.com", plan, state), []string{"sign-in was cleared or changed (now: bob@contoso.com)", "alex@contoso.com's mailbox stopped after 900 of 2,088", "nothing is read with the wrong account", "Sign in as alex@contoso.com", "Export the remaining messages"}},
		{"R-z cleared", refusalText("R-z", 900, 2088, "", plan, state), []string{"now: signed out", "Sign in as alex@contoso.com"}},
		{"internal", refusalText("internal", 3, 10, "", plan, state), []string{"internal error", "3 of 10", "marked unfinished", "Finish as partial", "Export the remaining messages"}},
	} {
		t.Run(c.id, func(t *testing.T) {
			assure.Refused(t, 1, c.text, assure.Code(1), assure.Names(c.names...), forbid)
		})
	}
	if u := refusalText("R-u", 5, 10, "", plan, state); strings.Contains(u, "none of them was marked failed") {
		t.Errorf("R-u still says the in-flight message was not listed: %q", u)
	}
}
