// Package tty answers the two questions link delivery asks of the terminal
// (design-local-confidentiality §4.C3): is this stream a terminal, so a one-time
// link may be printed to it [CC5]; and is this process in the terminal's
// foreground process group, so it may read it without being stopped by SIGTTIN
// [CC18]. Standard library only plus golang.org/x/sys; no cgo.
package tty
