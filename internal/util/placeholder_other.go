//go:build !darwin && !windows

package util

// IsPlaceholder reports whether path is an online-only placeholder; this system has
// none MailArchive knows.
func IsPlaceholder(string) bool { return false }
