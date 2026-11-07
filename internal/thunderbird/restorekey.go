package thunderbird

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// The names Restore to Thunderbird shares with every other caller that must find a
// restore in progress (docs/design-restore-thunderbird.md §3.4 foldcheck note N3, §5.2
// note item 3, §7.4 foldcheck note M4): presync, which must never start Thunderbird
// on a profile a restore is writing into; scheduled backups, which leave a restore's
// folder out; uninstall, which removes the files; and the restore itself. One
// function computes each, so they cannot disagree.

// ProfileKey is the key of one Thunderbird profile: the first 16 hex characters of
// the SHA-256 of its canonical path. The path's links are resolved (EvalSymlinks;
// when that fails, the path as given), it is made absolute and clean, and on Windows
// and macOS it is case-folded and NFC-normalised (their usual file systems ignore
// case and, on macOS, normalisation), so one profile spelled two ways has one key.
// Folding can only make two profiles share one key, the safe direction: a restore
// lock then keeps out a little more than it must.
//
// Settled 2026-11-07 [R6-15]: a spelling no link resolves — macOS's /System/Volumes/Data
// firmlink, a Windows junction or subst drive — reached one profile by two keys (two
// restore locks, two settle scopes, presync blind to one). So a profile folder that exists
// is keyed by its identity (device and inode; on Windows volume serial number and file
// index), whatever path reaches it; the path's key above is kept only for a folder whose
// identity cannot be read (gone, or a volume that reports none).
func ProfileKey(dir string) string {
	if vol, file, ok := folderIdentity(dir); ok {
		sum := sha256.Sum256([]byte(fmt.Sprintf("identity\x00%d\x00%d", vol, file)))
		return hex.EncodeToString(sum[:])[:16]
	}
	return profileKey(dir, runtime.GOOS)
}

func profileKey(dir, goos string) string {
	p := dir
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		p = r
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	p = filepath.Clean(p)
	if goos == "windows" || goos == "darwin" {
		p = util.FoldKey(p)
	}
	sum := sha256.Sum256([]byte(p))
	return hex.EncodeToString(sum[:])[:16]
}

// RestoreLockPrefix and RestoreLockSuffix frame a MailArchive restore lock's name:
// "restore-" + ProfileKey + ".lock", in MailArchive's default settings folder.
const (
	RestoreLockPrefix = "restore-"
	RestoreLockSuffix = ".lock"
)

// RestoreLockName is the MailArchive restore lock's file name for a profile key.
func RestoreLockName(key string) string { return RestoreLockPrefix + key + RestoreLockSuffix }

// IsRestoreLockName reports whether name is a restore lock's name: "restore-", then
// exactly 16 lowercase hex characters, then ".lock" (uninstall's pattern, design §7.4
// foldcheck2 note N-b).
func IsRestoreLockName(name string) bool {
	key, ok := strings.CutPrefix(name, RestoreLockPrefix)
	if !ok {
		return false
	}
	key, ok = strings.CutSuffix(key, RestoreLockSuffix)
	if !ok || len(key) != 16 {
		return false
	}
	for _, c := range key {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// RestoreLockPath is the restore lock of a profile in MailArchive's default settings
// folder, job.Dir(), always, never the folder of a -config file (§7.4 foldcheck note
// M4): presync and scheduled backups never take -config, so they always look here.
func RestoreLockPath(profileDir string) (string, error) {
	d, err := job.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, RestoreLockName(ProfileKey(profileDir))), nil
}
