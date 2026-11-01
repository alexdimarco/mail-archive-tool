//go:build !windows

package archdelete

import "syscall"

// logNoFollow opens deletions.log without following a link at its name.
const logNoFollow = syscall.O_NOFOLLOW
