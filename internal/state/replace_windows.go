//go:build windows

package state

// renameInUse classifies a rename error replaceFile waits out: on Windows,
// ERROR_ACCESS_DENIED and ERROR_SHARING_VIOLATION, which MoveFileEx returns
// while `status`, serve's go-back handler or the desktop view is reading the
// manifest (state.Load) or a last-run/last-verify record, or antivirus is
// scanning the new temp file. A var so a test can substitute it.
var renameInUse = windowsRenameInUse
