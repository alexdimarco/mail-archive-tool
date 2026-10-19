package app

import (
	"runtime"
	"strings"
)

// mdName rewrites the maildir info separator ':' to one legal on the host: ':' on
// unix, '!' on Windows, where ':' is illegal in a filename (os.WriteFile would make
// an NTFS alternate data stream, not a file) and which the maildir parser
// (maildirUnique) also accepts. Applied to a maildir fixture path so the file
// written and the name read back are identical on every OS.
func mdName(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(s, ":", "!")
	}
	return s
}
