package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"mail-archive-tool/internal/state"
)

// The read side of Restore to Thunderbird (docs/design-restore-thunderbird.md §2, §3.4,
// §6.1, §6.4, §6.5; slice R1). It writes nothing anywhere. Three parts:
//
//   - FaithfulOriginal: a record's preserved original exactly as the archive kept it, or
//     the reason it cannot be given. extract emits through it, so extract and restore
//     cannot disagree about which bytes are faithful (extract's output is unchanged:
//     MA-181..MA-185).
//   - NewArchiveTree: the Extraction workspace's folder tree, built from a manifest
//     however it was loaded (plain, or an encrypted archive's, opened with its key).
//   - SelectForRestore: what a restore of some of that tree would create and hold,
//     counted from the manifest and one Lstat per record (no message is read), under
//     the restore's own caps.

// OriginalSkipKind is why a record's faithful bytes could not be given.
type OriginalSkipKind int

const (
	// SkipGate: the recorded path, or the file at it, failed the gate (not a safe
	// in-archive path, a link, not a regular file, over the size cap, unreadable).
	SkipGate OriginalSkipKind = iota + 1
	// SkipNoOriginal: the record kept no preserved original.
	SkipNoOriginal
	// SkipFixity: the original does not match its recorded fixity, or an encrypted
	// archive's sealed original could not be read (missing or damaged).
	SkipFixity
)

// OriginalSkip is a record whose original was not given: the path it would have been
// read from and the reason, in extract's words.
type OriginalSkip struct {
	Path   string
	Reason string
	Kind   OriginalSkipKind
}

// Original is a record's preserved original, byte for byte as the archive kept it.
type Original struct {
	Data []byte
	// NoFixity: the record has no recorded fixity for its original, so the bytes are
	// "as stored", not checked.
	NoFixity bool
}

// recordHTMLSegs is the gate on a record's recorded path that extract, restore, status
// and verify share: a safe in-archive path of at least two parts ending in the page's
// suffix. It returns the path's parts.
func recordHTMLSegs(path string) ([]string, bool) {
	segs, ok := validRelPath(path)
	if !ok || len(segs) < 2 || !strings.HasSuffix(path, verifyHTMLSuffix) {
		return nil, false
	}
	return segs, true
}

// FaithfulOriginal gives rec's preserved original, or why it cannot. For a plain archive
// (sealed nil) it reads the record's `.eml` under out through the gate verify uses (no
// link at any level, a regular file, read bounded to the recorded size plus one byte, so
// a longer file fails on its length) and checks it against the recorded fixity when
// there is one. For an encrypted archive it reads the sealed original through sealed and
// checks it the same way. Nothing is ever made up. err is a hard stop only: the
// archive's key locked mid-run (ErrEncryptedNeedsKey); every other failure is a skip.
func FaithfulOriginal(out string, rec state.Record, sealed SealedOriginals) (Original, *OriginalSkip, error) {
	if _, ok := recordHTMLSegs(rec.Path); !ok {
		return Original{}, &OriginalSkip{rawPath(rec.Path), "recorded path is not a safe in-archive .html path", SkipGate}, nil
	}
	emlRel := strings.TrimSuffix(rec.Path, verifyHTMLSuffix) + verifyEMLSuffix
	if sealed != nil {
		return sealedOriginal(rec, emlRel, sealed)
	}
	segs, ok := validRelPath(emlRel)
	if !ok {
		return Original{}, &OriginalSkip{rawPath(emlRel), "the .eml path is not a safe in-archive path", SkipGate}, nil
	}
	kind, size, full := extractInspect(out, segs)
	switch kind {
	case "missing":
		return Original{}, &OriginalSkip{emlRel, "no preserved original (.eml) — this record has no bytes to extract", SkipNoOriginal}, nil
	case "symlink":
		return Original{}, &OriginalSkip{emlRel, "a symlink stands where the .eml should be (not followed)", SkipGate}, nil
	case "irregular":
		return Original{}, &OriginalSkip{emlRel, "the .eml is not a regular file", SkipGate}, nil
	}
	recorded := recordedEML(rec)
	// The expected byte count (the recorded size when there is fixity, else the size on
	// disk), capped so an oversized blob is skipped, not slurped (PC8).
	expected := size
	if recorded != nil {
		expected = recorded.Size
	}
	if expected > maxExtractEMLBytes {
		return Original{}, &OriginalSkip{emlRel, fmt.Sprintf("the .eml is %d bytes, larger than the %d-byte extract cap", expected, maxExtractEMLBytes), SkipGate}, nil
	}
	bound := expected
	if recorded != nil {
		bound = recorded.Size + 1
	}
	data, sum, err := readBounded(full, bound)
	if err != nil {
		return Original{}, &OriginalSkip{emlRel, "the .eml could not be read", SkipGate}, nil
	}
	if recorded == nil {
		return Original{Data: data, NoFixity: true}, nil, nil
	}
	if int64(len(data)) != recorded.Size || sum != recorded.SHA256 {
		return Original{}, &OriginalSkip{emlRel, "the .eml does not match its recorded fixity (modified or truncated) — run `mailarchive verify` first", SkipFixity}, nil
	}
	return Original{Data: data}, nil, nil
}

// sealedOriginal is FaithfulOriginal for an encrypted archive: the sealed original,
// decrypted (its ciphertext checked against the record's digest by sealed), then checked
// against the recorded fixity when there is one (PC13).
func sealedOriginal(rec state.Record, emlRel string, sealed SealedOriginals) (Original, *OriginalSkip, error) {
	if rec.Sealed != nil && rec.Sealed.EML != nil && rec.Sealed.EML.Size > maxExtractEMLBytes {
		return Original{}, &OriginalSkip{emlRel, fmt.Sprintf("the original is %d bytes sealed, larger than the %d-byte extract cap", rec.Sealed.EML.Size, maxExtractEMLBytes), SkipGate}, nil
	}
	data, err := sealed.Original(rec)
	switch {
	case errors.Is(err, ErrNoOriginal):
		return Original{}, &OriginalSkip{emlRel, "no preserved original — this record has no bytes to extract", SkipNoOriginal}, nil
	case errors.Is(err, ErrEncryptedNeedsKey):
		return Original{}, nil, err
	case err != nil:
		return Original{}, &OriginalSkip{emlRel, "the original could not be read from the archive (missing or damaged) — run `mailarchive verify`", SkipFixity}, nil
	}
	recorded := recordedEML(rec)
	if recorded == nil {
		return Original{Data: data, NoFixity: true}, nil, nil
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != recorded.Size || hex.EncodeToString(sum[:]) != recorded.SHA256 {
		return Original{}, &OriginalSkip{emlRel, "the original does not match its recorded fixity — run `mailarchive verify` first", SkipFixity}, nil
	}
	return Original{Data: data}, nil, nil
}

// recordedEML is the record's recorded fixity for its original, or nil.
func recordedEML(rec state.Record) *state.FileDigest {
	if rec.Fixity == nil {
		return nil
	}
	return rec.Fixity.EML
}

// ArchiveTree is the archive's folder tree (the Extraction workspace's own rules: store →
// folder path from each record's Folder, empty parts dropped), built from a manifest
// however it was loaded — so an encrypted archive's manifest, opened with its key, gives
// the same tree a plain one does. Node ids are validated against the tree, never used
// to build a path.
type ArchiveTree struct {
	a   *ArchiveSource
	man *state.Manifest
}

// NewArchiveTree builds the tree of man. It applies the tree builder's rules for the
// owner's own archive (a name over 1,024 bytes or a folder over 300 levels refused;
// no node count), so a restore applies its own caps on top (SelectForRestore).
func NewArchiveTree(man *state.Manifest) (*ArchiveTree, error) {
	if man == nil {
		return nil, errors.New("app: NewArchiveTree needs a loaded manifest")
	}
	a := &ArchiveSource{man: man}
	if err := a.build(); err != nil {
		return nil, err
	}
	return &ArchiveTree{a: a, man: man}, nil
}

// Stores are the archive's stores, by token.
func (t *ArchiveTree) Stores() []ArchiveStore { return t.a.Stores() }

// Tree is the folder tree in tree order: each store, then its folders, depth first.
func (t *ArchiveTree) Tree() []ArchiveNode { return t.a.Tree() }

// Locate resolves a node id to its store and folder path.
func (t *ArchiveTree) Locate(id string) (token, folder string, ok bool) { return t.a.Locate(id) }

// Keys are the manifest keys of the records filed directly in node id.
func (t *ArchiveTree) Keys(id string) []string { return t.a.Keys(id) }

// The restore's own caps (design §6.5, RT-7; confirmed by the lead, foldcheck L1): at
// most this many folders, and this many levels, in one restore, whatever the tree
// builder allows.
const (
	RestoreMaxFolders = 10000
	RestoreMaxDepth   = 300
)

// RestoreNoFolder names the folder records with no folder are restored into (§3.4).
const RestoreNoFolder = "(no folder)"

// RestoreTooMany is W-TOOMANY: the folders chosen would make a restore over one of its
// caps. Nothing has been written; choosing fewer folders is the remedy.
type RestoreTooMany struct {
	Folders int // more than RestoreMaxFolders folders, when set
	Depth   int // a folder more than RestoreMaxDepth levels down, when set
}

func (e *RestoreTooMany) Error() string {
	if e.Depth > 0 {
		return fmt.Sprintf("the folders chosen are nested more than %d levels deep (at least %d), more than one restore makes — nothing was written; choose fewer folders, or folders nearer the top", RestoreMaxDepth, e.Depth)
	}
	return fmt.Sprintf("the folders chosen would make more than %d folders in Thunderbird (at least %d) — nothing was written; choose fewer folders", RestoreMaxFolders, e.Folders)
}

// RestoreFolder is one folder a restore creates under its new top folder.
type RestoreFolder struct {
	// Names is the folder's path below the restore's top folder, as the archive spells
	// it (made file-safe only when written, slice R4). With records from more than one
	// store, the first name is the store's label.
	Names []string
	// Keys are the restorable records filed directly in it, in a stable order.
	Keys []string
	// Bytes is their originals' sizes (the recorded size, else the size on disk; the
	// sealed size for an encrypted archive).
	Bytes int64
}

// RestoreSelection is the read side of a restore's plan: what it would create, and the
// counts the plan shows, before any message is read.
type RestoreSelection struct {
	// Folders are the folders to create, each once, parents before children: every folder
	// holding a restorable record, and its parents.
	Folders []RestoreFolder

	Records    int   // records chosen
	Restorable int   // records with an original to restore
	Bytes      int64 // the restorable originals' sizes
	NoFixity   int   // restorable records with no recorded fixity ("as stored")
	PST        int   // records of an Outlook store, which never keeps an original (W-PST)
	NoOriginal int   // other records whose original is not there (missing, or not a usable file)
	TooBig     int   // records whose original is over the 2 GiB cap

	// Deleted counts restorable records the mailbox no longer held at a later backup (the
	// workspace's rule: !(Present || LastSeen.IsZero())). It is shown only when
	// KeepsTimeline is true: some record of the archive has a non-zero LastSeen.
	Deleted       int
	KeepsTimeline bool
}

// SelectForRestore says what restoring nodeIDs of tree (none: the whole archive) would
// create and hold. out is the archive folder (a plain archive's originals are Lstat-ed
// under it); sealed says the manifest is an encrypted archive's, whose originals are
// judged from the record. A node id not in the tree is refused; choosing a node takes
// its subfolders too. Over a cap it returns *RestoreTooMany.
func SelectForRestore(out string, tree *ArchiveTree, nodeIDs []string, sealed bool) (*RestoreSelection, error) {
	for _, id := range nodeIDs {
		if _, _, ok := tree.Locate(id); !ok {
			return nil, fmt.Errorf("the folder chosen (%s) is not in this archive — load its folders again", printable(id))
		}
	}
	chosen := func(id string) bool {
		if len(nodeIDs) == 0 {
			return true
		}
		for _, s := range nodeIDs {
			if id == s || strings.HasPrefix(id, s+"/") {
				return true
			}
		}
		return false
	}

	sel := &RestoreSelection{}
	for _, rec := range tree.man.All() {
		if !rec.LastSeen.IsZero() {
			sel.KeepsTimeline = true
			break
		}
	}

	type held struct {
		tok   string
		names []string // the folder's own parts, below its store
		keys  []string
		bytes int64
	}
	var folders []*held
	stores := map[string]bool{}
	ids := make([]string, 0, len(tree.a.byNode))
	for id := range tree.a.byNode {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	seen := map[string]bool{}
	for _, id := range ids {
		if !chosen(id) {
			continue
		}
		tok, folder, ok := tree.Locate(id)
		if !ok {
			continue
		}
		h := &held{tok: tok}
		if folder == "" {
			h.names = []string{RestoreNoFolder}
		} else {
			h.names = strings.Split(folder, "/")
		}
		for _, key := range tree.Keys(id) {
			if seen[key] {
				continue
			}
			seen[key] = true
			rec, ok := tree.man.Get(key)
			if !ok {
				continue
			}
			sel.Records++
			size, verdict := restorable(out, rec, tree.a.pst[tok], sealed)
			switch verdict {
			case restorePST:
				sel.PST++
				continue
			case restoreNoOriginal:
				sel.NoOriginal++
				continue
			case restoreTooBig:
				sel.TooBig++
				continue
			}
			sel.Restorable++
			sel.Bytes += size
			if recordedEML(rec) == nil {
				sel.NoFixity++
			}
			if !(rec.Present || rec.LastSeen.IsZero()) {
				sel.Deleted++
			}
			h.keys = append(h.keys, key)
			h.bytes += size
		}
		if len(h.keys) > 0 {
			stores[tok] = true
			folders = append(folders, h)
		}
	}

	// Each store gets a level of its own only when the restorable records come from more
	// than one, so two stores' "Inbox" never merge (§3.4).
	byStore := len(stores) > 1
	labels := map[string]string{}
	for _, s := range tree.Stores() {
		labels[s.Token] = s.Label()
	}
	made := map[string]*RestoreFolder{}
	var order []*RestoreFolder
	ensure := func(names []string) (*RestoreFolder, error) {
		if len(names) > RestoreMaxDepth {
			return nil, &RestoreTooMany{Depth: len(names)}
		}
		var last *RestoreFolder
		for i := 1; i <= len(names); i++ {
			k := namesKey(names[:i])
			if f, ok := made[k]; ok {
				last = f
				continue
			}
			if len(order) >= RestoreMaxFolders {
				return nil, &RestoreTooMany{Folders: len(order) + 1}
			}
			f := &RestoreFolder{Names: append([]string(nil), names[:i]...)}
			made[k] = f
			order = append(order, f)
			last = f
		}
		return last, nil
	}
	for _, h := range folders {
		names := h.names
		if byStore {
			names = append([]string{labels[h.tok]}, names...)
		}
		f, err := ensure(names)
		if err != nil {
			return nil, err
		}
		f.Keys = append(f.Keys, h.keys...)
		f.Bytes += h.bytes
	}
	sort.SliceStable(order, func(i, j int) bool { return namesLess(order[i].Names, order[j].Names) })
	for _, f := range order {
		sort.Strings(f.Keys)
		sel.Folders = append(sel.Folders, *f)
	}
	return sel, nil
}

type restoreVerdict int

const (
	restoreOK restoreVerdict = iota
	restorePST
	restoreNoOriginal
	restoreTooBig
)

// restorable judges a record for the plan without reading it: an Outlook store's record
// never has an original; an encrypted archive's has one when the record names its sealed
// original; a plain archive's when its `.eml` is a regular file through the gate (one
// Lstat per part, the predicate extract, status and verify share). The size is the
// recorded one, else the size on disk (the sealed size for an encrypted archive).
func restorable(out string, rec state.Record, pst, sealed bool) (int64, restoreVerdict) {
	if pst {
		return 0, restorePST
	}
	var size int64
	if sealed {
		if rec.Sealed == nil || rec.Sealed.EML == nil || rec.Unit == "" {
			return 0, restoreNoOriginal
		}
		size = rec.Sealed.EML.Size
	} else {
		if _, ok := recordHTMLSegs(rec.Path); !ok {
			return 0, restoreNoOriginal
		}
		segs, ok := validRelPath(strings.TrimSuffix(rec.Path, verifyHTMLSuffix) + verifyEMLSuffix)
		if !ok {
			return 0, restoreNoOriginal
		}
		kind, onDisk, _ := extractInspect(out, segs)
		if kind != "ok" {
			return 0, restoreNoOriginal
		}
		size = onDisk
		if d := recordedEML(rec); d != nil {
			size = d.Size
		}
	}
	if size > maxExtractEMLBytes {
		return 0, restoreTooBig
	}
	return size, restoreOK
}

// namesKey is a folder path's map key; each part is length-prefixed, so no name, whatever
// it holds, can make two paths share a key.
func namesKey(names []string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString(strconv.Itoa(len(n)))
		b.WriteByte(':')
		b.WriteString(n)
	}
	return b.String()
}

// namesLess orders two folder paths part by part, so a folder comes right before its
// children and its siblings' subtrees stay together.
func namesLess(a, b []string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
