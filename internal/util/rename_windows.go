//go:build windows

package util

// renameInUse classifies a rename error the atomic writer waits out: on
// Windows, another process briefly holding the source or target open.
var renameInUse = WindowsRenameInUse
