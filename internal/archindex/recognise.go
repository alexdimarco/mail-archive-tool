package archindex

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"mail-archive-tool/internal/state"
)

// Recognition of a plain archive (SD-2, amended 2026-11-02 by MA-1034, the 1.5.3 re-walk's N1).
// Recognition used to read at most 1 MiB of the manifest and parse it whole with state.LoadFrom,
// so an archive of more than about 2,200 messages was "not an archive": greyed with no Switch in
// Your archives, refused by Open an archive folder… and by add-archive. It now walks the
// manifest's JSON tokens as they stream in, under bounds that keep both the read and the memory
// small whatever the file holds:
//
//   - manifestCap bounds the bytes read (archdelete's bound for real archives);
//   - maxValueBytes bounds the one JSON value held at a time (a string, a number, the version);
//   - maxStoresBytes bounds the store ids and tokens kept to tell the kind;
//   - maxDepth bounds nesting.
//
// It checks what makes the file a manifest this build can read — one JSON object (or null, or
// nothing at all, as LoadFrom reads them), a version no newer than this build's, a "stores"
// object of strings, an "entries" object of records — and keeps only "stores", whose ids are read
// lexically (state.SnapshotStoreID: never resolved on disk, SD-1). The records themselves are
// skipped, never kept: each reader that uses them (status, a backup, a delete's plan) reads the
// whole manifest itself and refuses what it cannot use.

// manifestCap bounds recognition's read of a manifest: sized for real archives, as a delete's
// manifest read is (archdelete's manifestCap, "sized for real archives"). A var only so a test can
// stand in for a manifest over it without writing 1 GiB.
var manifestCap int64 = 1 << 30

const (
	// maxValueBytes bounds the bytes of one JSON value the walk holds at once: far over any
	// subject, path or id a real manifest holds, and what the whole manifest was allowed before.
	maxValueBytes = 1 << 20
	// maxStoresBytes bounds the store ids and tokens kept to tell the kind: a real archive has a
	// handful of stores.
	maxStoresBytes = 1 << 20
	// maxDepth bounds nesting: a real manifest nests five or six deep.
	maxDepth = 64
)

// Why a manifest is not recognised. errReadManifest wraps a failure to read the file, which may
// pass next time (a sharing violation, an I/O error), so its verdict is not kept.
var (
	errManifestTooLarge = errors.New("manifest over its cap")
	errManifestValue    = errors.New("manifest holds a value over its cap")
	errManifestDepth    = errors.New("manifest nests too deep")
	errManifestShape    = errors.New("manifest is not a manifest object")
	errManifestStores   = errors.New("manifest's stores are over their cap")
)

type errReadManifest struct{ err error }

func (e *errReadManifest) Error() string { return e.err.Error() }
func (e *errReadManifest) Unwrap() error { return e.err }

// manifestSource is the manifest as the walk's decoder reads it: it counts the bytes and refuses
// to read more while the decoder holds over maxValueBytes it has not yet given back as a token,
// so one huge string or number cannot fill memory.
type manifestSource struct {
	r   io.Reader
	n   int64
	dec *json.Decoder
}

func (s *manifestSource) Read(p []byte) (int, error) {
	if s.dec != nil {
		// What the decoder holds and has not given back is at most one value and the white
		// space before it; it may grow to one byte over the bound, which then refuses.
		room := maxValueBytes + 1 - (s.n - s.dec.InputOffset())
		if room <= 0 {
			return 0, errManifestValue
		}
		if int64(len(p)) > room {
			p = p[:room]
		}
	}
	n, err := s.r.Read(p)
	s.n += int64(n)
	if err != nil && err != io.EOF {
		return n, &errReadManifest{err}
	}
	return n, err
}

// scanManifest walks the manifest r (at most manifestCap bytes are read) and returns the archive's
// kind, as a LoadFrom of the same bytes would give it (kindFrom over its stores), or why it is not a manifest
// this build reads.
func scanManifest(r io.Reader) (string, error) {
	src := &manifestSource{r: io.LimitReader(r, manifestCap+1)}
	dec := json.NewDecoder(src)
	src.dec = dec
	tok, err := dec.Token()
	switch {
	case err == io.EOF && src.n == 0:
		return kindFrom(nil), nil // a zero-byte manifest: LoadFrom reads an empty one
	case err == io.EOF:
		return "", errManifestShape // white space only does not parse
	case err != nil:
		return "", err
	}
	var stores map[string]string
	version := 0
	switch tok {
	case nil: // null: LoadFrom reads an empty manifest
	case json.Delim('{'):
		stores, version, err = walkManifest(dec)
		if err != nil {
			return "", err
		}
	default:
		return "", errManifestShape
	}
	// Nothing but white space may follow (json.Unmarshal's rule).
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			err = errManifestShape
		}
		return "", err
	}
	if src.n > manifestCap {
		return "", errManifestTooLarge
	}
	if version > state.ManifestVersion {
		return "", &state.NewerFormatError{Name: manifestName, Version: version, Understood: state.ManifestVersion}
	}
	return kindFrom(stores), nil
}

// walkManifest walks the manifest object's members after its "{", through its "}". Names match as
// json.Unmarshal matches them (case folded; the last of a repeated name wins, and a repeated
// "stores" object adds to the one before, as Unmarshal fills a map).
func walkManifest(dec *json.Decoder) (stores map[string]string, version int, err error) {
	storesBytes := 0
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, 0, err
		}
		name, _ := t.(string)
		switch {
		case strings.EqualFold(name, "version"):
			if err := dec.Decode(&version); err != nil {
				return nil, 0, err
			}
		case strings.EqualFold(name, "stores"):
			stores, storesBytes, err = walkStores(dec, stores, storesBytes)
			if err != nil {
				return nil, 0, err
			}
		case strings.EqualFold(name, "entries"):
			if err := skipEntries(dec); err != nil {
				return nil, 0, err
			}
		default:
			if err := skipValue(dec, 1); err != nil {
				return nil, 0, err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // the closing "}"
		return nil, 0, err
	}
	return stores, version, nil
}

// walkStores reads the "stores" value: null (no stores) or an object whose values are strings
// (or null), kept within maxStoresBytes.
func walkStores(dec *json.Decoder, stores map[string]string, used int) (map[string]string, int, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, 0, err
	}
	switch t {
	case nil:
		return nil, 0, nil
	case json.Delim('{'):
	default:
		return nil, 0, errManifestShape
	}
	if stores == nil {
		stores = map[string]string{}
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, 0, err
		}
		id, _ := kt.(string)
		vt, err := dec.Token()
		if err != nil {
			return nil, 0, err
		}
		var tok string
		switch v := vt.(type) {
		case nil:
		case string:
			tok = v
		default:
			return nil, 0, errManifestShape
		}
		if used += len(id) + len(tok); used > maxStoresBytes {
			return nil, 0, errManifestStores
		}
		stores[id] = tok
	}
	if _, err := dec.Token(); err != nil {
		return nil, 0, err
	}
	return stores, used, nil
}

// skipEntries skips the "entries" value without keeping it: null, or an object whose values are
// records (objects, or null).
func skipEntries(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	switch t {
	case nil:
		return nil
	case json.Delim('{'):
	default:
		return errManifestShape
	}
	for dec.More() {
		if _, err := dec.Token(); err != nil { // the record's key
			return err
		}
		vt, err := dec.Token()
		if err != nil {
			return err
		}
		switch vt {
		case nil:
		case json.Delim('{'):
			if err := skipFrom(dec, vt, 2); err != nil {
				return err
			}
		default:
			return errManifestShape
		}
	}
	_, err = dec.Token()
	return err
}

// skipValue skips one value at depth (how many objects and arrays hold it), keeping only its
// nesting.
func skipValue(dec *json.Decoder, depth int) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	return skipFrom(dec, t, depth)
}

// skipFrom is skipValue for a value whose first token t was read already.
func skipFrom(dec *json.Decoder, t json.Token, depth int) error {
	open := 0
	for {
		if d, ok := t.(json.Delim); ok {
			switch d {
			case '{', '[':
				open++
				if depth+open > maxDepth {
					return errManifestDepth
				}
			case '}', ']':
				open--
			}
		}
		if open == 0 {
			return nil
		}
		var err error
		if t, err = dec.Token(); err != nil {
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

// readError reports whether err is a failure to read the file (not a verdict on its bytes).
func readError(err error) bool {
	var re *errReadManifest
	return errors.As(err, &re)
}
