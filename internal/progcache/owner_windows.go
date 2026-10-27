//go:build windows

package progcache

import "os"

// ownedByMe is not consulted on Windows: the per-user cache folder's ACL is the
// user's own (%LocalAppData%).
func ownedByMe(os.FileInfo) bool { return true }
