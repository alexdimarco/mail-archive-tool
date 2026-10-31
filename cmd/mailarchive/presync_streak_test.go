package main

import (
	"context"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
)

// covers: MA-908
// The command line's -thunderbird-sync asks for the sync (design-presync-attended §5.3,
// slice AS5; C-34): positive twin first, with the state folder found the hook is the
// session's and the run carries PresyncWanted, so a scheduled `-unattended` run moves the
// archive's sync streak; with the state folder not found, setPresyncHook still says why
// (the caller logs it) and installs a hook that runs no session and records not-set-up
// with its warning, so the run counts a miss instead of looking like "sync off".
func TestCLIAsksForTheSyncAndRecordsNotSetUp(t *testing.T) {
	fake := &cliFake{res: presync.Result{Outcome: presync.OutcomeOK}}
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	var opts app.Options
	if err := setPresyncHook(&opts, ""); err != nil || opts.BeforeRead == nil || !opts.PresyncWanted || opts.PresyncAttended {
		t.Fatalf("positive twin: err %v, hook %v, PresyncWanted %v, PresyncAttended %v", err, opts.BeforeRead != nil, opts.PresyncWanted, opts.PresyncAttended)
	}
	if br := opts.BeforeRead(context.Background(), nil); br.Outcome == nil || br.Outcome.Name != "ok" {
		t.Fatalf("positive twin: outcome %+v", br.Outcome)
	}
	restore()

	fake = &cliFake{res: presync.Result{Outcome: presync.OutcomeOK}}
	restore = presyncjob.OverrideSessionForTest(fake.run, "") // the state folder cannot be found
	defer restore()
	opts = app.Options{}
	err := setPresyncHook(&opts, "")
	if err == nil || opts.BeforeRead == nil || !opts.PresyncWanted {
		t.Fatalf("no state folder: err %v, hook %v, PresyncWanted %v; want the error, a not-set-up hook and the sync asked for", err, opts.BeforeRead != nil, opts.PresyncWanted)
	}
	br := opts.BeforeRead(context.Background(), []string{"/in/a"})
	if br.Outcome == nil || br.Outcome.Name != presync.OutcomeNotSetUp.Name || br.Outcome.Synced || br.Outcome.Line == "" {
		t.Errorf("outcome %+v, want not-set-up, not synced, with its line", br.Outcome)
	}
	if len(br.Warnings) != 1 || br.Warnings[0].Code != presync.OutcomeNotSetUp.Code || br.Warnings[0].Message == "" {
		t.Errorf("warnings %+v, want the not-set-up warning", br.Warnings)
	}
	if fake.cfg.Inputs != nil {
		t.Error("a session ran although the sync could not be prepared")
	}
}
