package main

import "os"

// readNoArgInputs reads §4.3's inputs on Linux: whether no stream is a terminal,
// and whether a graphical display is present. The Windows and macOS inputs are
// false here. The display is read even though the graphical row (L1) is dormant
// in SE1, so that row becomes a one-line change in defaultVerb once L-L1 lands.
func readNoArgInputs(string) noArgInputs {
	return noArgInputs{
		noTerminal: noTerminal(),
		hasDisplay: os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "",
	}
}
