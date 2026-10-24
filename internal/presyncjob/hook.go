//go:build darwin || windows

// Package presyncjob assembles the Thunderbird pre-sync as app.Run's BeforeRead
// hook, in ONE place shared by every surface that starts a local archive run
// which may pre-sync: the -thunderbird-sync CLI flag, the scheduled GUI job
// (internal/wizard.RunHeadless) and the Desktop's "Run now"
// (internal/wizard.RunJobInProcess). So the pre-sync behaves identically however
// the run was started (design docs/design-thunderbird-presync.md §4.1, §4.2.5).
// Supported on macOS and Windows; a no-op elsewhere (hook_other.go).
package presyncjob

import (
	"context"
	"io"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
)

// SetHook installs the pre-sync on opts.BeforeRead: one session over the run's
// discovered sources, mapped to a run warning, never failing the archive (R32 —
// app.Run recovers a panic in the hook). profile carries an explicit
// -thunderbird-profile / job profile; "" means derive it from the inputs and
// require profiles.ini membership (PC3). logw receives the owner-facing
// narration (a manual run passes os.Stderr; a scheduled run its own log file).
func SetHook(opts *app.Options, profile string, logw io.Writer) error {
	stateDir, err := StateDir()
	if err != nil {
		return err
	}
	named := profile != ""
	opts.BeforeRead = func(ctx context.Context, files []string) app.BeforeReadResult {
		res := presync.Run(ctx, presync.Config{
			Inputs:       files,
			Profile:      profile,
			ProfileNamed: named,
			StateDir:     stateDir,
			Log:          logw,
		})
		return app.BeforeReadResult{Warnings: warnings(res), Release: res.Release}
	}
	return nil
}

// warnings maps a pre-sync result to the run's warnings: a WARN outcome becomes
// one RunWarning (its code and what/why/do-this text); an ok or note outcome
// adds none.
func warnings(res presync.Result) []state.RunWarning {
	if msg, warn := presync.WarnText(res.Outcome); warn {
		return []state.RunWarning{{Code: res.Outcome.Code, Message: msg}}
	}
	return nil
}
