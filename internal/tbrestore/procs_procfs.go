package tbrestore

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// countProcThunderbird is the Linux process probe (design §5.1 note: new code in
// slice R3), over a /proc tree at root: every process of uid whose executable
// (/proc/PID/exe) is a Thunderbird by presync's name rule, or, when that link cannot
// be read, whose command name (/proc/PID/comm) is one. known is false when the tree
// itself cannot be listed. It is written over any root so a test can give it a tree
// of its own; it builds on every platform, and only Linux calls it.
func countProcThunderbird(root string, uid int, ownerOf func(os.FileInfo) (int, bool)) (n int, known bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		dir := filepath.Join(root, e.Name())
		fi, err := os.Lstat(dir)
		if err != nil {
			continue // ended meanwhile
		}
		if owner, ok := ownerOf(fi); !ok || owner != uid {
			continue
		}
		if exe, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
			if isThunderbirdExe(exe) {
				n++
			}
			continue
		}
		if comm, err := os.ReadFile(filepath.Join(dir, "comm")); err == nil && isThunderbirdExe(strings.TrimSpace(string(comm))) {
			n++
		}
	}
	return n, true
}
