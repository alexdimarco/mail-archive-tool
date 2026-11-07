//go:build !windows

package tbengine

import "os"

// elevated reports a process running as root on macOS and Linux (design §9.6,
// W-ELEVATED): files it made would belong to root, so Thunderbird could not change them.
func elevated() bool { return os.Geteuid() == 0 }

const elevatedWords = wElevatedUnix
