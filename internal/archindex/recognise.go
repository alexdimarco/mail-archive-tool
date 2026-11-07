package archindex

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"mail-archive-tool/internal/state"
)

// Recognition of a plain archive (SD-2, amended 2026-11-02 by MA-1034, the 1.5.3 re-walk's N1, and
// by MA-1035, its adversarial pass). Recognition used to read at most 1 MiB of the manifest and
// parse it whole with state.LoadFrom, so an archive of more than about 2,200 messages was "not an
// archive": greyed in Your archives with no Switch, refused by Open an archive folder… and by
// add-archive. It now walks the manifest's bytes as they stream in, checking its JSON as it goes,
// under bounds that keep both the read and the memory small whatever the file holds:
//
//   - manifestCap bounds the bytes read (archdelete's bound for real archives);
//   - maxStoresBytes bounds the stores kept to tell the kind: their text and storeEntryCost for
//     each store;
//   - maxHeldBytes bounds a top-level name or "version" value held to be read;
//   - maxDepth bounds nesting.
//
// Nothing else is held: a record's value (a subject json.Marshal wrote as six bytes a character,
// a path) is checked byte by byte and skipped, whatever its length (MA-1035: one email whose
// subject was written over 1 MiB made the archive "not an archive" when each value was held).
//
// It checks what makes the file a manifest this build can read — one JSON object (or null, or
// nothing at all, as LoadFrom reads them), a version no newer than this build's, a "stores"
// object of strings, an "entries" object of records, a "capture_gaps" object of gaps and a
// "collapsed_tokens" object of true or false (MA-1035, amended: the two fields beside the records
// are checked as LoadFrom types them) — and keeps only "stores", whose ids are read lexically
// (state.SnapshotStoreID: never resolved on disk, SD-1). The records and the gaps themselves are
// skipped, never kept nor their contents checked: each reader that uses them (status, a backup, a
// delete's plan) reads the whole manifest itself and refuses what it cannot use.

// manifestCap bounds recognition's read of a manifest: sized for real archives, as a delete's
// manifest read is (archdelete's manifestCap, "sized for real archives"). A var only so a test can
// stand in for a manifest over it without writing 1 GiB.
var manifestCap int64 = 1 << 30

const (
	// maxStoresBytes bounds what the stores kept to tell the kind cost: their text as the manifest
	// writes it, and storeEntryCost for each store (what a map entry costs beyond its text). A
	// real archive has a handful of stores; 1,000 sources with ids of real length fit.
	maxStoresBytes = 1 << 20
	storeEntryCost = 64
	// maxHeldBytes bounds a top-level name or a "version" value held to be read. A name longer
	// than this cannot be "version", "stores" or "entries" in any spelling json.Unmarshal matches,
	// and a version longer than this is no integer: LoadFrom refuses it too.
	maxHeldBytes = 256
	// maxDepth bounds nesting: a real manifest nests five or six deep.
	maxDepth = 64
)

// Why a manifest is not recognised. errReadManifest wraps a failure to read the file, which may
// pass next time (a sharing violation, an I/O error), so its verdict is not kept.
var (
	errManifestTooLarge = errors.New("manifest over its cap")
	errManifestDepth    = errors.New("manifest nests too deep")
	errManifestShape    = errors.New("manifest is not a manifest object")
	errManifestStores   = errors.New("manifest's stores are over their cap")
)

type errReadManifest struct{ err error }

func (e *errReadManifest) Error() string { return e.err.Error() }
func (e *errReadManifest) Unwrap() error { return e.err }

// capReader is the manifest as the walk reads it: it counts the bytes, gives at most manifestCap+1
// of them, and refuses once more than manifestCap were read.
type capReader struct {
	r io.Reader
	n int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.n > manifestCap {
		return 0, errManifestTooLarge
	}
	if room := manifestCap + 1 - c.n; int64(len(p)) > room {
		p = p[:room]
	}
	n, err := c.r.Read(p)
	c.n += int64(n)
	if err != nil && err != io.EOF {
		return n, &errReadManifest{err}
	}
	return n, err
}

// scanner walks a manifest's bytes. While holding, the bytes it consumes are also kept in held, up
// to heldMax (over notes that the value went beyond it); members counts the names of the object at
// depth countAt (the stores).
type scanner struct {
	r *bufio.Reader

	holding bool
	held    []byte
	heldMax int
	over    bool
	countAt int
	members int
}

// next is the next byte; the end of the file inside a value does not parse.
func (s *scanner) next() (byte, error) {
	b, err := s.r.ReadByte()
	if err == io.EOF {
		return 0, errManifestShape
	}
	if err != nil {
		return 0, err
	}
	if s.holding {
		if len(s.held) < s.heldMax {
			s.held = append(s.held, b)
		} else {
			s.over = true
		}
	}
	return b, nil
}

// peek is the next byte without consuming it; ok is false at the end of the file.
func (s *scanner) peek() (b byte, ok bool, err error) {
	p, err := s.r.Peek(1)
	if err == io.EOF {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return p[0], true, nil
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// ws is the next byte that is not white space.
func (s *scanner) ws() (byte, error) {
	for {
		b, err := s.next()
		if err != nil || !isSpace(b) {
			return b, err
		}
	}
}

// wsOrEOF is ws outside the top-level value, where the end of the file is io.EOF.
func (s *scanner) wsOrEOF() (byte, error) {
	for {
		b, err := s.r.ReadByte()
		if err != nil || !isSpace(b) {
			return b, err
		}
	}
}

// value checks one JSON value whose first byte c was read, held by depth objects and arrays.
func (s *scanner) value(c byte, depth int) error {
	switch {
	case c == '{':
		return s.object(depth + 1)
	case c == '[':
		return s.array(depth + 1)
	case c == '"':
		return s.str()
	case c == 't':
		return s.lit("rue")
	case c == 'f':
		return s.lit("alse")
	case c == 'n':
		return s.lit("ull")
	case c == '-' || (c >= '0' && c <= '9'):
		return s.number(c)
	}
	return errManifestShape
}

func (s *scanner) lit(rest string) error {
	for i := 0; i < len(rest); i++ {
		b, err := s.next()
		if err != nil {
			return err
		}
		if b != rest[i] {
			return errManifestShape
		}
	}
	return nil
}

// str checks a string after its opening quote, through its closing one. As encoding/json reads a
// string, a control character is refused and any other byte, valid UTF-8 or not, is taken.
func (s *scanner) str() error {
	for {
		b, err := s.next()
		if err != nil {
			return err
		}
		switch {
		case b == '"':
			return nil
		case b < 0x20:
			return errManifestShape
		case b == '\\':
			e, err := s.next()
			if err != nil {
				return err
			}
			switch e {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				for i := 0; i < 4; i++ {
					h, err := s.next()
					if err != nil {
						return err
					}
					if !(h >= '0' && h <= '9' || h >= 'a' && h <= 'f' || h >= 'A' && h <= 'F') {
						return errManifestShape
					}
				}
			default:
				return errManifestShape
			}
		}
	}
}

// digits consumes the digits that follow, at least min of them.
func (s *scanner) digits(min int) error {
	for n := 0; ; n++ {
		b, ok, err := s.peek()
		if err != nil {
			return err
		}
		if !ok || b < '0' || b > '9' {
			if n < min {
				return errManifestShape
			}
			return nil
		}
		if _, err := s.next(); err != nil {
			return err
		}
	}
}

// number checks a number whose first byte c was read: -?(0|[1-9][0-9]*)(.[0-9]+)?([eE][+-]?[0-9]+)?
func (s *scanner) number(c byte) error {
	if c == '-' {
		var err error
		if c, err = s.next(); err != nil {
			return err
		}
	}
	switch {
	case c == '0':
	case c >= '1' && c <= '9':
		if err := s.digits(0); err != nil {
			return err
		}
	default:
		return errManifestShape
	}
	b, ok, err := s.peek()
	if err != nil {
		return err
	}
	if ok && b == '.' {
		s.next()
		if err := s.digits(1); err != nil {
			return err
		}
		if b, ok, err = s.peek(); err != nil {
			return err
		}
	}
	if ok && (b == 'e' || b == 'E') {
		s.next()
		if b, ok, err = s.peek(); err != nil {
			return err
		}
		if ok && (b == '+' || b == '-') {
			s.next()
		}
		return s.digits(1)
	}
	return nil
}

// object checks an object after its "{", through its "}", at depth.
func (s *scanner) object(depth int) error {
	if depth > maxDepth {
		return errManifestDepth
	}
	c, err := s.ws()
	if err != nil || c == '}' {
		return err
	}
	for {
		if c != '"' {
			return errManifestShape
		}
		if depth == s.countAt {
			s.members++
		}
		if err := s.str(); err != nil {
			return err
		}
		if c, err = s.colon(); err != nil {
			return err
		}
		if err := s.value(c, depth); err != nil {
			return err
		}
		var done bool
		if c, done, err = s.more('}'); err != nil || done {
			return err
		}
	}
}

// array checks an array after its "[", through its "]", at depth.
func (s *scanner) array(depth int) error {
	if depth > maxDepth {
		return errManifestDepth
	}
	c, err := s.ws()
	if err != nil || c == ']' {
		return err
	}
	for {
		if err := s.value(c, depth); err != nil {
			return err
		}
		var done bool
		if c, done, err = s.more(']'); err != nil || done {
			return err
		}
	}
}

// colon reads the ":" after a name and returns the first byte of the value after it.
func (s *scanner) colon() (byte, error) {
	c, err := s.ws()
	if err != nil {
		return 0, err
	}
	if c != ':' {
		return 0, errManifestShape
	}
	return s.ws()
}

// more reads what follows a member or an element: close (done is true), or "," and the first byte
// of the next one (returned), which may not be close.
func (s *scanner) more(close byte) (next byte, done bool, err error) {
	c, err := s.ws()
	if err != nil {
		return 0, false, err
	}
	switch c {
	case close:
		return 0, true, nil
	case ',':
		next, err = s.ws()
		return next, false, err
	}
	return 0, false, errManifestShape
}

// hold checks the value whose first byte c was read at depth, keeping its bytes up to max; over
// reports a value longer than max (its bytes past max are checked, not kept).
func (s *scanner) hold(c byte, depth, max int) (held []byte, over bool, err error) {
	s.holding, s.held, s.heldMax, s.over = true, append(s.held[:0], c), max, false
	err = s.value(c, depth)
	s.holding = false
	return s.held, s.over, err
}

// scanManifest walks the manifest r (at most manifestCap bytes are read) and returns the archive's
// kind, as a LoadFrom of the same bytes would give it (kindFrom over its stores), or why it is not
// a manifest this build reads.
func scanManifest(r io.Reader) (string, error) {
	_, kind, err := scanStores(r)
	return kind, err
}

// scanStores is scanManifest with the stores it kept (nil when it is not a manifest this
// build reads), so the Outlook-only verdict is drawn from the same walk.
func scanStores(r io.Reader) (map[string]string, string, error) {
	src := &capReader{r: r}
	s := &scanner{r: bufio.NewReaderSize(src, 64<<10)}
	c, err := s.wsOrEOF()
	switch {
	case err == io.EOF && src.n == 0:
		return nil, kindFrom(nil), nil // a zero-byte manifest: LoadFrom reads an empty one
	case err == io.EOF:
		return nil, "", errManifestShape // white space only does not parse
	case err != nil:
		return nil, "", err
	}
	var stores map[string]string
	version := 0
	switch c {
	case 'n': // null: LoadFrom reads an empty manifest
		if err := s.lit("ull"); err != nil {
			return nil, "", err
		}
	case '{':
		if stores, version, err = s.manifest(); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", errManifestShape
	}
	// Nothing but white space may follow (json.Unmarshal's rule).
	if _, err := s.wsOrEOF(); err != io.EOF {
		if err == nil {
			err = errManifestShape
		}
		return nil, "", err
	}
	if src.n > manifestCap {
		return nil, "", errManifestTooLarge
	}
	if version > state.ManifestVersion {
		return nil, "", &state.NewerFormatError{Name: manifestName, Version: version, Understood: state.ManifestVersion}
	}
	return stores, kindFrom(stores), nil
}

// outlookFrom reports an archive every one of whose stores, read as kindFrom reads them, is
// an Outlook data file (a .pst or .ost, or -outlook's own export): none of its mail has an
// original message to go back into Thunderbird (Restore to Thunderbird's W-PST; friction
// P4, settled 2026-11-07). No store at all is not Outlook-only.
func outlookFrom(stores map[string]string) bool {
	n := 0
	for id, tok := range stores {
		if !state.SafeToken(tok) {
			continue
		}
		switch state.SourceKind(state.SnapshotStoreID(id)) {
		case state.SourcePST, state.SourceOutlookScratch:
			n++
		default:
			return false
		}
	}
	return n > 0
}

// manifest walks the manifest object's members after its "{", through its "}". Names match as
// json.Unmarshal matches them (case folded; the last of a repeated name wins, and a repeated
// "stores" object adds to the one before, as Unmarshal fills a map).
func (s *scanner) manifest() (stores map[string]string, version int, err error) {
	used := 0
	c, err := s.ws()
	if err != nil || c == '}' {
		return nil, 0, err
	}
	for {
		if c != '"' {
			return nil, 0, errManifestShape
		}
		raw, over, err := s.hold(c, 1, maxHeldBytes)
		if err != nil {
			return nil, 0, err
		}
		var name string
		if !over {
			if err := json.Unmarshal(raw, &name); err != nil {
				return nil, 0, errManifestShape
			}
		}
		if c, err = s.colon(); err != nil {
			return nil, 0, err
		}
		switch {
		case strings.EqualFold(name, "version"):
			raw, over, err := s.hold(c, 1, maxHeldBytes)
			if err != nil {
				return nil, 0, err
			}
			if over || json.Unmarshal(raw, &version) != nil {
				return nil, 0, errManifestShape
			}
		case strings.EqualFold(name, "stores"):
			if stores, used, err = s.stores(c, stores, used); err != nil {
				return nil, 0, err
			}
		case strings.EqualFold(name, "entries"), strings.EqualFold(name, "capture_gaps"):
			if err := s.records(c); err != nil {
				return nil, 0, err
			}
		case strings.EqualFold(name, "collapsed_tokens"):
			if err := s.flags(c); err != nil {
				return nil, 0, err
			}
		default:
			if err := s.value(c, 1); err != nil {
				return nil, 0, err
			}
		}
		var done bool
		if c, done, err = s.more('}'); err != nil {
			return nil, 0, err
		}
		if done {
			return stores, version, nil
		}
	}
}

// stores reads a "stores" value whose first byte c was read, into stores as json.Unmarshal fills
// the manifest's map[string]string (null empties it; a value that is not a string or null
// refuses), within maxStoresBytes counted with what was kept before (used).
func (s *scanner) stores(c byte, stores map[string]string, used int) (map[string]string, int, error) {
	budget := maxStoresBytes - used
	if budget < 0 {
		budget = 0
	}
	s.countAt, s.members = 2, 0
	raw, over, err := s.hold(c, 1, budget)
	s.countAt = 0
	if err != nil {
		return nil, 0, err
	}
	if c == 'n' {
		return nil, 0, nil // null: LoadFrom's map is emptied
	}
	cost := len(raw) + s.members*storeEntryCost
	if over || cost > budget {
		return nil, 0, errManifestStores
	}
	if err := json.Unmarshal(raw, &stores); err != nil {
		return nil, 0, errManifestShape
	}
	return stores, used + cost, nil
}

// records checks an "entries" or a "capture_gaps" value whose first byte c was read without
// keeping it: null, or an object whose values are records or gaps (objects, or null). What a
// record or a gap holds is not checked (MA-1034's not-checked list).
func (s *scanner) records(c byte) error {
	return s.mapOf(c, func(c byte) error {
		switch c {
		case 'n':
			return s.lit("ull")
		case '{':
			return s.object(3)
		}
		return errManifestShape
	})
}

// flags checks a "collapsed_tokens" value whose first byte c was read without keeping it: null, or
// an object whose values are true, false or null, as json.Unmarshal fills the manifest's
// map[string]bool (MA-1035, amended: a value of another type refuses there, so here too).
func (s *scanner) flags(c byte) error {
	return s.mapOf(c, func(c byte) error {
		switch c {
		case 't':
			return s.lit("rue")
		case 'f':
			return s.lit("alse")
		case 'n':
			return s.lit("ull")
		}
		return errManifestShape
	})
}

// mapOf checks a top-level value whose first byte c was read, that the manifest reads into a map:
// null, or an object each of whose values, its first byte read, member checks.
func (s *scanner) mapOf(c byte, member func(c byte) error) error {
	switch c {
	case 'n':
		return s.lit("ull")
	case '{':
	default:
		return errManifestShape
	}
	c, err := s.ws()
	if err != nil || c == '}' {
		return err
	}
	for {
		if c != '"' {
			return errManifestShape
		}
		if err := s.str(); err != nil {
			return err
		}
		if c, err = s.colon(); err != nil {
			return err
		}
		if err := member(c); err != nil {
			return err
		}
		var done bool
		if c, done, err = s.more('}'); err != nil || done {
			return err
		}
	}
}

// kindFrom is the kind of an archive whose manifest's "stores" are stores, as LoadFrom keeps them:
// a token that is not one safe path segment is dropped, and each id is read lexically
// (state.SnapshotStoreID). A store id "mailbox:…" is a Microsoft 365 source, anything else a
// local one.
func kindFrom(stores map[string]string) string {
	var local, cloud bool
	for id, tok := range stores {
		if !state.SafeToken(tok) {
			continue
		}
		if strings.HasPrefix(state.SnapshotStoreID(id), "mailbox:") {
			cloud = true
		} else {
			local = true
		}
	}
	switch {
	case local && cloud:
		return "mixed"
	case local:
		return "local"
	case cloud:
		return "m365"
	default:
		return "empty"
	}
}

// recognised keeps a folder's verdict between page loads (the Desktop asks every row on every
// page load): by the folder's identity and the manifest's identity, size and modification time,
// so a manifest that changed — rewritten, replaced, grown — is read again.
type recognised struct {
	dir, man os.FileInfo
	kind     string
	ok       bool
	outlook  bool // every store an Outlook data file (outlookFrom)
}

// recognisedMax bounds what is kept: far more archives than a person keeps.
const recognisedMax = 512

var memo = struct {
	sync.Mutex
	m map[string]recognised
}{m: map[string]recognised{}}

// remembered is folder's kept verdict when dir and man are what it was read from.
func remembered(folder string, dir, man os.FileInfo) (recognised, bool) {
	memo.Lock()
	defer memo.Unlock()
	r, ok := memo.m[folder]
	if !ok || !os.SameFile(r.dir, dir) || !os.SameFile(r.man, man) || r.man.Size() != man.Size() || !r.man.ModTime().Equal(man.ModTime()) {
		return recognised{}, false
	}
	return r, true
}

func remember(folder string, r recognised) {
	memo.Lock()
	defer memo.Unlock()
	if len(memo.m) >= recognisedMax {
		memo.m = map[string]recognised{}
	}
	memo.m[folder] = r
}

// walking holds, per folder, the walk of its manifest under way, so that reads of the same folder
// at once (two page loads) walk it once: the later ones wait, then take the kept verdict (MA-1035).
var walking = struct {
	sync.Mutex
	m map[string]chan struct{}
}{m: map[string]chan struct{}{}}

// walkAlone waits until no other walk of folder is under way and marks this one; done ends it.
func walkAlone(folder string) (done func()) {
	for {
		walking.Lock()
		ch, busy := walking.m[folder]
		if !busy {
			ch = make(chan struct{})
			walking.m[folder] = ch
			walking.Unlock()
			return func() {
				walking.Lock()
				delete(walking.m, folder)
				walking.Unlock()
				close(ch)
			}
		}
		walking.Unlock()
		<-ch
	}
}

// readError reports whether err is a failure to read the file (not a verdict on its bytes).
func readError(err error) bool {
	var re *errReadManifest
	return errors.As(err, &re)
}
