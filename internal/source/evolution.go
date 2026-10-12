package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mail-archive-tool/internal/util"
)

// Evolution (GNOME's mail client) stores mail as maildirs in two layouts this
// reader understands:
//
//   - The local "On This Computer" store (~/.local/share/evolution/mail/local)
//     is a Maildir++ store: the top directory is INBOX, and every other folder
//     is a sibling directory whose name encodes the hierarchy with '.' separators
//     and _XX hex-escapes reserved characters (".a_2Eb.INBOX.Tickets"). A
//     "..maildir++" marker file sits at the root.
//
//   - Each IMAP account's disk cache (~/.cache/evolution/mail/<uid>) keeps a
//     "folders" directory of per-folder maildirs; nested folders live either as
//     child directories or under an explicit "subfolders" container.
//
// In both layouts each message file is a raw RFC 5322 message, so parsing reuses
// the same maildir reader as the Thunderbird/mbox path — only folder discovery
// differs.

// maildirPlusPlusMarker is the file Evolution writes at the root of a Maildir++
// store.
const maildirPlusPlusMarker = "..maildir++"

type evoKind int

const (
	evoMaildirPP evoKind = iota // local Maildir++ store
	evoCache                    // Camel disk cache (IMAP account)
)

// evolutionReader reads an Evolution mail store. It implements Source.
type evolutionReader struct {
	root   string
	store  string
	kind   evoKind
	follow bool // the input's directory-link opt-in (OpenOptions.FollowLinks)
}

// openEvolution returns a reader if dir is an Evolution store, else (nil, false)
// so Open can fall through to the generic mbox/maildir reader.
func openEvolution(dir string, opts OpenOptions) (*evolutionReader, bool) {
	switch {
	case isEvolutionCacheStore(dir):
		return &evolutionReader{root: dir, kind: evoCache, store: evolutionStoreName(dir, evoCache), follow: opts.FollowLinks}, true
	case isEvolutionMaildirStore(dir):
		return &evolutionReader{root: dir, kind: evoMaildirPP, store: evolutionStoreName(dir, evoMaildirPP), follow: opts.FollowLinks}, true
	default:
		return nil, false
	}
}

func (r *evolutionReader) StoreName() string { return r.store }
func (r *evolutionReader) Close() error      { return nil }

// Walk visits every message; the items and folders it could not read come back as a
// *FaultsError (WalkWithFaults delivers them as they happen).
func (r *evolutionReader) Walk(handler MessageHandler) error {
	return r.WalkWithFaults(handler, WalkOptions{})
}

// WalkWithFaults is Walk that delivers each fault through opts.Faults as it happens.
// The store is read under R25 (localstore.go): its root resolved and opened once, every
// entry judged before it is opened.
func (r *evolutionReader) WalkWithFaults(handler MessageHandler, opts WalkOptions) error {
	w := newLocalWalk(handler, opts, r.follow)
	return w.sink.result(r.walk(w))
}

func (r *evolutionReader) walk(w *localWalk) error {
	sc, err := openScope(r.root)
	if err != nil {
		return fmt.Errorf("read Evolution store %s: %w", r.root, err)
	}
	defer sc.close()
	if w.follow {
		if fi, err := os.Stat(sc.path); err == nil {
			w.seen(fi)
		}
	}
	var walkErr error
	if r.kind == evoCache {
		walkErr = r.walkCache(w, sc)
	} else {
		walkErr = r.walkMaildirPP(w, sc)
	}
	if walkErr != nil {
		return walkErr
	}
	return w.drain()
}

// isEvolutionMaildirStore reports whether dir is an Evolution Maildir++ store:
// a maildir carrying the ..maildir++ marker, or (as a fallback) a maildir that
// also holds at least one dot-prefixed maildir subfolder.
func isEvolutionMaildirStore(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, maildirPlusPlusMarker)); err == nil && !fi.IsDir() {
		return true
	}
	if !isMaildir(dir) {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".") && isMaildir(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

// isEvolutionCacheStore reports whether dir is a Camel disk cache store: it holds
// a "folders" directory containing at least one folder that is a maildir or has
// nested subfolders.
func isEvolutionCacheStore(dir string) bool {
	foldersDir := filepath.Join(dir, "folders")
	fi, err := os.Stat(foldersDir)
	if err != nil || !fi.IsDir() {
		return false
	}
	entries, err := os.ReadDir(foldersDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && evolutionFolderHasMail(filepath.Join(foldersDir, e.Name())) {
			return true
		}
	}
	return false
}

// evolutionFolderHasMail reports whether a cache folder directory carries mail
// (is a maildir) or holds nested subfolders.
func evolutionFolderHasMail(dir string) bool {
	if isMaildir(dir) {
		return true
	}
	if fi, err := os.Stat(filepath.Join(dir, "subfolders")); err == nil && fi.IsDir() {
		return true
	}
	return false
}

// walkMaildirPP reads the root INBOX and every dot-encoded subfolder of a
// Maildir++ store.
func (r *evolutionReader) walkMaildirPP(w *localWalk, sc *scope) error {
	// The root maildir is the store's INBOX; its messages sit at the top level
	// (folderPath nil ⇒ no redundant nesting, mirroring a single maildir store).
	if err := w.maildir(sc, ".", nil); err != nil {
		return err
	}

	entries, err := sc.readDir(".")
	if err != nil {
		return fmt.Errorf("read Evolution store %s: %w", r.root, err)
	}
	for _, e := range entries {
		if err := w.abort(); err != nil {
			return err
		}
		name := e.Name()
		if !strings.HasPrefix(name, ".") {
			continue // internals (cur/new/tmp), sidecar files, or the marker
		}
		v := w.judge(sc, name, e.Type(), posEither)
		if v.kind != kindDir && v.kind != kindDirLink {
			// The marker or a sidecar file is no folder. A link whose target is out of the
			// store, though, may be a folder that holds mail (a link is told from a folder
			// only by touching its target, which R25 never does): it is recorded, never
			// dropped, and so is a pipe or device standing where a folder would be.
			if v.kind == kindOutside || v.kind == kindSpecial {
				w.refuse(sc, name, nil, v)
			}
			continue
		}
		folderPath := decodeMaildirName(name)
		if len(folderPath) == 0 {
			continue // undecodable name; its mail is already covered by the root read
		}
		if v.kind == kindDir && !isMaildirIn(sc, name) {
			continue
		}
		if err := w.into(sc, name, v, folderPath, func(s *scope, rel string) error {
			if !isMaildirIn(s, rel) {
				return nil
			}
			return w.maildir(s, rel, folderPath)
		}); err != nil {
			return err
		}
	}
	return nil
}

// walkCache walks a Camel disk cache: each entry under "folders" is a top-level
// folder.
func (r *evolutionReader) walkCache(w *localWalk, sc *scope) error {
	return w.cacheInto(sc, "folders", nil, r.walkCacheChildren)
}

// cacheInto reads the container directory at rel (the "folders" directory, a "subfolders"
// container) with read, if its place allows it: a link there is a directory link,
// recorded and not followed; an absent container holds nothing.
func (w *localWalk) cacheInto(sc *scope, rel string, prefix []string, read func(*localWalk, *scope, string, []string) error) error {
	fi, err := sc.lstat(rel)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			w.folderFault(sc, rel, prefix, "folder-unreadable", err, "the folder container could not be read: ")
		}
		return nil
	}
	v := w.judge(sc, rel, fi.Mode(), posDir)
	if v.kind != kindDir && v.kind != kindDirLink {
		return nil // not a container
	}
	return w.into(sc, rel, v, prefix, func(s *scope, r string) error { return read(w, s, r, prefix) })
}

// walkCacheChildren treats each subdirectory of dir as a folder that extends
// prefix, reading its mail and recursing into its subfolders. Maildir internals
// and the "subfolders" container are not themselves folders.
func (r *evolutionReader) walkCacheChildren(w *localWalk, sc *scope, dir string, prefix []string) error {
	entries, err := sc.readDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			w.folderFault(sc, dir, prefix, "folder-unreadable", err, "the folder container could not be listed: ")
		}
		return nil // absent container: nothing to read
	}
	for _, e := range entries {
		if err := w.abort(); err != nil {
			return err
		}
		switch e.Name() {
		case "cur", "new", "tmp", "subfolders":
			continue
		}
		erel := relJoin(dir, e.Name())
		v := w.judge(sc, erel, e.Type(), posEither)
		if v.kind != kindDir && v.kind != kindDirLink {
			if v.kind == kindOutside || v.kind == kindSpecial {
				if v.kind == kindOutside && e.Type()&fs.ModeSymlink == 0 {
					continue // not a folder, and no link either
				}
				w.refuse(sc, erel, prefix, v)
			}
			continue
		}
		child := appendSeg(prefix, util.SanitizeSegment(e.Name()))
		if err := w.into(sc, erel, v, child, func(s *scope, rel string) error { return r.walkCacheFolder(w, s, rel, child) }); err != nil {
			return err
		}
	}
	return nil
}

// walkCacheFolder reads one cache folder's messages, then descends into its
// subfolders — nested directly under it and/or under a "subfolders" container.
func (r *evolutionReader) walkCacheFolder(w *localWalk, sc *scope, dir string, folderPath []string) error {
	if err := w.maildir(sc, dir, folderPath); err != nil {
		return err
	}
	if err := r.walkCacheChildren(w, sc, dir, folderPath); err != nil {
		return err
	}
	return w.cacheInto(sc, relJoin(dir, "subfolders"), folderPath, func(w *localWalk, s *scope, rel string, prefix []string) error {
		return r.walkCacheChildren(w, s, rel, prefix)
	})
}

// decodeMaildirName turns a Maildir++ subfolder directory name into a sanitized
// folder path. Hierarchy uses '.' separators; reserved characters within a
// component are escaped as _XX (two hex digits). Empty components are dropped and
// each surviving component is sanitized, so a decoded '/' or leading dot cannot
// escape the output root (R4).
//
//	".ischool_2Eutoronto_2Eca.INBOX.Tickets" → ["ischool.utoronto.ca","INBOX","Tickets"]
func decodeMaildirName(name string) []string {
	name = strings.TrimPrefix(name, ".")
	var out []string
	for _, comp := range strings.Split(name, ".") {
		if comp == "" {
			continue
		}
		if seg := util.SanitizeSegment(unescapeMaildir(comp)); seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// unescapeMaildir replaces _XX hex escapes with the byte they denote.
func unescapeMaildir(s string) string {
	if !strings.Contains(s, "_") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '_' && i+2 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(v))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// evolutionStoreName derives a human-readable label for the store (the top-level
// output directory). IMAP account UIDs are resolved to their account name; the
// local store is shown as "On This Computer".
func evolutionStoreName(dir string, kind evoKind) string {
	base := filepath.Base(strings.TrimRight(dir, string(os.PathSeparator)))
	if kind == evoCache {
		if name := evolutionAccountName(base); name != "" {
			return name
		}
		return "Evolution " + base
	}
	if base == "local" {
		return "On This Computer"
	}
	return base
}

// evolutionAccountName resolves a Camel cache account UID to its friendly name
// via ~/.config/evolution/sources/<uid>.source (a keyfile with DisplayName=...).
func evolutionAccountName(uid string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, p := range []string{
		filepath.Join(home, ".config", "evolution", "sources", uid+".source"),
		filepath.Join(home, ".var", "app", "org.gnome.Evolution", "config", "evolution", "sources", uid+".source"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "DisplayName=") {
				if v := strings.TrimSpace(strings.TrimPrefix(line, "DisplayName=")); v != "" {
					return v
				}
			}
		}
	}
	return ""
}

// appendSeg returns a fresh slice of prefix with seg appended, so recursive
// walks never alias a shared backing array.
func appendSeg(prefix []string, seg string) []string {
	out := make([]string, 0, len(prefix)+1)
	out = append(out, prefix...)
	return append(out, seg)
}
