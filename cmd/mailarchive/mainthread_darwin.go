//go:build darwin

package main

import "runtime"

// AppKit is touched only on the process's main thread (design-menubar §8.2): main runs
// on it from the start, and an instance started from the .app runs [NSApp run] there
// (internal/desktop's macOS event loop). Harmless for every other verb.
func init() { runtime.LockOSThread() }
