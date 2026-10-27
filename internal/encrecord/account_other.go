//go:build !windows

package encrecord

// lookupAccountName is "" off Windows, where the record's refusal names no account.
func lookupAccountName(string) string { return "" }
