//go:build !linux && !windows

package desktop

// endPreUpgrade cannot end an older instance here: learning which process owns a
// port has no pure-Go form on this system (macOS has no /proc and no
// GetExtendedTcpTable). The dashboard and `status` then say an older MailArchive
// Desktop is still running and that signing out and back in finishes the upgrade
// [CC22].
func endPreUpgrade(addr string) (int, string) { return 0, "" }
