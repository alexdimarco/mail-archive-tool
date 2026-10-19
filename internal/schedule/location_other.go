//go:build !windows

package schedule

import (
	"os"
	"path/filepath"
)

// locationWritableByOthersOS (Unix) reports whether dir or any ancestor up to the
// root is world-writable WITHOUT the sticky bit, which means any account on the
// machine can replace the program — or rename a parent and substitute its own —
// so a scheduled run would execute their code as the owner (Q20, §5.3, MA-648).
//
// The sticky bit matters: a world-writable sticky directory (mode 1777, e.g.
// /tmp) lets others create files but NOT delete or replace files they do not own,
// so it does not expose an existing program to another account; it is not flagged
// (and this is why a t.TempDir under a /tmp sticky root is correctly safe).
//
// Group-writable is NOT refused (owner ruling 2026-10-18: installing as an
// authenticated local user following the usual local-program rules is fine). A
// normal install lands in a group-writable prefix like Homebrew's /usr/local or
// /Applications, which the usual rules treat as normal; refusing those would
// reject legitimate installs, and reliably telling whether such a group has
// members other than the owner is not portable in Go anyway (primary-group and
// directory-service members do not appear in /etc/group). Q20 refuses only a
// genuine other-account write — a world-writable (non-sticky) location here, or
// a non-owner/non-admin write ACE on Windows — the design's primary example
// being a folder inheriting broad write (Authenticated Users / Everyone / world).
func locationWritableByOthersOS(dir string) bool {
	p := filepath.Clean(dir)
	for {
		if fi, err := os.Stat(p); err == nil {
			m := fi.Mode()
			if m.Perm()&0o002 != 0 && m&os.ModeSticky == 0 {
				return true
			}
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}
