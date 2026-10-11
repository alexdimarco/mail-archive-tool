//go:build !windows

package app

import "os"

// appendFlag is the OS's append mode: every write goes to the end of the file, whichever
// handle makes it.
const appendFlag = os.O_APPEND
