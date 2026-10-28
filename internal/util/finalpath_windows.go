//go:build windows

package util

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveLinks is abs as Windows reaches it, through junctions as well as symbolic
// links, which filepath.EvalSymlinks has not followed since Go 1.23 (P3-8 Windows
// lab, B2). It walks abs from its drive's root, one name at a time, through the os
// package, so a path past 260 characters is walked too (the P3-8 fixes' pass,
// WINPATH-3): each name that is a link is read and its target takes the place of the
// path so far. A volume mounted in a folder is not a link to a place: it is kept as
// spelled, so a disk mounted at C:\Archives\USB is never recorded by the drive
// letter it may also have, which can change at the next plug-in (WINPATH-2). The
// names from the first one that does not exist on are kept as written. ok is false
// for what this walk does not judge — a path not on a drive letter (a network share,
// whose links the server resolves), a link target it cannot place, or more than
// maxLinks links (a loop) — and the caller keeps what EvalSymlinks gives.
func resolveLinks(abs string) (string, bool) { return walkLinks(abs, os.Lstat, os.Readlink) }

// maxLinks bounds the links one walk follows, against a loop of links.
const maxLinks = 255

// walkLinks is resolveLinks with the file system's two questions as parameters, so a
// test can stand in a link Windows will not let it make (a folder-mounted volume).
func walkLinks(abs string, lstat func(string) (os.FileInfo, error), readlink func(string) (string, error)) (string, bool) {
	cur, rest, ok := splitDrive(abs)
	if !ok {
		return "", false
	}
	followed := 0
	for i := 0; i < len(rest); i++ {
		next := cur + `\` + rest[i]
		fi, err := lstat(next)
		if err != nil {
			// From here on nothing exists to be followed: the rest is kept as written.
			return finish(cur, rest[i:]), true
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			cur = next
			continue
		}
		target, err := readlink(next)
		if err != nil || namesVolume(target) {
			// Not a link to a place (a cloud placeholder, a deduplicated file), or a
			// volume mounted in this folder: kept as spelled.
			cur = next
			continue
		}
		if followed++; followed > maxLinks {
			return "", false
		}
		switch {
		case filepath.IsAbs(target):
		case filepath.VolumeName(target) == "" && strings.HasPrefix(target, `\`):
			target = cur[:2] + target // rooted on this drive
		case filepath.VolumeName(target) == "":
			target = cur + `\` + target // relative to the link's own folder
		default:
			return "", false // a drive-relative target ("D:x"): not placed here
		}
		joined := filepath.Clean(strings.Join(append([]string{target}, rest[i+1:]...), `\`))
		if cur, rest, ok = splitDrive(joined); !ok {
			return "", false
		}
		i = -1
	}
	return finish(cur, nil), true
}

// splitDrive splits an absolute drive-letter path into its drive ("C:") and names.
func splitDrive(p string) (drive string, names []string, ok bool) {
	vol := filepath.VolumeName(p)
	if len(vol) != 2 || vol[1] != ':' || !filepath.IsAbs(p) {
		return "", nil, false
	}
	for _, n := range strings.Split(p[2:], `\`) {
		if n != "" {
			names = append(names, n)
		}
	}
	return vol, names, true
}

// finish joins the walked path and the names kept as written, the walked part
// spelled as the file system holds it — its case, a short 8.3 name written out — as
// EvalSymlinks spells it, when that can be read (it cannot past 260 characters).
// No link is left in the walked part for EvalSymlinks to follow.
func finish(walked string, kept []string) string {
	if len(walked) == 2 {
		walked += `\`
	}
	if spelled, err := filepath.EvalSymlinks(walked); err == nil {
		walked = spelled
	}
	return filepath.Join(append([]string{walked}, kept...)...)
}

// namesVolume says a link target names a volume (\\?\Volume{…}\ or Volume{…}): the
// folder it stands in is where that volume is mounted, not a link to elsewhere.
func namesVolume(target string) bool {
	t := target
	for _, prefix := range []string{`\\?\`, `\??\`, `\\.\`} {
		t = strings.TrimPrefix(t, prefix)
	}
	return len(t) >= len("Volume{") && strings.EqualFold(t[:len("Volume{")], "Volume{")
}
