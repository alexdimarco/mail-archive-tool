//go:build windows

package util

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// standInfo is a stand-in Lstat answer: only its mode is asked.
type standInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (s standInfo) Mode() os.FileMode { return s.mode }

// covers: MA-854, R37
// What the walk does with each kind of link target (the P3-8 fixes' pass, WINPATH-2),
// stood in because a test cannot always mount a volume in a folder: a volume mounted
// in a folder — its target \\?\Volume{…}\, in any of the spellings Windows gives — is
// kept as the folder's path, so a disk mounted at C:\Archives\USB is never recorded by
// the drive letter it may also have; a junction or symbolic link to a drive path, a
// rooted target and a relative one each lead where they point and the walk goes on
// through the target; a reparse point that names no place (a cloud placeholder) is
// kept; a loop of links ends the walk (the caller then keeps what it had); names past
// the first missing one are kept as written; a network path is left to the caller.
// Real on Windows: the folder-mounted disk lab test (MAILARCHIVE_MOUNT_LAB=1).
func TestAFolderMountedVolumeIsKeptAndALinkIsFollowed(t *testing.T) {
	links := map[string]string{
		`C:\Archives\USB`:         `\\?\Volume{5a9e4878-dc09-4f79-bf5a-4b3c768c8225}\`,
		`C:\Archives\Card`:        `\??\Volume{5a9e4878-dc09-4f79-bf5a-4b3c768c8226}\`,
		`C:\Archives\Bare`:        `Volume{5a9e4878-dc09-4f79-bf5a-4b3c768c8227}\`,
		`C:\Plain`:                `C:\Users\me\Dropbox`,
		`C:\Users\me\Dropbox\Up`:  `..\Box`,
		`C:\Rooted`:               `\Users\me\Dropbox`,
		`C:\Users\me\Box\Again`:   `C:\Plain`,
		`C:\Loop`:                 `C:\Loop2`,
		`C:\Loop2`:                `C:\Loop`,
		`C:\Users\me\OneDrive\pl`: "", // a cloud placeholder: a reparse point that is no link
	}
	dirs := map[string]bool{`C:\Users`: true, `C:\Users\me`: true, `C:\Users\me\Dropbox`: true, `C:\Users\me\Box`: true,
		`C:\Users\me\OneDrive`: true, `C:\Archives`: true, `C:\Archives\USB\Mail`: true, `C:\Archives\Card\Mail`: true,
		`C:\Archives\Bare\Mail`: true, `C:\Users\me\Dropbox\Mail`: true}
	lstat := func(p string) (os.FileInfo, error) {
		if _, ok := links[p]; ok {
			return standInfo{mode: os.ModeIrregular}, nil
		}
		if dirs[p] {
			return standInfo{mode: os.ModeDir}, nil
		}
		return nil, os.ErrNotExist
	}
	readlink := func(p string) (string, error) {
		if target := links[p]; target != "" {
			return target, nil
		}
		return "", os.ErrInvalid
	}
	walk := func(p string) string {
		got, ok := walkLinks(p, lstat, readlink)
		if !ok {
			return "(not judged)"
		}
		return got
	}
	// Positive twin: a path with no link is itself.
	if got := walk(`C:\Users\me\Dropbox\Mail\copy`); !strings.EqualFold(got, `C:\Users\me\Dropbox\Mail\copy`) {
		t.Fatalf("positive twin: a path with no link: %s", got)
	}
	for in, want := range map[string]string{
		`C:\Archives\USB\Mail\copy`:     `C:\Archives\USB\Mail\copy`,
		`C:\Archives\Card\Mail`:         `C:\Archives\Card\Mail`,
		`C:\Archives\Bare\Mail`:         `C:\Archives\Bare\Mail`,
		`C:\Plain\Mail\copy`:            `C:\Users\me\Dropbox\Mail\copy`,
		`C:\Plain\Up\x`:                 `C:\Users\me\Box\x`,
		`C:\Rooted\Mail`:                `C:\Users\me\Dropbox\Mail`,
		`C:\Users\me\Box\Again\Mail`:    `C:\Users\me\Dropbox\Mail`,
		`C:\Users\me\OneDrive\pl\inner`: `C:\Users\me\OneDrive\pl\inner`,
		`C:\Loop\x`:                     "(not judged)",
		`\\server\share\x`:              "(not judged)",
	} {
		if got := walk(in); !strings.EqualFold(got, want) {
			t.Errorf("%s: walked to %s, want %s", in, got, want)
		}
	}
	assure.Reached(t, strings.EqualFold(walk(`C:\Archives\USB\Mail`), `C:\Archives\USB\Mail`) &&
		strings.EqualFold(walk(`C:\Plain\Mail`), `C:\Users\me\Dropbox\Mail`),
		"a folder-mounted volume kept as spelled, a junction followed")
}
