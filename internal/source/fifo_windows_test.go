//go:build windows

package source

import "errors"

// mkfifo: there are no FIFOs on Windows; a test that needs one skips on the error.
func mkfifo(path string) error { return errors.New("no FIFOs on Windows") }
