//go:build !windows

package tbrestore

// No Microsoft Store Thunderbird and no Windows network drives elsewhere.
func storeInstallFound() bool { return false }
func onNetwork(string) bool   { return false }
