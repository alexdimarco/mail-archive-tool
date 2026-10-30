//go:build darwin

package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
)

// covers: MA-550, MA-567
// A scheduled sync's INSTALL note carries the §6 sync notice and the macOS
// first-approval command (launchctl kickstart gui/<uid>/<label>), so the owner
// learns how to grant the Automation permission the scheduled quit needs — a
// grant that must come from a scheduler-started run, not Terminal (PC4). Neither
// note prints a raw snake_case status code at the owner (friction review,
// 2026-10-24: plain-language rule). The PREVIEW note, printed before the schedule
// exists, frames the grant as a future step and omits the kickstart command (whose
// launchd label does not exist yet).
func TestPresyncInstallNote(t *testing.T) {
	note := presyncInstallNote("mailarchive-abc", false)
	if !strings.Contains(note, presync.SyncNotice) {
		t.Error("install note is missing the §6 sync notice")
	}
	if !strings.Contains(note, "launchctl kickstart gui/") {
		t.Errorf("install note is missing the first-approval command: %q", note)
	}
	if !strings.Contains(note, "com.dimarcotech.mailarchive.mailarchive-abc") {
		t.Errorf("install note is missing the launchd label: %q", note)
	}
	if strings.Contains(note, "presync_quit_not_permitted") {
		t.Errorf("install note prints a raw snake_case code at the owner: %q", note)
	}

	preview := presyncInstallNote("mailarchive-abc", true)
	if !strings.Contains(preview, presync.SyncNotice) {
		t.Error("preview note is missing the §6 sync notice")
	}
	if strings.Contains(preview, "launchctl kickstart") {
		t.Errorf("preview note prints the kickstart command for a schedule not yet installed: %q", preview)
	}
	if strings.Contains(preview, "presync_quit_not_permitted") {
		t.Errorf("preview note prints a raw snake_case code at the owner: %q", preview)
	}
	if !strings.Contains(preview, "After you install") {
		t.Errorf("preview note should frame the grant as a future step: %q", preview)
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
