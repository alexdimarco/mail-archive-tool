package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The archive source (extraction design §4.3, §4.6, slice E4). It reads the configured
// archive, or another archive folder [ruling W1], and writes nothing there: no lock, no
// manifest save, no index repair, no temp file. The folder is untrusted input, so every
// read goes through an *os.Root opened on it, behind the component-wise Lstat walk
// (no symlink at any level, a regular file, one hard link), bounded to the recorded size,
// and held to the fixity the plan recorded. The same adapter serves both archives; only
// the manifest cap and the way search.db is opened differ for another folder.

const (
	archiveManifestName = ".mailarchive-manifest.json"
	archiveIndexName    = "search.db"

	// archiveOtherManifestCap is what another archive folder's manifest may weigh before
	// anything is unmarshalled (R-o4, RR2-C11).
	archiveOtherManifestCap = 1 << 30
)

// ArchiveOptions say how an archive is opened.
type ArchiveOptions struct {
	// Other is another archive folder, chosen with the picker or typed, read-only: the
	// manifest is read under a cap, search.db is opened immutable, and nothing is ever
	// written there.
	Other bool
}

// ArchiveStore is a store of the archive: the token that names its directory and its
// keys, and the mailbox it holds.
type ArchiveStore struct {
	Token   string
	Mailbox string // empty for a store that is not a mailbox (a .pst file, an mbox, a maildir)
	PST     bool   // an Outlook data file, which never has an original
}

// Label is the store as the page names it: the token, and the mailbox it holds.
func (s ArchiveStore) Label() string {
	if s.Mailbox != "" {
		return s.Token + " — " + s.Mailbox
	}
	return s.Token
}

// ArchiveNode is one node of the archive's folder tree: a store, or a folder of one.
type ArchiveNode struct {
	ID      string // "<store>" or "<store>/<folder path>": validated against the snapshot, never used to build a path
	Parent  string // "" for a store
	Name    string
	Count   int // the records filed directly in it
	Store   bool
	Mailbox string // for a store
}

type archiveLoc struct{ token, folder string }

// archiveStamp is what identifies one reading of the manifest: a snapshot is cached by it.
type archiveStamp struct {
	size int64
	mod  time.Time
}

// ArchiveSource is an open archive: a snapshot of its manifest to browse and resolve
// selections from, and the CopyFilesSource a run copies files through.
type ArchiveSource struct {
	dir   string
	other bool

	// root is the handle a source opened for a run holds for the run; a loaded archive holds none
	// between requests, and each request reads through a root it opens (rootFor, E4-AI9).
	rootMu sync.Mutex
	root   *os.Root

	man           *state.Manifest // the snapshot; nil for a source opened only to run
	emptyManifest bool            // the manifest file was zero bytes
	stamp         archiveStamp
	stores        []ArchiveStore
	nodes         []ArchiveNode
	byNode        map[string][]string
	loc           map[string]archiveLoc
	pst           map[string]bool
	mailbox       map[string]string // a store's token → the mailbox it holds

	refMu sync.Mutex
	refs  map[string][]CopyFolderRef // a folder's chain of folders, as the plan names them, made once

	mu       sync.Mutex
	live     *state.Manifest // the manifest as it reads now, for Changed
	liveSeen archiveStamp
}

// OpenArchive opens dir and reads its manifest snapshot. Refusals are *CopyRefusal: R-o
// (no archive at the configured location), R-o3 (another folder with no manifest), R-o4
// (a manifest over the cap) and R-p (a manifest or an index written by a newer
// MailArchive, or a manifest that can't be read).
func OpenArchive(dir string, opt ArchiveOptions) (*ArchiveSource, error) {
	a, err := openArchiveRoot(dir, opt)
	if err != nil {
		return nil, err
	}
	if err := a.load(); err != nil {
		a.Close()
		return nil, err
	}
	a.release() // a loaded archive holds no handle on the folder between requests (E4-AI9)
	return a, nil
}

// OpenArchiveForRun opens dir for a run: the root and nothing else. The run copies from
// the plan's own records and reads the manifest again only when a file no longer matches
// (Changed).
func OpenArchiveForRun(dir string, opt ArchiveOptions) (*ArchiveSource, error) {
	return openArchiveRoot(dir, opt)
}

func openArchiveRoot(dir string, opt ArchiveOptions) (*ArchiveSource, error) {
	abs := absPath(dir)
	fi, err := os.Stat(abs)
	switch {
	case err != nil && errors.Is(err, fs.ErrNotExist):
		return nil, archiveNoFolder(abs, opt.Other)
	case err != nil && opt.Other:
		return nil, refuse("R-o3", fmt.Sprintf("%s can't be opened (%s) — check that this account can read the folder, or click Browse to choose another archive folder.", printable(abs), readName(err)))
	case err != nil:
		return nil, archiveMissing(abs, opt.Other, "")
	case !fi.IsDir():
		return nil, archiveNotFolder(abs, opt.Other)
	}
	root, err := openArchiveDir(abs)
	if err != nil {
		return nil, archiveMissing(abs, opt.Other, "")
	}
	return &ArchiveSource{dir: abs, other: opt.Other, root: root}, nil
}

// archiveNoFolder is R-o3 for a path where there is no folder at all (a typo, a drive that is not
// connected), and R-o for the configured archive (friction F10).
func archiveNoFolder(dir string, other bool) *CopyRefusal {
	if other {
		return refuse("R-o3", fmt.Sprintf("There is no folder at %s — check the spelling, or click Browse to choose the archive's top folder, the one that holds index.html and README.txt.", printable(dir)))
	}
	return archiveMissing(dir, other, "")
}

// archiveNotFolder is R-o3 for a path that is a file.
func archiveNotFolder(dir string, other bool) *CopyRefusal {
	if other {
		return refuse("R-o3", fmt.Sprintf("%s is a file, not a folder — choose the archive's top folder, the one that holds index.html and README.txt, or click Browse.", printable(dir)))
	}
	return archiveMissing(dir, other, "")
}

// archiveMissing is R-o for the configured archive and R-o3 for another folder that is a folder but
// holds no archive's manifest. why, when set, says what is wrong with the manifest that is there.
func archiveMissing(dir string, other bool, why string) *CopyRefusal {
	if other {
		what := "it has no " + archiveManifestName
		if why != "" {
			what = "its " + archiveManifestName + why
		}
		return refuse("R-o3", fmt.Sprintf("%s isn't a MailArchive archive (%s) — choose the archive's top folder, the one that holds index.html and README.txt.", printable(dir), what))
	}
	return refuse("R-o", fmt.Sprintf("There's no archive at %s yet — run Archive now first, or choose Microsoft 365 as the source.", printable(dir)))
}

// ArchiveAt reports whether dir holds an archive's manifest as a regular file: the cheap
// check a selection's binding and the source control's availability line make. It reads no
// file and writes nothing.
func ArchiveAt(dir string) bool {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	fi, err := os.Lstat(filepath.Join(dir, archiveManifestName))
	return err == nil && fi.Mode().IsRegular()
}

// ArchivesAbove are the archive folders (a folder with an archive's manifest in it)
// that parent is or lies inside, by the path as it is given and as it resolves,
// nearest first: Desktop's Create folder writes in none of them, and an unencrypted
// copy is made in none of them (phase 3 P3-4).
func ArchivesAbove(parent string) []string {
	var out []string
	seen := map[string]bool{}
	starts := []string{parent}
	if r, err := filepath.EvalSymlinks(parent); err == nil && r != parent {
		starts = append(starts, r)
	}
	for _, start := range starts {
		if !filepath.IsAbs(start) {
			continue
		}
		for p := filepath.Clean(start); ; {
			if !seen[p] && ArchiveAt(p) {
				seen[p] = true
				out = append(out, p)
			}
			up := filepath.Dir(p)
			if up == p {
				break
			}
			p = up
		}
	}
	return out
}

// PlanFoldersOf summarizes msgs by folder path, in the order first met: a retry's selection.
func PlanFoldersOf(msgs []CopyMessage) []CopyPlanFolder { return foldersOf(msgs) }

// Dir is the archive folder's absolute path.
func (a *ArchiveSource) Dir() string { return a.dir }

// Other reports whether this is another archive folder, read-only.
func (a *ArchiveSource) Other() bool { return a.other }

// rootsOpen counts the directory handles the archive source holds on archive folders, so that a test
// can see one that is held between requests (E4-AI9).
var rootsOpen atomic.Int64

// ArchiveRootsOpen is how many archive roots are open now: none between requests of a loaded archive.
func ArchiveRootsOpen() int { return int(rootsOpen.Load()) }

func openArchiveDir(abs string) (*os.Root, error) {
	r, err := os.OpenRoot(abs)
	if err == nil {
		rootsOpen.Add(1)
	}
	return r, err
}

func closeArchiveDir(r *os.Root) {
	if r != nil && r.Close() == nil {
		rootsOpen.Add(-1)
	}
}

// Close releases the root a source opened for a run holds; a loaded archive holds none.
func (a *ArchiveSource) Close() error {
	a.rootMu.Lock()
	r := a.root
	a.root = nil
	a.rootMu.Unlock()
	closeArchiveDir(r)
	return nil
}

// rootFor is the root a request reads the archive through, and what to call when it is done: the one a
// source opened for a run holds, or, for a loaded archive, a root opened for this request and closed at its end
// (E4-AI9). The files a request opened through it stay open after it is closed.
func (a *ArchiveSource) rootFor() (*os.Root, func(), error) {
	a.rootMu.Lock()
	held := a.root
	a.rootMu.Unlock()
	if held != nil {
		return held, func() {}, nil
	}
	r, err := openArchiveDir(a.dir)
	if err != nil {
		return nil, func() {}, archiveMissing(a.dir, a.other, "")
	}
	return r, func() { closeArchiveDir(r) }, nil
}

// release lets go of the root a loaded archive was opened with: from here every request opens its own.
func (a *ArchiveSource) release() { a.Close() }

// Stamp identifies the manifest the snapshot was read from (its size and mtime).
func (a *ArchiveSource) Stamp() (int64, time.Time) { return a.stamp.size, a.stamp.mod }

// StampNow is the manifest's size and mtime as they are now, so a cached snapshot can be
// told from the manifest it was read from.
func (a *ArchiveSource) StampNow() (int64, time.Time, error) {
	root, done, err := a.rootFor()
	if err != nil {
		return 0, time.Time{}, err
	}
	defer done()
	fi, err := root.Lstat(archiveManifestName)
	if err != nil {
		return 0, time.Time{}, err
	}
	return fi.Size(), fi.ModTime(), nil
}

// readManifest reads the manifest through the root, under the cap for another folder.
func (a *ArchiveSource) readManifest() ([]byte, archiveStamp, error) {
	root, done, rerr := a.rootFor()
	if rerr != nil {
		return nil, archiveStamp{}, rerr
	}
	defer done()
	fi, err := root.Lstat(archiveManifestName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, archiveStamp{}, archiveMissing(a.dir, a.other, "")
	case err != nil:
		return nil, archiveStamp{}, archiveMissing(a.dir, a.other, " can't be read: "+readName(err))
	case fi.Mode()&fs.ModeSymlink != 0 || !fi.Mode().IsRegular():
		return nil, archiveStamp{}, archiveMissing(a.dir, a.other, " is a link or a folder, not the archive's own file")
	}
	if a.other && fi.Size() > archiveOtherManifestCap {
		return nil, archiveStamp{}, archiveTooBig(a.dir, fi.Size())
	}
	f, err := openRegularIn(root, archiveManifestName)
	if err != nil {
		return nil, archiveStamp{}, archiveMissing(a.dir, a.other, " can't be read: "+readName(err))
	}
	defer f.Close()
	var r io.Reader = f
	if a.other {
		r = io.LimitReader(f, archiveOtherManifestCap+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, archiveStamp{}, archiveMissing(a.dir, a.other, " can't be read: "+readName(err))
	}
	if a.other && int64(len(data)) > archiveOtherManifestCap {
		return nil, archiveStamp{}, archiveTooBig(a.dir, int64(len(data)))
	}
	return data, archiveStamp{size: fi.Size(), mod: fi.ModTime()}, nil
}

func archiveTooBig(dir string, size int64) *CopyRefusal {
	return refuse("R-o4", fmt.Sprintf("%s's manifest is larger than MailArchive reads from another archive folder (%s; the limit is 1 GB) — nothing was read beyond that check, and nothing was written. If you trust this archive, make it the archive location in Settings (Save asks first when a scheduled backup is installed).", printable(dir), humanBytes(size)))
}

// archiveNewer is R-p for a manifest or an index a newer MailArchive wrote.
func archiveNewer(what string, version int) *CopyRefusal {
	return refuse("R-p", fmt.Sprintf("The archive's %s was written by a newer MailArchive (format %d) — update MailArchive Desktop to export from it.", what, version))
}

// load reads the manifest and builds the tree.
func (a *ArchiveSource) load() error {
	// An encrypted archive's messages are sealed units the tree cannot read by
	// path: refuse it as a source, saying it is encrypted — never "written by a
	// newer MailArchive" (phase2a §5).
	if kerr := RefuseKeyless("export from it", a.dir); kerr != nil {
		return refuse("R-enc", kerr.Error())
	}
	data, stamp, err := a.readManifest()
	if err != nil {
		return err
	}
	man, err := state.LoadFrom(filepath.Join(a.dir, archiveManifestName), data)
	if err != nil {
		var nf *state.NewerFormatError
		if errors.As(err, &nf) {
			return archiveNewer("manifest", nf.Version)
		}
		return archiveManifestDamaged(a.dir, a.other)
	}
	a.man, a.stamp = man, stamp
	a.emptyManifest = len(data) == 0
	if err := a.build(); err != nil {
		a.man = nil
		return refuse("R-p", fmt.Sprintf("The archive's manifest can't be read (%s) — nothing was written.", err.Error()))
	}
	return nil
}

// archiveManifestDamaged is R-p for a manifest that is cut short or is not what MailArchive writes (friction
// F11): an interrupted copy leaves one. The words never carry the parser's own text; for another folder, which
// is read-only, the remedy is a fresh copy, and for the owner's own archive it is the backup.
func archiveManifestDamaged(dir string, other bool) *CopyRefusal {
	if other {
		return refuse("R-p", fmt.Sprintf("The copy of the archive at %s looks incomplete: its manifest (%s) is cut short or damaged, so nothing was read from it, and nothing was written. Copy the archive again from where you got it (an interrupted copy leaves a file like this), then choose the new copy.", printable(dir), archiveManifestName))
	}
	return refuse("R-p", fmt.Sprintf("The archive's manifest (%s in %s) is cut short or damaged — nothing was changed. Restore it from a backup of the archive; if there is none, deleting it makes the next Archive now export every message again (the archived files themselves are untouched).", archiveManifestName, printable(dir)))
}

// Note is a sentence for an archive that holds no messages, or "": a manifest of zero bytes (a copy or a
// sync that has not finished) is told apart from an archive that has had nothing captured yet.
func (a *ArchiveSource) Note() string {
	if a.man == nil || a.man.Len() > 0 {
		return ""
	}
	if a.emptyManifest {
		return "This archive holds no messages yet — its manifest is empty. If the folder is still being copied or synced, wait until that finishes and load it again."
	}
	return "This archive holds no messages yet."
}

// The tree of another archive's manifest is bounded as the Graph tree is (graph.MaxWorkspaceFolders,
// Exchange's own nesting limit) and in the bytes its node ids add up to: every node's id is the whole
// path of its folder, so the cost of a deep path grows with the square of its depth (E4-AO2). The count
// and the bytes are the limits of another archive folder only: the owner's configured archive, which may
// hold several accounts, is the archive R-o4 sends a trusted folder to, and is not refused for its size.
// What one path may spell (depth and the length of a name) is bounded for both.
const (
	archiveMaxFolderDepth = 300
	archiveMaxFolderName  = 1024
	archiveMaxNodes       = 10000
	archiveMaxIDBytes     = 8 << 20
)

// archiveShapeError is a manifest whose folders are beyond what the tree lists.
type archiveShapeError string

func (e archiveShapeError) Error() string { return string(e) }

const (
	archiveTooDeep    = archiveShapeError("a folder in it is nested more than 300 levels deep")
	archiveNameTooBig = archiveShapeError("a folder name in it is longer than 1,024 bytes")
	archiveTooMany    = archiveShapeError("it lists more than 10,000 folders and stores")
	archiveIDsTooBig  = archiveShapeError("its folder names add up to more than 8 MiB")
)

// archiveCanonFolder is a record's folder path as the folder its names spell: empty segments
// ("/Sub", "a//b", "a/") are dropped, so that no folder has the id of its store or of another
// folder (E4-AC8). A path deeper than archiveMaxFolderDepth, or with a name longer than
// archiveMaxFolderName, is refused, before anything is split or joined.
func archiveCanonFolder(folder string) (string, error) {
	depth, rewrite := 0, false
	for i, start := 0, 0; i <= len(folder); i++ {
		if i < len(folder) && folder[i] != '/' {
			continue
		}
		switch n := i - start; {
		case n == 0:
			rewrite = true
		case n > archiveMaxFolderName:
			return "", archiveNameTooBig
		default:
			if depth++; depth > archiveMaxFolderDepth {
				return "", archiveTooDeep
			}
		}
		start = i + 1
	}
	if !rewrite {
		return folder, nil
	}
	var b strings.Builder
	for i, start := 0, 0; i <= len(folder); i++ {
		if i < len(folder) && folder[i] != '/' {
			continue
		}
		if i > start {
			if b.Len() > 0 {
				b.WriteByte('/')
			}
			b.WriteString(folder[start:i])
		}
		start = i + 1
	}
	return b.String(), nil
}

// nodeID is the id of a store's folder in the tree.
func archiveNodeID(token, folder string) string {
	if folder == "" {
		return token
	}
	return token + "/" + folder
}

// build makes the stores, the tree and the keys of each folder from the snapshot: the tree
// is store token → folder path, from each record's key prefix and current Folder, and
// each store is labelled with its mailbox (XC21).
//
// The tree is bounded (archiveMaxFolderDepth and the limits beside it): a manifest over a limit
// is refused with an archiveShapeError before the tree is built. A record's folder is read as
// the folder its names spell (archiveCanonFolder).
func (a *ArchiveSource) build() error {
	recs := a.man.All()
	mailbox, pst := map[string]string{}, map[string]bool{}
	for id, tok := range a.man.Stores {
		if !state.SafeToken(tok) {
			continue
		}
		switch {
		case strings.HasPrefix(id, "mailbox:"):
			mailbox[tok] = util.StripBidi(util.StripControl(strings.TrimPrefix(id, "mailbox:")))
		case strings.HasSuffix(strings.ToLower(id), ".pst"), strings.HasSuffix(strings.ToLower(id), ".ost"):
			pst[tok] = true
		}
	}
	a.pst, a.mailbox = pst, mailbox
	a.byNode = map[string][]string{}
	a.loc = map[string]archiveLoc{}
	folders := map[string]map[string]bool{} // token → every folder path with a record at or below it
	nodes, idBytes := 0, 0
	add := func(id string) error { // one more node in the tree, within its limits
		if !a.other { // the owner's own archive: its size is its owner's (R-o4's remedy for a trusted folder)
			return nil
		}
		nodes++
		idBytes += len(id)
		switch {
		case nodes > archiveMaxNodes:
			return archiveTooMany
		case idBytes > archiveMaxIDBytes:
			return archiveIDsTooBig
		}
		return nil
	}
	store := func(tok string) error {
		if folders[tok] != nil {
			return nil
		}
		folders[tok] = map[string]bool{} // a store with no record yet is still a store
		return add(tok)
	}
	for key, rec := range recs {
		tok, _, _ := strings.Cut(key, "\x00")
		if !state.SafeToken(tok) {
			continue
		}
		folder, err := archiveCanonFolder(rec.Folder)
		if err != nil {
			return err
		}
		if err := store(tok); err != nil {
			return err
		}
		id := archiveNodeID(tok, folder)
		a.byNode[id] = append(a.byNode[id], key)
		if folder == "" || folders[tok][folder] {
			continue
		}
		// every prefix of the path is a folder, the longest first: a prefix that is already there
		// has all of its own prefixes there too, so only the new ones are made (and counted)
		segs := strings.Split(folder, "/")
		for i := len(segs); i >= 1; i-- {
			p := folder
			if i < len(segs) {
				p = strings.Join(segs[:i], "/")
			}
			if folders[tok][p] {
				break
			}
			folders[tok][p] = true
			if err := add(archiveNodeID(tok, p)); err != nil {
				return err
			}
		}
	}
	for _, tok := range a.man.Stores {
		if state.SafeToken(tok) {
			if err := store(tok); err != nil {
				return err
			}
		}
	}
	tokens := make([]string, 0, len(folders))
	for tok := range folders {
		tokens = append(tokens, tok)
	}
	sort.Strings(tokens)
	a.stores, a.nodes = nil, nil
	a.refs = nil
	for _, tok := range tokens {
		a.stores = append(a.stores, ArchiveStore{Token: tok, Mailbox: mailbox[tok], PST: pst[tok]})
		a.nodes = append(a.nodes, ArchiveNode{ID: tok, Name: tok, Count: len(a.byNode[tok]), Store: true, Mailbox: mailbox[tok]})
		a.loc[tok] = archiveLoc{token: tok}
		paths := make([]string, 0, len(folders[tok]))
		for p := range folders[tok] {
			paths = append(paths, p)
		}
		sort.Slice(paths, func(i, j int) bool { return pathLess(paths[i], paths[j]) })
		for _, p := range paths {
			parent, name := tok, p // a path's parent is its own prefix: a substring, nothing joined
			if i := strings.LastIndexByte(p, '/'); i >= 0 {
				parent, name = archiveNodeID(tok, p[:i]), p[i+1:]
			}
			id := archiveNodeID(tok, p)
			a.nodes = append(a.nodes, ArchiveNode{ID: id, Parent: parent, Name: plainText(name), Count: len(a.byNode[id])})
			a.loc[id] = archiveLoc{token: tok, folder: p}
		}
	}
	for id := range a.byNode {
		sort.Strings(a.byNode[id]) // a stable order of keys within a folder
	}
	return nil
}

// pathLess orders two folder paths segment by segment, so a folder comes right before its
// children and its siblings' subtrees stay together.
func pathLess(a, b string) bool {
	for {
		as, ar, aMore := strings.Cut(a, "/")
		bs, br, bMore := strings.Cut(b, "/")
		if as != bs {
			return as < bs
		}
		switch {
		case !aMore:
			return bMore // a is b, or the folder b is below
		case !bMore:
			return false
		}
		a, b = ar, br
	}
}

// folderRefs is the chain of folders a record filed in folder of store tok is in: the store, then
// each level, as the plan names them. It is made once per folder, each from its parent's, so a
// deep path costs its depth and not the square of it (E4-AO2).
func (a *ArchiveSource) folderRefs(tok, folder string) []CopyFolderRef {
	a.refMu.Lock()
	defer a.refMu.Unlock()
	return append([]CopyFolderRef(nil), a.refsLocked(tok, folder)...)
}

func (a *ArchiveSource) refsLocked(tok, folder string) []CopyFolderRef {
	id := archiveNodeID(tok, folder)
	if r, ok := a.refs[id]; ok {
		return r
	}
	var r []CopyFolderRef
	if folder == "" {
		r = []CopyFolderRef{{ID: tok, Name: tok}}
	} else {
		parent, name := "", folder
		if i := strings.LastIndexByte(folder, '/'); i >= 0 {
			parent, name = folder[:i], folder[i+1:]
		}
		up := a.refsLocked(tok, parent)
		r = append(append(make([]CopyFolderRef, 0, len(up)+1), up...), CopyFolderRef{ID: id, Name: name})
	}
	if a.refs == nil {
		a.refs = map[string][]CopyFolderRef{}
	}
	a.refs[id] = r
	return r
}

// Stores are the stores the archive holds, by token.
func (a *ArchiveSource) Stores() []ArchiveStore { return append([]ArchiveStore(nil), a.stores...) }

// Tree is the folder tree in tree order: each store, then its folders, depth first.
func (a *ArchiveSource) Tree() []ArchiveNode { return append([]ArchiveNode(nil), a.nodes...) }

// Records is how many records the snapshot holds.
func (a *ArchiveSource) Records() int { return a.man.Len() }

// Rekeyed is how many records the load re-scoped in memory (a pre-v3 manifest).
func (a *ArchiveSource) Rekeyed() int { return a.man.Rekeyed }

// Locate resolves a tree node id to its store and folder path.
func (a *ArchiveSource) Locate(id string) (token, folder string, ok bool) {
	l, ok := a.loc[id]
	return l.token, l.folder, ok
}

// Keys are the manifest keys of the records filed directly in node id.
func (a *ArchiveSource) Keys(id string) []string { return append([]string(nil), a.byNode[id]...) }

// Describe names the source for the header and the manifest (XC21).
func (a *ArchiveSource) Describe() CopySourceInfo {
	info := CopySourceInfo{Kind: "archive", Archive: a.dir, Other: a.other,
		OriginalMeans: "the .eml file the archive kept for the message, copied byte-exact"}
	for _, s := range a.stores {
		info.Stores = append(info.Stores, CopyStoreInfo{Store: s.Token, Mailbox: s.Mailbox})
	}
	return info
}

// ---- the index -------------------------------------------------------------------

// OpenList opens search.db for one list request, read-only (immutable for another
// archive folder), after the same gate every archive file passes: no symlink, a regular
// file, one hard link. The caller closes it at the end of the request. A -wal beside the
// file is not read by an immutable open; WALBeside says so, for the page's note.
func (a *ArchiveSource) OpenList() (*index.ListHandle, error) {
	f, _, err := a.openGated(archiveIndexName)
	if err != nil {
		var af *ArchiveFileError
		if errors.As(err, &af) && af.Kind == AFMissing {
			return nil, fmt.Errorf("no search index in the archive: %w", fs.ErrNotExist)
		}
		return nil, err
	}
	f.Close()
	return index.OpenForList(filepath.Join(a.dir, archiveIndexName), index.ListOptions{Immutable: a.other})
}

// WALBeside reports whether a non-empty search.db-wal sits beside the index: an immutable
// open does not read it, so the list may miss recent messages.
func (a *ArchiveSource) WALBeside() bool {
	root, done, err := a.rootFor()
	if err != nil {
		return false
	}
	defer done()
	fi, err := root.Lstat(archiveIndexName + "-wal")
	return err == nil && fi.Size() > 0
}

// MapKey is the manifest key an index row stands for. A row's key is the manifest's when
// the manifest has it; for an index of the pre-meta format, or a manifest that was
// re-scoped in memory, the key is mapped through state.MigrateKey as the loader does.
func (a *ArchiveSource) MapKey(version int, row index.ListRow) (string, bool) {
	if _, ok := a.man.Get(row.Key); ok {
		return row.Key, true
	}
	if version < 2 || a.man.Rekeyed > 0 {
		if k, ok := state.MigrateKey(row.Key, row.Path); ok {
			if _, in := a.man.Get(k); in {
				return k, true
			}
		}
	}
	return "", false
}

// ---- resolution --------------------------------------------------------------------

// ArchiveResolved is what resolving keys into a plan's messages found.
type ArchiveResolved struct {
	Messages []CopyMessage
	// Skipped is how many records were left out because their files were deleted but the
	// manifest and index still list them (a redaction not yet reindexed, §4.6 item 5).
	// Their subject, sender and time are never written into the export.
	Skipped int
	// Vanished are the keys, of those Skipped, whose record the manifest still has but whose files
	// are gone: a retry carries such a message into its plan, where the run lists it as removed from
	// the archive, so that no part of a set offers to export it for ever (E4-AC10).
	Vanished []string
}

// Messages resolves manifest keys into the plan's messages, in the order given, with the
// redaction belt (§4.6 item 5): the record's page, and the files its fixity says exist,
// are Lstat-ed through the root, and a record whose files are gone is left out and
// counted. meta carries what the index knows of a message (subject, sender, date) where
// it could be read; without it the manifest's own fields are used. progress, when set, is
// called now and then with how many keys have been read.
func (a *ArchiveSource) Messages(ctx context.Context, keys []string, meta map[string]index.ListRow, progress func(done, total int)) (ArchiveResolved, error) {
	var out ArchiveResolved
	root, done, err := a.rootFor() // one root for the whole resolution, closed at its end (E4-AI9)
	if err != nil {
		return out, err
	}
	defer done()
	for i, key := range keys {
		// a cancel is looked for at every record, not every 256th: a record without an original
		// reads its archived page whole, up to 32 MiB, and parses it twice (E4-AC7)
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if i%256 == 0 && progress != nil {
			progress(i, len(keys))
		}
		rec, ok := a.man.Get(key)
		if !ok {
			out.Skipped++
			continue
		}
		m, ok := a.resolve(root, key, rec, meta)
		if !ok {
			out.Skipped++
			out.Vanished = append(out.Vanished, key)
			continue
		}
		out.Messages = append(out.Messages, m)
	}
	return out, nil
}

// archiveKinds are the sibling files of a record, from the page's path.
func archiveSiblings(rel string) (page, eml, zip string) {
	stem := strings.TrimSuffix(rel, verifyHTMLSuffix)
	return rel, stem + verifyEMLSuffix, stem + verifyZipSuffix
}

// exists reports whether rel is in the archive as far as one Lstat through the root can
// tell; an error other than "not there" says nothing, and counts as there (the run lists
// the file when it cannot read it).
func (a *ArchiveSource) exists(root *os.Root, rel string) (present bool, size int64) {
	fi, err := root.Lstat(filepath.FromSlash(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return false, 0
	}
	if err != nil {
		return true, -1
	}
	return true, fi.Size()
}

// resolve is resolveRecord and the gate's reading of the files the run would copy for the message: the
// preview says how many the gate will refuse before Start, not only after (friction F2).
func (a *ArchiveSource) resolve(root *os.Root, key string, rec state.Record, meta map[string]index.ListRow) (CopyMessage, bool) {
	m, ok := a.resolveRecord(root, key, rec, meta)
	if !ok {
		return m, false
	}
	if _, valid := validRelPath(rec.Path); !valid || !strings.HasSuffix(rec.Path, verifyHTMLSuffix) {
		m.Blocked = "path"
		return m, true
	}
	page, eml, zip := archiveSiblings(rec.Path)
	files := []string{eml} // what Files opens for a message with an original
	if m.NoOrig != "" {
		files = []string{page}
		if m.Zip {
			files = append(files, zip)
		}
	}
	m.Blocked = a.blockedBy(root, files)
	return m, true
}

// blockedBy opens each file behind the gate and returns the reason the gate refuses the first one it
// refuses for good ("link", "hardlink", "irregular", "path"), or "": a file that is only unreadable or
// missing is not the gate's refusal, and the run lists it by its own reason.
func (a *ArchiveSource) blockedBy(root *os.Root, rels []string) string {
	for _, rel := range rels {
		f, _, err := a.openGatedIn(root, rel)
		if err == nil {
			f.Close()
			continue
		}
		var af *ArchiveFileError
		if errors.As(err, &af) {
			switch af.Kind {
			case AFSymlink:
				return "link"
			case AFLinks:
				return "hardlink"
			case AFIrregular:
				return "irregular"
			case AFPath:
				return "path"
			}
		}
	}
	return ""
}

func (a *ArchiveSource) resolveRecord(root *os.Root, key string, rec state.Record, meta map[string]index.ListRow) (CopyMessage, bool) {
	tok, _, _ := strings.Cut(key, "\x00")
	m := CopyMessage{SourceID: key, SizeHint: -1, Rel: rec.Path, Gone: !(rec.Present || rec.LastSeen.IsZero())}
	folder, err := archiveCanonFolder(rec.Folder)
	if err != nil { // build refused such a manifest: a record the tree does not hold is the store's own
		folder = ""
	}
	m.Folder = a.folderRefs(tok, folder)
	m.Mailbox = a.mailbox[tok]
	if row, ok := meta[key]; ok {
		m.Subject, m.From, m.FromName, m.Received = row.Subject, row.SenderEmail, row.SenderName, row.Date
	} else {
		m.Subject = rec.Subject
	}
	if m.Received.IsZero() { // no row, or a row with no valid date (E4-AO4): the manifest record's own
		if t, err := time.Parse(time.RFC3339, rec.Date); err == nil && t.Year() > 0 && t.Year() <= 9999 {
			m.Received = t.UTC()
		}
	}
	// The subject is cut before it is cleaned: a record with no index row takes the manifest's own, which
	// is bounded in bytes only, and the plan reserves twelve bytes for each byte of it (E4-AO6).
	m.Subject = plainText(archiveCut(m.Subject, archiveMaxSubjectBytes))
	if rec.Fixity != nil {
		fx := *rec.Fixity
		m.Fix = &fx
	}
	m.Notes = archiveGapNotes(rec)

	// The belt: only a path the gate would walk is looked at, an unsafe one is listed by the run.
	if _, ok := validRelPath(rec.Path); !ok || !strings.HasSuffix(rec.Path, verifyHTMLSuffix) {
		return m, true
	}
	page, eml, zip := archiveSiblings(rec.Path)
	pagePresent, pageSize := a.exists(root, page)
	if !pagePresent {
		return m, false
	}
	// What a message weighs is what its files weigh on disk, which the run copies at most: the
	// size the manifest records is a claim, held to the file when it is copied (streamPart), and
	// a forged one (4 EiB, or a negative number) must not size the plan (E4-AO3).
	var size int64
	known := true
	add := func(dig *state.FileDigest, onDisk int64) {
		var n int64
		switch {
		case onDisk >= 0:
			n = onDisk
		case dig != nil && dig.Size >= 0:
			n = dig.Size
		default:
			known = false
			return
		}
		if size > math.MaxInt64-n {
			size = math.MaxInt64
		} else {
			size += n
		}
	}
	emlPresent, emlSize := a.exists(root, eml)
	hasFix := func(f func(*state.Fixity) *state.FileDigest) *state.FileDigest {
		if rec.Fixity == nil {
			return nil
		}
		return f(rec.Fixity)
	}
	emlDig := hasFix(func(f *state.Fixity) *state.FileDigest { return f.EML })
	zipDig := hasFix(func(f *state.Fixity) *state.FileDigest { return f.Zip })
	htmlDig := hasFix(func(f *state.Fixity) *state.FileDigest { return f.HTML })
	if !emlPresent && emlDig != nil {
		return m, false // the original the archive recorded is gone: removed since (§4.6 item 5)
	}
	if emlPresent {
		add(emlDig, emlSize)
		m.SizeHint = size
		if !known {
			m.SizeHint = -1
		}
		return m, true
	}
	m.NoOrig = noOrigNotKept
	if a.pst[tok] {
		m.NoOrig = noOrigPST
	}
	zipPresent, zipSize := a.exists(root, zip)
	if !zipPresent && zipDig != nil {
		return m, false
	}
	m.Zip = zipPresent
	add(htmlDig, pageSize)
	if zipPresent {
		add(zipDig, zipSize)
	}
	if known {
		m.SizeHint = size
	}
	if page, err := a.pageBytes(root, rec.Path); err == nil {
		m.Quarantine = !export.ArchivedPageInert(page)
	} else if errors.Is(err, errPageTooLarge) {
		m.Quarantine = true
	}
	return m, true
}

// The text of a manifest record that reaches the plan is bounded where the plan takes it: a manifest
// is bounded in bytes only, and the plan holds, reserves for and writes what it is given (E4-AO6). The
// subject has the bound the list puts on an index row's; a gap label or an inline-image id is a file name
// or a content id, which 512 bytes hold.
const (
	archiveMaxSubjectBytes = index.MaxListSubjectBytes
	archiveMaxLabelBytes   = 512
)

// archiveCut is s cut to at most max bytes, at a character.
func archiveCut(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// archiveGapNotes are the archive's own gaps for a record, in words (§4.6 item 3):
// state.UnknownSentinel, which state.Load puts on every issue-free v1 record, is replaced
// by what it means.
func archiveGapNotes(rec state.Record) []string {
	var notes []string
	seen := map[string]bool{}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			notes = append(notes, s)
		}
	}
	for _, label := range append(append([]string(nil), rec.Missing...), rec.Terminal...) {
		label = archiveCut(label, archiveMaxLabelBytes)
		// A parse gap (design-mime-fidelity §4.9) is read before its separators are stripped:
		// it is a part kept as received, a multipart shown as text or a message kept as an
		// earlier version read it, not an attachment that was never downloaded (MS2-AO8).
		if kind, detail, ok := state.ParseGapParts(label); ok {
			// The detail of a fallback or an identity stub is the walker's own account of what it
			// compared with an earlier parser: the row's why says it for a person (friction F15).
			why := state.RecordRowWhyFor(kind, detail)
			if kind == state.GapKindFidelityFallback || kind == state.GapKindIdentityStub {
				add("the archive records a gap in this message: " + why)
			} else {
				add("the archive records a gap in this message: " + why + " (" + plainText(detail) + ")")
			}
			continue
		}
		label = plainText(label)
		switch {
		case label == state.UnknownSentinel:
			add("completeness was not tracked by the version that captured this message")
		case label == state.MissingBody:
			add("the archive records this message's body as never downloaded")
		case label == state.LabelUnparsed:
			add("the archive records this message as one it could not parse")
		case state.IsUnreadableLabel(label):
			add("the archive records part of this message as unreadable (" + label + ")")
		default:
			add("the archive records attachment '" + label + "' as never downloaded")
		}
	}
	for _, cid := range rec.Unresolved {
		add("the archive records an inline image (cid:" + plainText(archiveCut(cid, archiveMaxLabelBytes)) + ") it could not find in the message")
	}
	return notes
}

// errPageTooLarge says a page is above what the gate reads.
var errPageTooLarge = errors.New("the page is above what the archived-page gate reads")

// pageBytes reads an archived page for the gate: through the root, behind the file gate,
// and not above export.ArchivedPageMaxBytes.
func (a *ArchiveSource) pageBytes(root *os.Root, rel string) ([]byte, error) {
	f, fi, err := a.openGatedIn(root, rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi.Size() > export.ArchivedPageMaxBytes {
		return nil, errPageTooLarge
	}
	return io.ReadAll(io.LimitReader(f, fi.Size()+1))
}

// ---- the file gate ------------------------------------------------------------------

// ArchiveFileKind says why a file of the archive was not read.
type ArchiveFileKind int

const (
	AFMissing   ArchiveFileKind = iota // not there (removed, or the archive is gone)
	AFRead                             // there, and could not be read
	AFSymlink                          // a link stands where the file should be, at some level
	AFIrregular                        // not a regular file
	AFLinks                            // a regular file with more than one hard link
	AFPath                             // the record's path is not one that stays inside the archive
	AFChanged                          // the bytes do not match what the plan recorded
	AFTooLarge                         // above the limit applied
)

// ArchiveFileError is a per-message failure of the archive gate.
type ArchiveFileError struct {
	Kind  ArchiveFileKind
	Rel   string
	Err   error
	Size  int64 // AFTooLarge: the file's declared size
	Limit int64 // AFTooLarge: the limit applied
}

func (e *ArchiveFileError) Error() string {
	what := map[ArchiveFileKind]string{AFMissing: "is not in the archive", AFRead: "could not be read", AFSymlink: "has a link in its path",
		AFIrregular: "is not a regular file", AFLinks: "has more than one hard link", AFPath: "is not a safe path inside the archive",
		AFChanged: "does not match what the archive recorded", AFTooLarge: "is larger than this version exports"}[e.Kind]
	if e.Err != nil {
		return e.Rel + " " + what + ": " + e.Err.Error()
	}
	return e.Rel + " " + what
}

func (e *ArchiveFileError) Unwrap() error { return e.Err }

func archiveReadErr(rel string, err error) *ArchiveFileError {
	if errors.Is(err, fs.ErrNotExist) {
		return &ArchiveFileError{Kind: AFMissing, Rel: rel, Err: err}
	}
	return &ArchiveFileError{Kind: AFRead, Rel: rel, Err: err}
}

// openGated opens the archive's file rel behind the §4.6 gate: a validated relative path,
// a component-wise Lstat through the root with no symlink or reparse point at any level, a
// regular file, an open that cannot block on a pipe, the opened handle the same file the
// walk saw, and exactly one hard link (XC19). Within the root the window between the check
// and the use remains: a co-writer can swap a file between Lstat and open, which the
// fixity check then catches when a digest is recorded.
func (a *ArchiveSource) openGated(rel string) (*os.File, os.FileInfo, error) {
	root, done, err := a.rootFor()
	if err != nil { // the folder could not be opened: a read error, which asks Gone whether it is the archive
		return nil, nil, archiveReadErr(rel, errors.New("the archive's folder could not be opened"))
	}
	defer done()
	return a.openGatedIn(root, rel)
}

// openGatedIn is openGated through a root the caller holds.
func (a *ArchiveSource) openGatedIn(root *os.Root, rel string) (*os.File, os.FileInfo, error) {
	parts, ok := validRelPath(rel)
	if !ok {
		return nil, nil, &ArchiveFileError{Kind: AFPath, Rel: rel}
	}
	var last os.FileInfo
	for i := range parts {
		fi, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if err != nil {
			return nil, nil, archiveReadErr(rel, err)
		}
		if fi.Mode()&fs.ModeSymlink != 0 || isReparsePoint(fi) {
			return nil, nil, &ArchiveFileError{Kind: AFSymlink, Rel: rel}
		}
		if i < len(parts)-1 {
			if !fi.IsDir() {
				return nil, nil, &ArchiveFileError{Kind: AFMissing, Rel: rel}
			}
			continue
		}
		if !fi.Mode().IsRegular() {
			return nil, nil, &ArchiveFileError{Kind: AFIrregular, Rel: rel}
		}
		var pinned bool
		if last, pinned = util.PinIdentity(fi); !pinned { // compared once it is opened (P3-8, B1)
			// Its identity would be read at the comparison, from whatever is there then
			// (WINPATH-1).
			return nil, nil, &ArchiveFileError{Kind: AFChanged, Rel: rel, Err: errors.New("it was replaced while it was being opened")}
		}
	}
	f, err := openRegularIn(root, filepath.FromSlash(rel))
	if err != nil {
		if errors.Is(err, errNotRegular) {
			return nil, nil, &ArchiveFileError{Kind: AFIrregular, Rel: rel}
		}
		return nil, nil, archiveReadErr(rel, err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, archiveReadErr(rel, err)
	}
	if !os.SameFile(last, fi) {
		f.Close()
		return nil, nil, &ArchiveFileError{Kind: AFChanged, Rel: rel, Err: errors.New("it was replaced while it was being opened")}
	}
	if n, err := fileLinks(f); err != nil {
		f.Close()
		return nil, nil, archiveReadErr(rel, err)
	} else if n > 1 {
		f.Close()
		return nil, nil, &ArchiveFileError{Kind: AFLinks, Rel: rel}
	}
	return f, fi, nil
}

// ---- the run's side ------------------------------------------------------------------

// Fetch is not how an archive message is copied: it has files, not one stream (Files).
func (a *ArchiveSource) Fetch(ctx context.Context, m *CopyMessage, w io.Writer, maxBytes int64) (int64, error) {
	return 0, errors.New("an archive message is copied as files")
}

// ArchiveGoneError is the archive itself, not one file, being gone.
type ArchiveGoneError struct{ Why string }

func (e *ArchiveGoneError) Error() string {
	return "the archive is no longer available (" + e.Why + ")"
}

// Probe is the breaker's control request: the archive's folder and manifest still answer.
func (a *ArchiveSource) Probe(ctx context.Context) error {
	if why, gone := a.Gone(); gone {
		return &ArchiveGoneError{Why: why}
	}
	return nil
}

// RefreshAuth: nothing signs in to an archive.
func (a *ArchiveSource) RefreshAuth(ctx context.Context) error { return nil }

// SignedIn is true for the source's own mailbox, which an archive export has none of: the
// run's sign-in poll compares it with the plan's, and both are empty.
func (a *ArchiveSource) SignedIn() (string, bool) { return "", true }

// Gone reports whether the archive's root or manifest is gone (§4.6 item 7): looked at by
// path, because a root handle on a deleted folder still answers.
func (a *ArchiveSource) Gone() (string, bool) {
	if fi, err := os.Stat(a.dir); err != nil || !fi.IsDir() {
		return archiveWhy(err), true
	}
	if _, err := os.Lstat(filepath.Join(a.dir, archiveManifestName)); err != nil {
		return archiveWhy(err), true
	}
	return "", false
}

func archiveWhy(err error) string {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return "it was disconnected or moved"
	}
	return readName(err)
}

// Changed reports whether the archive's record for m no longer says what the plan recorded:
// it was removed, or a capture rewrote it (its recorded digests differ). The manifest is
// read again only when it has changed since the last time it was read.
func (a *ArchiveSource) Changed(m *CopyMessage) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	root, done, rerr0 := a.rootFor()
	if rerr0 != nil {
		return true
	}
	fi, err := root.Lstat(archiveManifestName)
	done()
	if err != nil {
		return true
	}
	stamp := archiveStamp{size: fi.Size(), mod: fi.ModTime()}
	if a.live == nil || a.liveSeen != stamp {
		data, st, rerr := a.readManifest()
		if rerr != nil {
			return true
		}
		man, lerr := state.LoadFrom(filepath.Join(a.dir, archiveManifestName), data)
		if lerr != nil {
			return true
		}
		a.live, a.liveSeen = man, st
	}
	rec, ok := a.live.Get(m.SourceID)
	if !ok {
		return true
	}
	return !sameFixity(rec.Fixity, m.Fix) || rec.Path != m.Rel
}

// Capturing reports whether a capture holds the archive's lock now. It is asked only after a file failed
// its check, never to wait for one: the export takes no lock and never backs off (MA-510). The probe
// opens an existing lock file and takes the lock for an instant (lockfile.Held), creating and writing
// nothing; it is not made for another archive folder, which this export opens read-only and which
// no capture of this computer writes.
func (a *ArchiveSource) Capturing() bool {
	return !a.other && lockfile.Held(filepath.Join(a.dir, lockfile.Name))
}

func sameFixity(a, b *state.Fixity) bool {
	if a == nil || b == nil {
		return a == b
	}
	same := func(x, y *state.FileDigest) bool {
		if x == nil || y == nil {
			return x == y
		}
		return *x == *y
	}
	return same(a.EML, b.EML) && same(a.HTML, b.HTML) && same(a.Zip, b.Zip)
}

// Files opens the files m yields, behind the gate: its original, or, when the archive kept
// none, its archived page (kept as .html only when it passes the archived-page gate,
// otherwise copied as text) and its attachments zip. Nothing is read from the original
// until the part is copied, and each part is held to the fixity the plan recorded.
func (a *ArchiveSource) Files(ctx context.Context, m *CopyMessage) (*CopyFiles, error) {
	if m.Removed { // a record the manifest no longer lists is not exported, whatever of its files is left (E4-AC10)
		return nil, &ArchiveFileError{Kind: AFMissing, Rel: m.Rel}
	}
	if _, ok := validRelPath(m.Rel); !ok || !strings.HasSuffix(m.Rel, verifyHTMLSuffix) {
		return nil, &ArchiveFileError{Kind: AFPath, Rel: m.Rel}
	}
	page, eml, zip := archiveSiblings(m.Rel)
	files := &CopyFiles{Status: statusExported, Notes: append([]string(nil), m.Notes...)}
	fix := func(pick func(*state.Fixity) *state.FileDigest) *state.FileDigest {
		if m.Fix == nil {
			return nil
		}
		return pick(m.Fix)
	}
	var open []*os.File
	files.release = func() {
		for _, f := range open {
			f.Close()
		}
	}
	fail := func(err error) (*CopyFiles, error) {
		files.release()
		return nil, err
	}
	if m.NoOrig == "" {
		f, fi, err := a.openGated(eml)
		if err != nil {
			return fail(err)
		}
		open = append(open, f)
		dig := fix(func(x *state.Fixity) *state.FileDigest { return x.EML })
		if dig == nil {
			files.Notes = append(files.Notes, "copied as stored: the archive recorded no checksum for this original, so it can't be attested ("+checkIntegrityButton+" in MailArchive Desktop, or the command-line verify, records one)")
		}
		files.Parts = append(files.Parts, a.streamPart(f, fi, eml, dig, roleOriginal, verifyEMLSuffix))
		return files, nil
	}
	files.Status = statusWithoutOrig
	files.Notes = append(files.Notes, noOriginalNote(m.NoOrig))
	dig := fix(func(x *state.Fixity) *state.FileDigest { return x.HTML })
	f, fi, err := a.openGated(page)
	if err != nil {
		return fail(err)
	}
	if fi.Size() > export.ArchivedPageMaxBytes {
		// Too large to check: it is not trusted, and is copied as text.
		open = append(open, f)
		files.Notes = append(files.Notes, quarantineNote+" (it is too large for MailArchive to check)")
		files.Parts = append(files.Parts, a.streamPart(f, fi, page, dig, roleQuarantinedPage, archivedPageTxtSuffix))
	} else {
		data, err := io.ReadAll(io.LimitReader(f, fi.Size()+1))
		f.Close()
		if err != nil {
			return fail(archiveReadErr(page, err))
		}
		if int64(len(data)) != fi.Size() || (dig != nil && !digestMatches(dig, data)) {
			return fail(&ArchiveFileError{Kind: AFChanged, Rel: page})
		}
		role, suffix := roleArchivedPage, verifyHTMLSuffix
		if !export.ArchivedPageInert(data) {
			role, suffix = roleQuarantinedPage, archivedPageTxtSuffix
			files.Notes = append(files.Notes, quarantineNote)
		}
		files.Parts = append(files.Parts, bytesPart(data, role, suffix))
	}
	if m.Zip {
		zf, zfi, err := a.openGated(zip)
		if err != nil {
			return fail(err)
		}
		open = append(open, zf)
		files.Parts = append(files.Parts, a.streamPart(zf, zfi, zip, fix(func(x *state.Fixity) *state.FileDigest { return x.Zip }), roleArchivedZip, verifyZipSuffix))
	}
	return files, nil
}

// archivedPageTxtSuffix names an archived page that fails the gate.
const archivedPageTxtSuffix = ".archived-page.txt"

// quarantineNote is what an entry says of a page copied as text (design §4.11, P10).
const quarantineNote = "this archived page doesn't begin with the security policy a page of MailArchive carries, or it holds a live refresh, so it is copied as text (.archived-page.txt) and not as a page: open it as text only"

func noOriginalNote(why string) string {
	if why == noOrigPST {
		return "Outlook .pst items never have an original .eml: the archived page and its attachments are copied instead"
	}
	return "the archive kept no original: it was captured without 'Keep each message's original .eml', so the archived page and its attachments are copied instead"
}

func digestMatches(d *state.FileDigest, data []byte) bool {
	sum := sha256.Sum256(data)
	return int64(len(data)) == d.Size && hex.EncodeToString(sum[:]) == d.SHA256
}

// bytesPart is a part whose bytes are already in memory, read and checked.
func bytesPart(data []byte, role, suffix string) CopyFilePart {
	return CopyFilePart{Role: role, Suffix: suffix, Size: int64(len(data)), Copy: func(w io.Writer, maxBytes int64) (int64, error) {
		if maxBytes > 0 && int64(len(data)) > maxBytes {
			return 0, &ArchiveFileError{Kind: AFTooLarge, Size: int64(len(data)), Limit: maxBytes}
		}
		n, err := io.Copy(w, bytes.NewReader(data))
		return n, err
	}}
}

// readTagged marks an error of reading the archive's file, so that a failure of the
// destination writer, which io.Copy returns the same way, is never taken for one.
type readTagged struct{ r io.Reader }

type archiveReadFailure struct{ err error }

func (e *archiveReadFailure) Error() string { return e.err.Error() }
func (e *archiveReadFailure) Unwrap() error { return e.err }

func (t readTagged) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && err != io.EOF {
		return n, &archiveReadFailure{err}
	}
	return n, err
}

// streamPart is a part copied by streaming the open file, bounded and checked against the
// recorded digest: one byte past the recorded size is read, so a file longer than
// recorded (a correct prefix with appended bytes) fails on its length (PC13).
func (a *ArchiveSource) streamPart(f *os.File, fi os.FileInfo, rel string, dig *state.FileDigest, role, suffix string) CopyFilePart {
	declared := fi.Size()
	if dig != nil {
		declared = dig.Size
	}
	// The recorded size is a claim: a file that weighs another size is not what the archive
	// recorded, whatever the claim, and is said so before the cap or a byte is looked at (E4-AO3).
	mismatch := dig != nil && fi.Size() != dig.Size
	return CopyFilePart{Role: role, Suffix: suffix, Size: declared, Copy: func(w io.Writer, maxBytes int64) (int64, error) {
		if mismatch {
			return 0, &ArchiveFileError{Kind: AFChanged, Rel: rel}
		}
		if maxBytes > 0 && declared > maxBytes {
			return 0, &ArchiveFileError{Kind: AFTooLarge, Rel: rel, Size: declared, Limit: maxBytes}
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(readTagged{f}, declared+1))
		if err != nil {
			var rf *archiveReadFailure
			if errors.As(err, &rf) {
				return n, archiveReadErr(rel, rf.err)
			}
			return n, err // the destination's own failure
		}
		if n != declared || (dig != nil && hex.EncodeToString(h.Sum(nil)) != dig.SHA256) {
			return n, &ArchiveFileError{Kind: AFChanged, Rel: rel}
		}
		return n, nil
	}}
}
