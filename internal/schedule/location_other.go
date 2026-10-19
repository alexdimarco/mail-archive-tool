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
// Group-writable is a STATED RESIDUAL, not refused: telling reliably whether a
// group has members other than the owner needs group enumeration Go cannot do
// portably (primary-group members and directory-service groups do not appear in
// /etc/group), and refusing every group-writable prefix would wrongly reject
// common single-user installs such as Homebrew's group-writable /usr/local. The
// adversarial pass and the owner decide whether to tighten this; the world-
// writable case — the design's primary example, a folder inheriting broad write
// (Authenticated Users / Everyone / world) — is refused.
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
