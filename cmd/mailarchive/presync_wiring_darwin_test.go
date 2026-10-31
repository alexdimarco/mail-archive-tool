//go:build darwin

package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
)

// covers: MA-550, MA-567, MA-939
// A scheduled sync's install and preview notes carry the §6 sync notice and the macOS
// permission note the wizard and the Desktop show (presync.MacAutomationNote): answer
// macOS's question the first time a scheduled backup closes Thunderbird, or allow it in
// System Settings > Privacy & Security > Automation. Corrected 2026-10-31 (the wording
// pass before 1.5.2): the notes no longer tell the owner to start the installed job
// from Terminal (`launchctl kickstart`) "while you are at the computer" — a scheduled
// run skips the sync while the keyboard or mouse was used in the last 2 minutes
// (presync session S4), so Thunderbird is never started and macOS never asks. Neither
// note prints a raw snake_case status code at the owner (friction review, 2026-10-24).
func TestPresyncInstallNote(t *testing.T) {
	want := presync.SyncNotice + "\n\n" + presync.MacAutomationNote
	for _, preview := range []bool{false, true} {
		note := presyncInstallNote("mailarchive-abc", preview)
		if note != want {
			t.Errorf("preview %v: the note is\n  %q\nwant\n  %q", preview, note, want)
		}
		for _, bad := range []string{"launchctl kickstart", "while you are at the computer", "presync_quit_not_permitted"} {
			if strings.Contains(note, bad) {
				t.Errorf("preview %v: the note says %q: %q", preview, bad, note)
			}
		}
	}
}

// covers: MA-568
// The -thunderbird-sync flag installs the pre-sync as app.Run's BeforeRead hook
// (the single seam); with the flag off the hook stays nil, which is every path
// today.
func TestSetPresyncHookInstalls(t *testing.T) {
	var opts app.Options
	if opts.BeforeRead != nil {
		t.Fatal("BeforeRead should start nil")
	}
	if err := setPresyncHook(&opts, ""); err != nil {
		t.Fatalf("setPresyncHook: %v", err)
	}
	if opts.BeforeRead == nil {
		t.Error("setPresyncHook did not install the BeforeRead hook")
	}
}
