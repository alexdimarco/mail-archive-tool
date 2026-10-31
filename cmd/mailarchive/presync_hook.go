package main

import (
	"errors"
	"os"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
)

// setPresyncHook installs the Thunderbird pre-sync for -thunderbird-sync as
// app.Run's BeforeRead hook, through the ONE shared hook the scheduled and Run-now
// jobs use (design §4.1, Q9 manual run; design-presync-attended §2.2, T20), so
// side warnings (B6), the profile lock's release and the outcome reach a command
// line run exactly as they reach those. It never fails the archive (R32). The run
// is never attended here: the command line at a local terminal is slice AS10's,
// after the owner's nod (Q3). The owner watches a manual run, so the session
// narrates to stderr. Where there is no adapter the flag is refused, and the
// caller archives without the sync.
//
// The run asks for the sync (opts.PresyncWanted), so a scheduled `-unattended` run
// moves the archive's sync streak (design-presync-attended §5.3, slice AS5); a hook that
// could not be prepared is recorded as not-set-up (C-34), never as "sync off".
func setPresyncHook(opts *app.Options, profile string) error {
	if !presyncjob.Supported() {
		return errors.New("-thunderbird-sync is only available on macOS and Windows in this version")
	}
	opts.PresyncWanted = true
	h := presyncjob.HookOptions{Profile: profile, Log: os.Stderr, Surface: presync.SurfaceCLI}
	err := presyncjob.SetHook(opts, h)
	if err != nil {
		presyncjob.SetNotSetUp(opts, h)
	}
	return err
}
