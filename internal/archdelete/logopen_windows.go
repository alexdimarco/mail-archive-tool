//go:build windows

package archdelete

// logNoFollow is 0 on Windows: os.OpenFile has no no-follow flag there, so the
// Lstat before the open and the Stat of the opened file are the guards.
const logNoFollow = 0
