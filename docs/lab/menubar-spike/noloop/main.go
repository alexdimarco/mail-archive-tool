// Hypothesis H1 for the 2026-11-12 "app quits at once" report: a Go program in a
// .app with no AppKit event loop (as mailarchive is today) is still registered by
// LaunchServices as the running app, so a second open of the same bundle starts no
// second process and the reopen it sends is never answered. Logs each start.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	if i := strings.Index(dir, ".app/Contents/MacOS"); i >= 0 {
		dir = filepath.Dir(dir[:i+len(".app")])
	}
	f, _ := os.OpenFile(filepath.Join(dir, "noloop.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	fmt.Fprintf(f, "%s start pid=%d\n", time.Now().Format("15:04:05.000"), os.Getpid())
	time.Sleep(15 * time.Second)
	fmt.Fprintf(f, "%s exit pid=%d\n", time.Now().Format("15:04:05.000"), os.Getpid())
}
