// Package tbcomplete tells how much of a Thunderbird IMAP account Thunderbird has downloaded
// to this computer, from what Thunderbird itself lists (docs/design-thunderbird-completeness.md).
//
// This file is slice C1a: the reader of a profile's folderCache.json. The file is untrusted
// input. It is read once, through util.ReadRegularFile with a 16 MB cap (a link is refused, a
// pipe never waited on), decoded token by token so a key that appears twice is seen, and never
// written. Its keys are compared as strings and never opened. The reader gives facts only: for one
// store, which folders the cache lists, each field read, missing or broken, and what it could not
// tell. Whether a folder is kept, and whether the account is caught up, is the verdict's (C1b).
package tbcomplete

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/util"
)

const (
	// CacheFile is the folder cache's name in the profile folder (nsMsgFolderCache.cpp).
	CacheFile = "folderCache.json"
	// CacheMaxBytes bounds it: a larger file is refused, never read whole (design §4.1 step 2,
	// TC-15). The owner's 19-folder-cache lab profiles and a 238,000-message Gmail account's file
	// are far below it.
	CacheMaxBytes = 16 << 20
	// CacheMaxDepth is the deepest a value may sit, the top-level object being 1: Thunderbird's
	// reader, jsoncpp's CharReaderBuilder, stops past it (setDefaults' stackLimit 256;
	// OurReader::readValue's check, json_reader.cpp, Mozilla's bundled copy at the tip, read
	// 2026-11-08), so Thunderbird never reads a file nested deeper, and neither does this reader
	// (C1a pass C1A-03). Thunderbird itself writes 3 levels: the object, an entry, a number.
	CacheMaxDepth = 256
)

// Folder flags, nsMsgFolderFlags.idl (the same values at ESR 153.6.0esr and Release 159.0, the
// source re-read's claim 5).
const (
	FlagVirtual      uint32 = 0x00000020 // a saved search
	FlagTrash        uint32 = 0x00000100
	FlagSentMail     uint32 = 0x00000200
	FlagInbox        uint32 = 0x00001000
	FlagImapBox      uint32 = 0x00002000 // an IMAP folder: it needs serverTotal (§4.1 step 3)
	FlagArchive      uint32 = 0x00004000 // never All Mail on its own (foldcheck M5)
	FlagAllMail      uint32 = 0x00008000 // Gmail's All Mail
	FlagImapNoselect uint32 = 0x01000000
	FlagOffline      uint32 = 0x08000000 // kept on this computer
	FlagJunk         uint32 = 0x40000000
)

// State is how a field of an entry was read. Its zero value is Missing, so a field nobody read
// is never OK.
type State int

const (
	// Missing: the entry has no such field, or (folderSize only) holds -1, Thunderbird's "size
	// not known" (kSizeUnknown, MailNewsTypes.h; Why then says so).
	Missing State = iota
	Broken        // there, but not what Thunderbird writes; Why says how
	OK            // a JSON integer in the field's range; Value holds it
	// NotCounted: totalMsgs off the account root holds -1, the value every folder starts with
	// (nsMsgDBFolder.cpp ESR 153 :236, mNumTotalMessages(-1)) until Thunderbird reads the folder's
	// summary. Thunderbird has no count either, so the entry is not Valid, but nothing was changed
	// by hand: Entry.NotCountedYet tells C1b to say "not counted yet", never invalid_entry's words
	// (C1a pass C1A-01, the lead's call).
	NotCounted
)

// Field is one numeric field of a folder-cache entry.
type Field struct {
	State State
	Value int64  // when OK
	Why   string // when Broken ("a quoted text", "not a whole number", …), NotCounted, or a -1 Missing
}

// Entry is one folder-cache entry of a store: one key and its value.
type Entry struct {
	// Key is the key as written in the file (JSON-decoded), never opened.
	Key string
	// Path is the folder's name under the store: the key's tail with ".msf" taken off and each
	// ".sbd" level shown as "/" (for example "[Gmail]/Sent Mail"), as written, uncleaned (C3
	// cleans and caps it, TC-7). "" for the account root.
	Path string
	// Root: the entry is the account's own top level (the key is the store folder itself). It is
	// not validated, and its counts may be negative (Thunderbird starts them at -1).
	Root bool
	// NotObject: the value was not a JSON object (TC-15). Every field is Missing, so its flags are
	// not known.
	NotObject bool

	Flags             Field // unsigned 32-bit
	TotalMsgs         Field // signed 32-bit: header rows in the folder's summary
	PendingMsgs       Field // signed 32-bit: headers the server reported, not fetched yet
	ServerTotal       Field // signed 32-bit: the server's count at Thunderbird's last look (IMAP)
	ExpungedBytes     Field // signed 64-bit: deleted copies' bytes still in the offline file
	FolderSize        Field // signed 64-bit: the summary's sum of message sizes (server sizes)
	LastSyncTimeInSec Field // signed 32-bit seconds; ≤ 0 or Missing means never checked (C1b)
}

// FlagsKnown reports whether the entry's flags were read. When they were not, nobody can tell
// whether the folder is kept: foldcheck M3 treats it as kept (C1b, reason invalid_entry).
func (e Entry) FlagsKnown() bool { return e.Flags.State == OK }

// Has reports whether the entry's flags are known and carry every bit of flag (flag ≠ 0).
func (e Entry) Has(flag uint32) bool {
	return e.FlagsKnown() && flag != 0 && uint32(e.Flags.Value)&flag == flag
}

// Valid is §4.1 step 3's rule for a folder's entry (TC-2 and the M4 re-read), for an IMAP store,
// the only kind C1 reads: flags, totalMsgs, expungedBytes and serverTotal are OK; and
// pendingMsgs, folderSize and lastSyncTimeInSec, which may be Missing, are not Broken. The
// account root is never validated and is always Valid.
//
// Thunderbird writes expungedBytes for every folder at ESR 153 and Release 159, so one absent is
// an entry it did not write. serverTotal is required of every folder, not only of one carrying
// the ImapBox flag 0x2000 (C1a pass C1A-07, the lead's call): every folder of an IMAP account's
// tree is an IMAP folder (nsImapMailFolder::AddSubfolderWithPath refuses a child that is not,
// nsImapMailFolder.cpp ESR 153 :353-356), and an IMAP folder writes serverTotal unconditionally
// (WriteToFolderCacheElem :1901; Release 159 :1974), while ImapBox is set only once the server
// has listed the folder (SetBoxFlags :1107, the one place it is set). So a real folder without
// ImapBox, such as one the server has not listed yet, still has serverTotal, and an entry without
// serverTotal is one Thunderbird did not write.
func (e Entry) Valid() bool {
	if e.Root {
		return true
	}
	if e.Flags.State != OK || e.TotalMsgs.State != OK || e.ExpungedBytes.State != OK || e.ServerTotal.State != OK {
		return false
	}
	for _, f := range []Field{e.PendingMsgs, e.FolderSize, e.LastSyncTimeInSec} {
		if f.State == Broken {
			return false
		}
	}
	return true
}

// NotCountedYet reports whether the entry's one failure is a totalMsgs of -1 (NotCounted): it would
// be Valid with a count. C1b words such a folder "not counted by Thunderbird yet", never
// invalid_entry's "changed outside Thunderbird" (C1a pass C1A-01). False for a Valid entry, for
// one that fails for any other reason too, and for the root (whose -1 is read as a value).
func (e Entry) NotCountedYet() bool {
	if e.TotalMsgs.State != NotCounted {
		return false
	}
	e.TotalMsgs = Field{State: OK}
	return e.Valid()
}

// Listed is what Thunderbird lists for the folder (design §2.2): totalMsgs + pendingMsgs, or
// serverTotal when that is larger. ok is false for the root and for an entry that is not Valid.
func (e Entry) Listed() (int64, bool) {
	if e.Root || !e.Valid() {
		return 0, false
	}
	n := e.TotalMsgs.Value
	if e.PendingMsgs.State == OK {
		n += e.PendingMsgs.Value
	}
	if e.ServerTotal.State == OK && e.ServerTotal.Value > n {
		n = e.ServerTotal.Value
	}
	return n, true
}

// Mode is how a store's keys were matched (foldcheck M2's one rule).
type Mode int

const (
	// NoKeys: no key of the cache belongs to the store (C1b: can't tell).
	NoKeys Mode = iota
	// Exact: some key matches the store's current path. Old-path keys are then ignored beside an
	// exact key for the same folder, and otherwise make the folder "can't tell" (OldPathOnly)
	// while it is still in the store. A key that matches the current path byte for byte wins:
	// when one does, a key that matches only once case or Unicode form is folded is an old-path
	// key (C1a pass C1A-05: Thunderbird looks keys up byte for byte, nsMsgFolderCache.cpp :335,
	// so after a respelling of the profile's path it writes new keys beside the old ones).
	Exact
	// Tail: no key matches exactly (a moved or restored profile Thunderbird has not opened since):
	// the keys are matched by their tail under the store's folder name, itself under a folder
	// named as the store's parent (ImapMail for an ImapMail store: §4.1 step 2's "the key's path
	// under ImapMail/<host>/", C1a pass C1A-06).
	Tail
	// CantTellKeys: the reader cannot tell which keys are the store's (C1b: can't tell, like
	// NoKeys), and gives no folders; CantTellWhy says why. In tail mode, the keys found by the
	// store's folder name come from more than one place, so they may be another account's
	// (C1A-06, the lead's call); or, on Windows, the store's path exists but its long or 8.3
	// short spelling could not be found (C1A-08).
	CantTellKeys
)

// Refusal is a key of the store the reader would not use: its tail is refused before any file
// check (foldcheck2 L2: a "." or ".." part, an absolute tail, a drive letter, a UNC prefix, a NUL),
// or, for a key under the store's current path, it is not shaped like a folder's key. Thunderbird
// never writes such a key; C1b reads it as invalid_entry.
type Refusal struct {
	Key string
	Why string
}

// AllMailKind is the answer to "which folder of the store is All Mail". Its zero value is
// AllMailCantTell, so an answer nobody set never names a folder.
type AllMailKind int

const (
	// AllMailCantTell: a folder that might be All Mail cannot be read (its flags broken, two keys
	// for it, or only an old-path key while it is still in the store).
	AllMailCantTell AllMailKind = iota
	// AllMailNone: no folder carries the All Mail flag 0x8000 (an Archive-only folder, 0x4000,
	// never counts: foldcheck M5).
	AllMailNone
	// AllMailSeveral: more than one folder carries 0x8000; none is picked.
	AllMailSeveral
	// AllMailOne: exactly one folder carries 0x8000, and Entry is it.
	AllMailOne
)

// AllMail is the store's All Mail, read from folderCache.json (the lead's call 2026-11-08 [B1
// split]: this reader owns it, not thunderbird.GmailForStore). Only AllMailOne names a folder;
// every other answer is "All Mail is absent" (M5), which for Gmail makes Y a floor (C1b).
type AllMail struct {
	Kind  AllMailKind
	Entry *Entry // a copy of the folder's entry, when Kind is AllMailOne
}

// Store is one store's view of the cache.
type Store struct {
	Mode Mode
	// CantTellWhy says why, when Mode is CantTellKeys (a log's words; C3 gives the surfaces').
	CantTellWhy string
	// Root is the account root's entry, when there is exactly one (exact mode: its exact key).
	Root *Entry
	// Folders are the folders read, one entry each, sorted by Path. An entry here may still be
	// not Valid, or have flags that are not known: C1b decides.
	Folders []Entry
	// OldPathOnly are M2's "can't tell" folders: in exact mode, a folder whose only keys are
	// old-path keys while its mailbox file or .msf is still in the store. Sorted by Path; never in
	// Folders.
	OldPathOnly []Entry
	// Duplicates are the Paths of folders with two keys (two exact keys, or two tail keys in tail
	// mode; "" for the root), sorted. Never in Folders.
	Duplicates []string
	// DuplicateKeys are the store's keys that appear more than once in the file (TC-15), sorted.
	DuplicateKeys []string
	// Refused are the store's keys the reader would not use, in file order.
	Refused []Refusal
	// IgnoredOldKeys counts old-path keys ignored: beside an exact key for the same folder, for
	// the root, or for a folder no longer in the store (M2: "ignored and counted").
	IgnoredOldKeys int
	// AllMail is the store's All Mail.
	AllMail AllMail
}

// CacheErrorKind is why ReadCache refused the file.
type CacheErrorKind int

const (
	CacheMissing    CacheErrorKind = iota + 1 // no folderCache.json (a profile never opened, panacea.dat)
	CacheNotRegular                           // a link, a pipe, a device or a folder at the name
	CacheUnreadable                           // over CacheMaxBytes, or an error reading it
	CacheMalformed                            // not one JSON object
)

// CacheError is ReadCache's refusal. Its words are a log's; the surfaces give their own (C3).
type CacheError struct {
	Kind CacheErrorKind
	Path string
	Err  error
}

func (e *CacheError) Error() string {
	return fmt.Sprintf("MailArchive could not read Thunderbird's list of folders, %s (%s): %v",
		CacheFile, util.StripControl(e.Path), e.Err)
}

func (e *CacheError) Unwrap() error { return e.Err }

// keyed is one top-level key of the file and its value, as read.
type keyed struct {
	key string
	raw json.RawMessage
}

// Cache is a folderCache.json as read: its keys in file order, each value kept raw until a store
// asks for it. It is held in memory for one run (one read per profile per run, TC-21).
type Cache struct {
	pairs []keyed
	count map[string]int // how many times each key appears
}

// lstat is os.Lstat; a test may observe the paths checked through it.
var lstat = os.Lstat

// ReadCache reads profileDir's folderCache.json through util.ReadRegularFile: a link is refused,
// not followed; a pipe or device is neither waited on nor read; a file over CacheMaxBytes is
// refused. Any refusal, and a file that is not one JSON object, is a *CacheError.
func ReadCache(profileDir string) (*Cache, error) {
	path := filepath.Join(profileDir, CacheFile)
	data, err := util.ReadRegularFile(path, CacheMaxBytes)
	if err != nil {
		kind := CacheUnreadable
		var nr *util.NotRegularError
		switch {
		case errors.Is(err, fs.ErrNotExist):
			kind = CacheMissing
		case errors.As(err, &nr):
			kind = CacheNotRegular
		}
		return nil, &CacheError{Kind: kind, Path: path, Err: err}
	}
	c, err := Parse(data)
	if err != nil {
		return nil, &CacheError{Kind: CacheMalformed, Path: path, Err: err}
	}
	return c, nil
}

// Parse decodes a folder cache: exactly one JSON object, nothing but white space after it, walked
// key by key so a key that appears twice is counted (Go's plain decoding would keep the last one
// silently). Each value is kept raw; it is read when a store asks for it. A file nested deeper
// than CacheMaxDepth, which Thunderbird itself does not read, is refused.
func Parse(data []byte) (*Cache, error) {
	if !withinDepth(data, CacheMaxDepth) {
		return nil, fmt.Errorf("nested deeper than Thunderbird reads (%d levels)", CacheMaxDepth)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("not JSON: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	c := &Cache{count: map[string]int{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("not JSON: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("not JSON: a key that is not a string")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("not JSON: %w", err)
		}
		c.pairs = append(c.pairs, keyed{key, raw})
		c.count[key]++
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, fmt.Errorf("not JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("more after the JSON object")
	}
	return c, nil
}

// withinDepth reports whether no value of data sits deeper than limit, the top-level value being
// at depth 1, as jsoncpp counts (OurReader::readValue refuses a value once limit nodes enclose
// it): a list or object at depth limit may be empty, since its members would be one deeper. It
// scans bytes outside strings only; what it lets through is then decoded as JSON, so a file that
// is not JSON is refused there.
func withinDepth(data []byte, limit int) bool {
	depth, inString, escaped := 0, false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > limit {
				return false
			}
			if depth == limit {
				j := i + 1
				for j < len(data) && (data[j] == ' ' || data[j] == '\t' || data[j] == '\n' || data[j] == '\r') {
					j++
				}
				if j < len(data) && data[j] != '}' && data[j] != ']' {
					return false
				}
			}
		case '}', ']':
			depth--
		}
	}
	return true
}

// spellingsOf gives the further spellings of an absolute store path that this OS's file system
// gives it: on Windows its long and its 8.3 short spelling (spell_windows.go), elsewhere none. An
// error means the path exists and its spellings could not be found.
var spellingsOf = osSpellings

// Store is the view of the store folder storeDir. The store's current path is storeDir made
// absolute, and also that path with its links resolved, when that differs (a key written by
// Thunderbird through either spelling is exact); on Windows each part of it may also be spelled
// as its long or its 8.3 short name (C1a pass C1A-08: Thunderbird may hold the profile's path
// in either spelling). Keys are compared by this OS's rules. When the store's path exists and its
// Windows spellings cannot be found, the view is CantTellKeys.
func (c *Cache) Store(storeDir string) (Store, error) {
	abs, err := filepath.Abs(storeDir)
	if err != nil {
		return Store{}, err
	}
	spellings := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		spellings = append(spellings, real)
	}
	variants, err := spellingsOf(abs)
	if err != nil {
		return Store{Mode: CantTellKeys, CantTellWhy: "could not find the long and short spellings of the store's folder: " + err.Error()}, nil
	}
	return c.storeAt(spellings, runtime.GOOS, variants...), nil
}

// one is a key of the store, as matched.
type one struct {
	e      Entry
	id     string   // the folder's identity: its tail compared as goos compares names
	tail   []string // the tail as written, split by goos's rules
	exact  bool
	prefix string // an old-path key's place: the key's parts before its tail, compared as goos compares names
}

// place is one spelling of the store's current path, split: each part with the spellings it may
// take (more than one on Windows only: the part as given, long and 8.3 short).
type place [][]string

// How a key matched the store.
const (
	byteExact = iota + 1 // the store's path, part for part, byte for byte
	foldExact            // the store's path once each part is compared as goos compares names
	byName               // its tail under a part named as the store, itself under the store's parent's name
)

// storeAt is Store for the store whose current path has the given spellings (absolute; the first
// is the one file checks are built on), comparing by goos's path rules (the design §4.1 step 2
// fold, read with MA-1265's rule that a backslash separates folders on Windows only). variants
// are further spellings of the first (Windows' long and short names): one with as many parts is
// merged into it part by part, so a path spelled long in one part and short in another still
// matches; one with another number of parts is a spelling of its own.
func (c *Cache) storeAt(spellings []string, goos string, variants ...string) Store {
	var s Store
	var places, extras []place
	for i, sp := range spellings {
		parts := storeParts(sp, goos)
		if len(parts) == 0 {
			continue
		}
		pl := asPlace(parts)
		for _, v := range variants {
			switch vp := storeParts(v, goos); {
			case i > 0 || len(vp) == 0:
			case len(vp) == len(parts):
				for k, p := range vp {
					if !slices.Contains(pl[k], p) {
						pl[k] = append(pl[k], p)
					}
				}
			default:
				extras = append(extras, asPlace(vp))
			}
		}
		places = append(places, pl)
	}
	places = append(places, extras...)
	if len(places) == 0 {
		return s
	}
	first := places[0]
	name, parent := first[len(first)-1], []string{""}
	if len(first) > 1 {
		parent = first[len(first)-2]
	}
	strict := func(a, b string) bool { return a == b }
	folded := func(a, b string) bool { return same(a, goos) == same(b, goos) }

	// First, how each key matches; a byte-for-byte match anywhere makes folded matches old-path
	// keys (C1A-05).
	type matched struct {
		kv   keyed
		raw  []string
		tail []string
		how  int
	}
	var keys []matched
	anyByteExact := false
	for _, kv := range c.pairs {
		raw := splitPath(kv.key, goos)
		m := matched{kv: kv, raw: raw}
		for _, pl := range places {
			if tail, ok := exactTail(raw, pl, strict); ok {
				m.tail, m.how = tail, byteExact
				break
			}
		}
		if m.how == 0 {
			for _, pl := range places {
				if tail, ok := exactTail(raw, pl, folded); ok {
					m.tail, m.how = tail, foldExact
					break
				}
			}
		}
		if m.how == 0 {
			tail, ok := nameTail(raw, name, parent, folded)
			if !ok {
				continue // another store's key, or no store's
			}
			m.tail, m.how = tail, byName
		}
		if m.how == byteExact && refusedTail(m.tail, goos) == "" {
			if _, ok := folderPath(m.tail); ok || len(m.tail) == 0 {
				anyByteExact = true
			}
		}
		keys = append(keys, m)
	}

	dupKeys := map[string]bool{}
	var mine []one
	for _, m := range keys {
		kv, tail := m.kv, m.tail
		exact := m.how == byteExact || m.how == foldExact && !anyByteExact
		if c.count[kv.key] > 1 && !dupKeys[kv.key] {
			dupKeys[kv.key] = true
			s.DuplicateKeys = append(s.DuplicateKeys, kv.key)
		}
		if why := refusedTail(tail, goos); why != "" {
			s.Refused = append(s.Refused, Refusal{kv.key, why})
			continue
		}
		path, isRoot := "", len(tail) == 0
		if !isRoot {
			var ok bool
			if path, ok = folderPath(tail); !ok {
				if exact {
					s.Refused = append(s.Refused, Refusal{kv.key, "not the key of a folder (Thunderbird's keys end in .msf, under folders that end in .sbd)"})
				}
				continue // an old-path key of another shape is no folder of this store's
			}
		}
		e := readEntry(kv.key, kv.raw, isRoot)
		e.Path = path
		mine = append(mine, one{e: e, id: identity(tail, goos), tail: tail, exact: exact,
			prefix: identity(nonEmpty(m.raw[:len(m.raw)-len(tail)]), goos)})
	}
	sort.Strings(s.DuplicateKeys)

	byID := func(exact bool) (map[string][]one, []string) {
		m := map[string][]one{}
		var ids []string
		for _, o := range mine {
			if o.exact == exact {
				if _, seen := m[o.id]; !seen {
					ids = append(ids, o.id)
				}
				m[o.id] = append(m[o.id], o)
			}
		}
		sort.Strings(ids)
		return m, ids
	}
	exactIDs, exactOrder := byID(true)
	oldIDs, oldOrder := byID(false)
	var dupEntries []Entry
	take := func(m map[string][]one, order []string) {
		for _, id := range order {
			list := m[id]
			switch {
			case len(list) > 1:
				s.Duplicates = append(s.Duplicates, list[0].e.Path)
				for _, o := range list {
					dupEntries = append(dupEntries, o.e)
				}
			case list[0].e.Root:
				r := list[0].e
				s.Root = &r
			default:
				s.Folders = append(s.Folders, list[0].e)
			}
		}
	}
	switch {
	case len(exactIDs) > 0:
		s.Mode = Exact
		take(exactIDs, exactOrder)
		for _, id := range oldOrder {
			list := oldIDs[id]
			if _, has := exactIDs[id]; has || list[0].e.Root {
				s.IgnoredOldKeys += len(list) // left behind by a move (bug 1726660)
				continue
			}
			still := false
			for _, o := range list {
				if inStore(spellings[0], o.tail) {
					still = true
					break
				}
			}
			if still {
				s.OldPathOnly = append(s.OldPathOnly, list[0].e)
			} else {
				s.IgnoredOldKeys += len(list) // a folder gone from the store
			}
		}
	case len(oldIDs) > 0:
		from := map[string]bool{}
		for _, o := range mine {
			from[o.prefix] = true
		}
		if len(from) > 1 {
			// Keys from two places named as the store: the store moved twice, or another
			// account's folder has the store's name. Which keys are this store's cannot be told
			// (C1A-06, the lead's call); Thunderbird's next save writes the current keys.
			return Store{Mode: CantTellKeys, DuplicateKeys: s.DuplicateKeys, Refused: s.Refused,
				CantTellWhy: fmt.Sprintf("no key matches the store's current path, and the keys found by its folder's name come from %d places", len(from))}
		}
		s.Mode = Tail
		take(oldIDs, oldOrder)
	}
	sort.SliceStable(s.Folders, func(i, j int) bool { return s.Folders[i].Path < s.Folders[j].Path })
	sort.SliceStable(s.OldPathOnly, func(i, j int) bool { return s.OldPathOnly[i].Path < s.OldPathOnly[j].Path })
	sort.Strings(s.Duplicates)
	s.AllMail = allMail(s.Folders, append(dupEntries, s.OldPathOnly...))
	return s
}

// allMail is the one folder of folders carrying 0x8000 (foldcheck M5). unsure are entries that
// are not folders of the store as read (duplicates, old-path-only): any of them that carries
// 0x8000, or whose flags are not known, could be All Mail, and so could a folder whose flags are
// not known.
func allMail(folders, unsure []Entry) AllMail {
	var found []Entry
	cantTell := false
	for _, e := range folders {
		switch {
		case !e.FlagsKnown():
			cantTell = true
		case e.Has(FlagAllMail):
			found = append(found, e)
		}
	}
	for _, e := range unsure {
		if !e.FlagsKnown() || e.Has(FlagAllMail) {
			cantTell = true
		}
	}
	switch {
	case len(found) > 1:
		return AllMail{Kind: AllMailSeveral}
	case cantTell:
		return AllMail{Kind: AllMailCantTell}
	case len(found) == 0:
		return AllMail{Kind: AllMailNone}
	}
	e := found[0]
	return AllMail{Kind: AllMailOne, Entry: &e}
}

// uncRoot is the first part of a Windows path that begins with two separators (a UNC share,
// \\server\share\…): a root of its own, so it never equals the "" root of a path rooted on the
// current drive (\server\share\…), which is another place (C1a pass C1A-04).
const uncRoot = `\\`

// splitPath splits p at its separators by goos's rules ("/", and on Windows "\" too), keeping
// empty parts (a doubled separator, a leading one), so nothing is resolved or hidden. On Windows
// a leading pair of separators is one part, uncRoot.
func splitPath(p, goos string) []string {
	if goos == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
		if rest, ok := strings.CutPrefix(p, "//"); ok {
			return append([]string{uncRoot}, strings.Split(rest, "/")...)
		}
	}
	return strings.Split(p, "/")
}

// storeParts is the store's absolute path split by goos's rules, with empty parts dropped after
// the first (a leading "" stands for the root, as thunderbird's samePath splits).
func storeParts(p, goos string) []string {
	var out []string
	for i, s := range splitPath(p, goos) {
		if s != "" || i == 0 {
			out = append(out, s)
		}
	}
	if len(out) == 1 && out[0] == "" {
		return nil
	}
	return out
}

// same is the form goos compares a path part in: Unicode NFC everywhere (design §4.1 step 2 item
// 1), and on Windows and macOS, whose usual file systems are case-blind, case-folded as
// util.FoldKey folds (which also widens NTFS's upcase table).
func same(part, goos string) string {
	if goos == "windows" || goos == "darwin" {
		return util.FoldKey(part)
	}
	return norm.NFC.String(part)
}

// exactTail matches raw (a key, split) against cur (a spelling of the store's path, split): when
// the key starts with the store's path, part for part by eq (a doubled separator inside the
// store's path is one), it gives the rest of the key, its tail ([] for the store folder itself).
func exactTail(raw []string, cur place, eq func(a, b string) bool) ([]string, bool) {
	k := 0
	for j, part := range raw {
		if k == len(cur) {
			return raw[j:], true
		}
		if part == "" && j > 0 {
			continue
		}
		if !anyEq(part, cur[k], eq) {
			return nil, false
		}
		k++
	}
	return nil, k == len(cur)
}

// nameTail is the tail of raw after its last part named as the store folder is, when the part
// before it is named as the store's parent (an old-path key of the store, from before a move:
// design §4.1 step 2's "the key's path under ImapMail/<host>/"; C1a pass C1A-06, so another
// account's folder that only shares the store's name, elsewhere, is not the store). The last such
// part is the one, since a folder's key below it has only parts that end in .sbd or .msf.
func nameTail(raw []string, name, parent []string, eq func(a, b string) bool) ([]string, bool) {
	for i := len(raw) - 1; i >= 1; i-- {
		if anyEq(raw[i], name, eq) {
			if !anyEq(raw[i-1], parent, eq) {
				return nil, false
			}
			return raw[i+1:], true
		}
	}
	return nil, false
}

// anyEq reports whether part equals, by eq, one of the spellings.
func anyEq(part string, spellings []string, eq func(a, b string) bool) bool {
	for _, sp := range spellings {
		if eq(part, sp) {
			return true
		}
	}
	return false
}

// asPlace is parts as a place of one spelling each.
func asPlace(parts []string) place {
	pl := make(place, len(parts))
	for k, p := range parts {
		pl[k] = []string{p}
	}
	return pl
}

// nonEmpty is parts without its empty ones but the first (the root), for comparing two keys'
// places whatever doubled separators they hold.
func nonEmpty(parts []string) []string {
	var out []string
	for i, p := range parts {
		if p != "" || i == 0 {
			out = append(out, p)
		}
	}
	return out
}

var driveLetter = regexp.MustCompile(`^[A-Za-z]:`)

// refusedTail is why tail is refused before any file check is built on it (foldcheck2 L2), or
// "". Each part is read split at "/" and "\" alike, on every OS, so a tail no OS could resolve
// inside the store is refused whatever OS reads it: an empty piece (a leading, doubled or trailing
// separator: an absolute or UNC tail), a "." or ".." piece, a drive letter, or a NUL byte; and on
// Windows a colon anywhere (a drive letter or a stream name). A part is also read without its
// ".msf" or ".sbd" ending, since the M2 check builds the mailbox file's path from the folder's name
// alone: "..msf" (a folder named ".") and "...msf" (".."), which would check the store itself or
// its parent, are refused too (found by FuzzFolderCache), and so is a part of dots and spaces only
// (".. ", "..."), which Windows reads as "." or "..". Thunderbird never names a folder so
// (nsMsgUtils' NS_MsgHashIfNecessary hashes a leading dot and a trailing dot or space).
func refusedTail(tail []string, goos string) string {
	for _, part := range tail {
		if goos == "windows" && strings.Contains(part, ":") {
			return "a colon in a part (a drive letter or a Windows stream name)"
		}
		pieces := strings.Split(strings.ReplaceAll(part, `\`, "/"), "/")
		last := pieces[len(pieces)-1]
		if name := strings.TrimSuffix(strings.TrimSuffix(last, ".msf"), ".sbd"); name != last && name != "" {
			pieces = append(pieces, name)
		}
		for _, piece := range pieces {
			switch {
			case strings.Trim(piece, ". ") == "":
				// "" (an absolute, UNC, doubled or trailing separator), "." and "..", and, since
				// Windows drops a part's trailing dots and spaces, ".. " and "...", which name the
				// folder itself or its parent there.
				return "an empty, . or .. part, or a part of dots and spaces only (an absolute or UNC tail, or a folder named so)"
			case strings.IndexByte(piece, 0) >= 0:
				return "a NUL byte"
			case driveLetter.MatchString(piece):
				return "a drive letter"
			}
		}
	}
	return ""
}

// folderPath is the folder's Path for a key's tail shaped as Thunderbird writes a folder's key:
// every part but the last ends in ".sbd" and the last in ".msf", each with a name before it
// (Thunderbird writes both endings in lower case). ok is false for any other shape.
func folderPath(tail []string) (string, bool) {
	names := make([]string, len(tail))
	for i, part := range tail {
		end := ".sbd"
		if i == len(tail)-1 {
			end = ".msf"
		}
		if len(part) <= len(end) || !strings.HasSuffix(part, end) {
			return "", false
		}
		names[i] = strings.TrimSuffix(part, end)
	}
	return strings.Join(names, "/"), true
}

// identity is the folder's identity for "two keys for one folder": its tail compared as goos
// compares names.
func identity(tail []string, goos string) string {
	parts := make([]string, len(tail))
	for i, p := range tail {
		parts[i] = same(p, goos)
	}
	return strings.Join(parts, "/")
}

// inStore reports whether the folder whose old-path key has this tail is still in the store
// folder store (foldcheck M2): its .msf, or its mailbox file (the tail without ".msf"), is there.
// The paths are built from the store and the tail as written (already checked by refusedTail),
// never from the key, and checked with Lstat one part at a time: nothing is opened, and a link is never
// followed. A link where a folder should be, or any error but "not there", reads as "still in the
// store" (can't tell), which fails toward `unknown`.
func inStore(store string, tail []string) bool {
	mailbox := append(append([]string(nil), tail[:len(tail)-1]...), strings.TrimSuffix(tail[len(tail)-1], ".msf"))
	return present(store, tail) || present(store, mailbox)
}

// present is inStore's check of one path, store/parts…
func present(store string, parts []string) bool {
	p := store
	for i, part := range parts {
		p = filepath.Join(p, part)
		fi, err := lstat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false
		case err != nil:
			return true
		case i < len(parts)-1 && fi.Mode()&fs.ModeSymlink != 0:
			return true // never followed: the folder may be there
		case i < len(parts)-1 && !fi.IsDir():
			return false // a file where the folder would be: nothing can be under it
		}
	}
	return true
}

// The fields this reader reads, and their kinds (the §2.1 fold's types, held at ESR 153 and
// Release 159: the source re-read's claims 1a and 1b).
const (
	kindUint32 = iota
	kindInt32
	kindInt64
)

// readEntry reads one entry's value: an object whose fields are read by name, each once (a field
// named twice is Broken, since Thunderbird never writes one twice). A value that is not an object
// is NotObject. Off the root, a negative value is Broken (§4.1 step 3), except lastSyncTimeInSec,
// where ≤ 0 means never checked (the year-2038 wrap of Thunderbird's 32-bit seconds included),
// and -1 exactly where Thunderbird itself writes it for "not known yet" (the [C1a pass C1A-01]
// fold): folderSize -1 is Missing (kSizeUnknown, the value every folder's size starts at,
// nsMsgDBFolder.cpp ESR 153 :243, written as it is at :1233), and totalMsgs -1 is NotCounted
// (mNumTotalMessages(-1), :236). expungedBytes keeps the plain rule (unsettled: its database
// arithmetic was not read).
func readEntry(key string, raw json.RawMessage, isRoot bool) Entry {
	e := Entry{Key: key, Root: isRoot}
	vals, seen, ok := objectFields(raw)
	if !ok {
		e.NotObject = true
		return e
	}
	sizeUnknown := Field{State: Missing, Why: "-1: Thunderbird has not worked out the folder's size yet"}
	notCounted := Field{State: NotCounted, Why: "-1: Thunderbird has not counted the folder's messages yet"}
	for _, f := range []struct {
		name     string
		kind     int
		dst      *Field
		neg      bool   // may be negative off the root
		minusOne *Field // what -1 off the root reads as, when not Broken
	}{
		{"flags", kindUint32, &e.Flags, false, nil},
		{"totalMsgs", kindInt32, &e.TotalMsgs, false, &notCounted},
		{"pendingMsgs", kindInt32, &e.PendingMsgs, false, nil},
		{"serverTotal", kindInt32, &e.ServerTotal, false, nil},
		{"expungedBytes", kindInt64, &e.ExpungedBytes, false, nil},
		{"folderSize", kindInt64, &e.FolderSize, false, &sizeUnknown},
		{"lastSyncTimeInSec", kindInt32, &e.LastSyncTimeInSec, true, nil},
	} {
		v, present := vals[f.name]
		switch {
		case !present:
		case seen[f.name] > 1:
			*f.dst = Field{State: Broken, Why: "named twice in one entry"}
		default:
			*f.dst = intField(v, f.kind)
			if isRoot || f.neg || f.dst.State != OK || f.dst.Value >= 0 {
				break
			}
			if f.dst.Value == -1 && f.minusOne != nil {
				*f.dst = *f.minusOne
			} else {
				*f.dst = Field{State: Broken, Why: "negative (Thunderbird writes no negative value here off the account root)"}
			}
		}
	}
	return e
}

// objectFields walks raw, when it is a JSON object, field by field: each field's last value and
// how many times its name appears.
func objectFields(raw json.RawMessage) (map[string]json.RawMessage, map[string]int, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, false
	}
	vals, seen := map[string]json.RawMessage{}, map[string]int{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, false
		}
		name, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, false
		}
		vals[name] = v
		seen[name]++
	}
	return vals, seen, true
}

var wholeNumber = regexp.MustCompile(`^-?[0-9]+$`)

// intField reads v as a JSON integer of kind: digits with an optional minus only (1.5, 1e3 and
// 1e308 are not whole numbers as Thunderbird writes them; -0 is 0), within the kind's range.
func intField(v json.RawMessage, kind int) Field {
	s := strings.TrimSpace(string(v))
	if !wholeNumber.MatchString(s) {
		why := "not a whole number"
		switch {
		case strings.HasPrefix(s, `"`):
			why = "a quoted text"
		case s == "true" || s == "false":
			why = "true or false"
		case s == "null":
			why = "null"
		case strings.HasPrefix(s, "{") || strings.HasPrefix(s, "["):
			why = "an object or a list"
		}
		return Field{State: Broken, Why: why}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return Field{State: Broken, Why: "out of range"}
	}
	lo, hi := int64(-1<<63), int64(1<<63-1)
	switch kind {
	case kindUint32:
		lo, hi = 0, 1<<32-1
	case kindInt32:
		lo, hi = -1<<31, 1<<31-1
	}
	if n < lo || n > hi {
		return Field{State: Broken, Why: "out of range"}
	}
	return Field{State: OK, Value: n}
}
