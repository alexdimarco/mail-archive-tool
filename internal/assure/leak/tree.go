package leak

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// Mode is when a tree is checked: at rest (between runs), or in flight (during a
// run, when the sweepable temps of an interrupted write may also be there).
type Mode int

const (
	AtRest Mode = iota
	InFlight
)

// Violation is one path an encrypted archive's folder may not hold, and why.
type Violation struct{ Path, Why string }

// rootFiles are the files an encrypted archive may hold at its root: the vault and
// its mirror, the lock, the minimised run records and notices (A6), the empty pause
// marker, the encrypted README, and the sealed record's envelope, root pointer and
// mirror, and inventory top file (phase2b §7).
var rootFiles = map[string]bool{
	vault.FileName: true, vault.BackupName: true, lockfile.Name: true,
	state.LastRunName: true, state.RunHistoryName: true, state.SourceStreakName: true,
	state.LastVerifyName: true, state.AttentionName: true, state.IntegrityAttentionName: true,
	seal.Name: true, "README.txt": true,
	container.EnvelopeName: true, container.PointerName: true, container.MirrorName: true, container.InventoryName: true,
}

// shardedDirs are the folders laid out as two-hex shards of 32-hex files: the unit
// store and the sealed record's chunks.
var shardedDirs = map[string]bool{export.UnitsDir: true, container.ChunksDir: true}

var (
	shardRe    = regexp.MustCompile(`^[0-9a-f]{2}$`)
	unitRe     = regexp.MustCompile(`^[0-9a-f]{32}$`)
	invShardRe = regexp.MustCompile(`^[0-9a-f]{2,4}\.[1-9][0-9]*$`)
	rootTempRe = regexp.MustCompile(`^\.mailarchive-[a-z-]*[0-9]+\.tmp$`)
	unitTempRe = regexp.MustCompile(`^\.mailarchive-[0-9]+\.tmp$`)
	// programTempRe is the program step's staging name (program design R11).
	programTempRe = regexp.MustCompile(`^\.mailarchive-program-[0-9]{1,20}\.tmp$`)
)

// CheckTree is every path under root an encrypted archive may not hold, in mode:
// only the root files above; the units/ and chunks/ folders, their two-hex-character
// shard folders, and <hh>/<32 hex> files whose name starts with their shard; the
// inventory/ folder and its <2-4 hex>.<generation> shard files — in flight also the
// sweepable temps (.mailarchive-*.tmp at the root, in a shard and in inventory/).
// Never a symlink or special file, never a snapshot, a log, a page, a report or a
// plain temp. exempt names the root files that are still cleartext until Phase 2b
// seals them (matched by name, or by name + "-" for SQLite's companions): the
// caller's list is the only exemption. It walks with Lstat and reports an
// unreadable entry rather than skipping it.
func CheckTree(root string, mode Mode, exempt map[string]bool) []Violation {
	var vs []Violation
	add := func(p, why string) { vs = append(vs, Violation{p, why}) }
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			add(p, "unreadable: "+err.Error())
			return nil
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		name := parts[len(parts)-1]
		t := d.Type()
		if t&fs.ModeSymlink != 0 || (!t.IsDir() && !t.IsRegular()) {
			add(rel, "a link or special file")
			return nil
		}
		switch {
		case len(parts) == 1 && t.IsDir():
			if !shardedDirs[name] && name != container.InventoryDir && name != container.ProgramDir {
				add(rel, "a folder other than units/, chunks/, inventory/ or program/")
				return filepath.SkipDir
			}
		case parts[0] == container.ProgramDir:
			// The copies of MailArchive (program design §1, §3): regular files of §3's
			// names; in flight also the step's own temporary files (the PG pass, INT-04).
			if len(parts) == 2 && t.IsRegular() && (container.ProgramNameOK(name) || (mode == InFlight && programTempRe.MatchString(name))) {
				return nil
			}
			add(rel, "not a program copy")
			if t.IsDir() {
				return filepath.SkipDir
			}
		case len(parts) == 1:
			if rootFiles[name] || exempted(name, exempt) || (mode == InFlight && rootTempRe.MatchString(name)) {
				return nil
			}
			add(rel, "a file an encrypted archive does not hold")
		case parts[0] == container.InventoryDir:
			if len(parts) == 2 && t.IsRegular() && (invShardRe.MatchString(name) || (mode == InFlight && unitTempRe.MatchString(name))) {
				return nil
			}
			add(rel, "not an inventory shard file")
			if t.IsDir() {
				return filepath.SkipDir
			}
		case len(parts) == 2 && t.IsDir():
			if !shardRe.MatchString(name) {
				add(rel, "a folder in "+parts[0]+"/ that is not a shard")
				return filepath.SkipDir
			}
		case len(parts) == 2:
			add(rel, "a file directly in "+parts[0]+"/")
		case len(parts) == 3 && t.IsRegular():
			if unitRe.MatchString(name) && strings.HasPrefix(name, parts[1]) {
				return nil
			}
			if mode == InFlight && unitTempRe.MatchString(name) {
				return nil
			}
			add(rel, "a file in a shard that is not a unit")
		default:
			add(rel, "nested deeper than a unit")
			if t.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		add(root, "walk failed: "+err.Error())
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].Path < vs[j].Path })
	return vs
}

func exempted(name string, exempt map[string]bool) bool {
	for e := range exempt {
		if name == e || strings.HasPrefix(name, e+"-") {
			return true
		}
	}
	return false
}
