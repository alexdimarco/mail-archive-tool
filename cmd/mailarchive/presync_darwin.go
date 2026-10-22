//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/app"
	jobfile "mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
)

// setPresyncHook installs the Thunderbird pre-sync as app.Run's BeforeRead hook
// (design §4.1, Q9 manual run): it resolves and runs one session on the
// discovered sources, then maps the outcome to a warning on the run record. It
// never fails the archive (R32) — app.Run already recovers a panic in the hook.
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

// presyncWarnings turns a pre-sync result into the run's warnings: a WARN
// outcome becomes one RunWarning (its code + the what/why/do-this text); an ok
// or note outcome adds none.
func presyncWarnings(res presync.Result) []state.RunWarning {
	if msg, warn := presync.WarnText(res.Outcome); warn {
		return []state.RunWarning{{Code: res.Outcome.Code, Message: msg}}
	}
	return nil
}

// presyncStateDir is the "presync" subdirectory of the job files' per-user
// directory (design §4.6): the write-ahead record and the session lock live
// here, never in the archive. It derives from the existing, pinned per-user
// location (jobfile.Dir, MA-621) rather than computing a new one.
func presyncStateDir() (string, error) {
	dir, err := jobfile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presync"), nil
}
