package leak

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

// Hit is one marker found: where (a path under the scanned root, with "!entry" for
// a member of a zip or gzip file), in the name or the contents, which field, and as
// which encoding.
type Hit struct {
	Where    string
	InName   bool
	Field    Field
	Encoding string
}

func (h Hit) String() string {
	place := "contents"
	if h.InName {
		place = "name"
	}
	return fmt.Sprintf("%s (%s): %s as %s", h.Where, place, h.Field, h.Encoding)
}

// needle is one encoded form of a marker.
type needle struct {
	encoding string
	b        []byte
	stripped bool // searched in the whitespace-stripped view (base64 may be wrapped)
}

// needles is every encoded form of m a file could hold it in: as is, upper-cased,
// UTF-16 (both byte orders), hex (both cases), and base64 at each of the three byte
// alignments a marker can start at (the edge characters that depend on the
// neighbouring bytes are trimmed). Markers are ASCII letters, so quoted-printable,
// HTML entities, JSON escapes and percent-encoding leave them as they are: the
// plain needle covers those.
func needles(m string) []needle {
	ns := []needle{
		{encoding: "plain", b: []byte(m)},
		{encoding: "upper-case", b: []byte(strings.ToUpper(m))},
		{encoding: "hex", b: []byte(hex.EncodeToString([]byte(m)))},
		{encoding: "HEX", b: []byte(strings.ToUpper(hex.EncodeToString([]byte(m))))},
	}
	u := utf16.Encode([]rune(m))
	le, be := make([]byte, 2*len(u)), make([]byte, 2*len(u))
	for i, r := range u {
		binary.LittleEndian.PutUint16(le[2*i:], r)
		binary.BigEndian.PutUint16(be[2*i:], r)
	}
	ns = append(ns, needle{encoding: "utf-16le", b: le}, needle{encoding: "utf-16be", b: be})
	for pad := 0; pad < 3; pad++ {
		enc := base64.StdEncoding.EncodeToString(append(bytes.Repeat([]byte{0}, pad), m...))
		enc = strings.TrimRight(enc, "=")
		// The first ceil(pad*4/3) characters mix the padding bytes, the last one may mix
		// what follows the marker: keep only the characters that are the marker's alone.
		start := (pad*4 + 2) / 3
		if len(enc)-1 > start {
			ns = append(ns, needle{encoding: fmt.Sprintf("base64(+%d)", pad), b: []byte(enc[start : len(enc)-1]), stripped: true})
		}
	}
	return ns
}

// ScanBytes is every marker of ms in b, attributed to where.
func ScanBytes(where string, b []byte, ms Markers) []Hit {
	stripped := bytes.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, b)
	var hits []Hit
	for _, f := range Fields {
		m, ok := ms[f]
		if !ok {
			continue
		}
		for _, n := range needles(m) {
			hay := b
			if n.stripped {
				hay = stripped
			}
			if bytes.Contains(hay, n.b) {
				hits = append(hits, Hit{Where: where, Field: f, Encoding: n.encoding})
				break
			}
		}
	}
	return hits
}

// ScanTree is every marker of ms under root: in each entry's name (relative to
// root), in each regular file's contents, and inside every member of a zip file and
// in the decompressed contents of a gzip file (deflate hides even literal bytes). It
// walks with Lstat, never follows a link, and fails on an unreadable entry rather
// than skipping it.
func ScanTree(root string, ms Markers) ([]Hit, error) {
	var hits []Hit
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		for _, h := range ScanBytes(rel, []byte(rel), ms) {
			h.InName = true
			hits = append(hits, h)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		hits = append(hits, scanContents(rel, b, ms)...)
		return nil
	})
	sort.Slice(hits, func(i, j int) bool { return hits[i].String() < hits[j].String() })
	return hits, err
}

// scanContents scans b and, for a zip or gzip file, what it holds.
func scanContents(where string, b []byte, ms Markers) []Hit {
	hits := ScanBytes(where, b, ms)
	switch {
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		if zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b))); err == nil {
			for _, f := range zr.File {
				inner := where + "!" + f.Name
				for _, h := range ScanBytes(inner, []byte(f.Name), ms) {
					h.InName = true
					hits = append(hits, h)
				}
				if rc, err := f.Open(); err == nil {
					data, _ := io.ReadAll(rc)
					rc.Close()
					hits = append(hits, scanContents(inner, data, ms)...)
				}
			}
		}
	case bytes.HasPrefix(b, []byte{0x1f, 0x8b}):
		if gr, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
			data, _ := io.ReadAll(gr)
			hits = append(hits, scanContents(where+"!gzip", data, ms)...)
		}
	}
	return hits
}

// Fields of hits, without repeats.
func FieldsOf(hits []Hit) map[Field]bool {
	got := map[Field]bool{}
	for _, h := range hits {
		got[h.Field] = true
	}
	return got
}
