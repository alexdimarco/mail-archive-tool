//go:build windows

package util

import (
	"strings"

	"golang.org/x/sys/windows"
)

// finalPath is the path Windows itself resolves p to — through junctions and
// folder-mounted links as well as symbolic links — which filepath.EvalSymlinks has
// not followed since Go 1.23 (P3-8 Windows lab, B2). ok is false when it cannot be
// told (p cannot be opened, or a volume mounted in a folder has no drive letter): the
// caller keeps what it had.
func finalPath(p string) (string, bool) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	for {
		// Flags 0: FILE_NAME_NORMALIZED with VOLUME_NAME_DOS, a drive-letter path.
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", false
		}
		if int(n) < len(buf) {
			s := windows.UTF16ToString(buf[:n])
			switch {
			case strings.HasPrefix(s, `\\?\UNC\`):
				return `\\` + s[len(`\\?\UNC\`):], true
			case strings.HasPrefix(s, `\\?\`):
				return s[len(`\\?\`):], true
			}
			return s, true
		}
		buf = make([]uint16, n)
	}
}
