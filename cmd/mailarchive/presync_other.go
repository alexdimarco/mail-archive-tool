//go:build !darwin

package main

import (
	"errors"

	"mail-archive-tool/internal/app"
)

// setPresyncHook refuses on every platform but macOS in this version (design
// §4.2.4, Q5: macOS first; Windows is TP6, Linux TP7, each after its own lab).
func setPresyncHook(_ *app.Options, _ string) error {
	return errors.New("-thunderbird-sync is only available on macOS in this version")
}

// presyncInstallNote is empty off macOS: a non-macOS schedule can never carry
// the sync (refused at schedule time), so there is nothing to say.
func presyncInstallNote(string) string { return "" }
