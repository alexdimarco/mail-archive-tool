//go:build !darwin && !windows

package presyncjob

import "errors"

// StateDir has no location where there is no pre-sync adapter (Linux, TP7):
// SetHook installs nothing there before it would ask.
func StateDir() (string, error) {
	return "", errors.New("the Thunderbird sync is only available on macOS and Windows in this version")
}
