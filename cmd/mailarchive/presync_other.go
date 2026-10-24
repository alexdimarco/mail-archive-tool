//go:build !darwin && !windows

package main

import (
	"errors"

	"mail-archive-tool/internal/app"
)

// setPresyncHook refuses on platforms without an adapter (Linux, TP7, after its
// own lab). macOS and Windows have real adapters (presync_hook.go).
func setPresyncHook(_ *app.Options, _ string) error {
	return errors.New("-thunderbird-sync is only available on macOS and Windows in this version")
}

// presyncInstallNote is empty where the sync is unavailable: such a schedule can
// never carry the sync (refused at schedule time), so there is nothing to say.
func presyncInstallNote(string) string { return "" }
