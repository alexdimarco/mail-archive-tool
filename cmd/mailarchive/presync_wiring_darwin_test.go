//go:build darwin

package main

import (
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
)

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

// covers: MA-561
// presyncWarnings turns a WARN outcome into one RunWarning carrying its code and
// the what/why/do-this message; an ok or note outcome adds none.
func TestPresyncWarningsMapping(t *testing.T) {
	w := presyncWarnings(presync.Result{Outcome: presync.OutcomeQuitNotPermitted})
	if len(w) != 1 || w[0].Code != presync.OutcomeQuitNotPermitted.Code || w[0].Message == "" {
		t.Errorf("warn mapping = %+v, want one quit-not-permitted warning with a message", w)
	}
	if got := presyncWarnings(presync.Result{Outcome: presync.OutcomeOK}); got != nil {
		t.Errorf("ok outcome produced warnings: %+v", got)
	}
	if got := presyncWarnings(presync.Result{Outcome: presync.OutcomeAlreadyRunning}); got != nil {
		t.Errorf("a note outcome produced warnings: %+v", got)
	}
}
