//go:build darwin || windows

package main

import (
	"context"
	"os"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
)

// setPresyncHook installs the Thunderbird pre-sync as app.Run's BeforeRead hook
// (design §4.1, Q9 manual run): it resolves and runs one session on the discovered
// sources, then maps the outcome to a warning on the run record. It never fails
// the archive (R32) — app.Run already recovers a panic in the hook. Shared by the
// macOS and Windows adapters; each supplies its own presyncStateDir.
func setPresyncHook(opts *app.Options, profile string) error {
	stateDir, err := presyncStateDir()
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
			Log:          os.Stderr, // the owner watches a manual run; narrate it
		})
		return app.BeforeReadResult{Warnings: presyncWarnings(res)}
	}
	return nil
}

// presyncWarnings turns a pre-sync result into the run's warnings: a WARN outcome
// becomes one RunWarning (its code + the what/why/do-this text); an ok or note
// outcome adds none.
func presyncWarnings(res presync.Result) []state.RunWarning {
	if msg, warn := presync.WarnText(res.Outcome); warn {
		return []state.RunWarning{{Code: res.Outcome.Code, Message: msg}}
	}
	return nil
}
