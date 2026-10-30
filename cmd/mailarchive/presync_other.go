//go:build !darwin && !windows

package main

// presyncInstallNote is empty where the sync is unavailable: such a schedule can
// never carry the sync (refused at schedule time), so there is nothing to say.
func presyncInstallNote(string, bool) string { return "" }
