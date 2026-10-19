package main

import "strings"

// readNoArgInputs reads §4.3's inputs on macOS: whether the unresolved invoked
// path is inside a .app bundle (so a Finder/Dock/open launch is told apart from
// a PATH symlink typed in Terminal), and whether no stream is a terminal. The
// Windows and Linux inputs are false here.
func readNoArgInputs(invokedPath string) noArgInputs {
	return noArgInputs{
		inBundle:   strings.Contains(invokedPath, "/Contents/MacOS/"),
		noTerminal: noTerminal(),
	}
}
