package container

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The keyless inventory (A17, A22): a top file beside the pointer, and immutable
// generation-named shards under inventory/ (§3).
const (
	InventoryName = ".mailarchive-inventory"
	InventoryDir  = "inventory"
	inventoryHead = "mailarchive-inventory"
	// InventoryVersion is the inventory format this MailArchive writes and reads.
	InventoryVersion = 1
	MinFanout        = 2
	MaxFanout        = 4
	maxShards        = 1 << 16
	maxShardLines    = 1 << 20
)

// ErrInventory refuses an inventory that breaks the format's rules. A newer
// version is ErrNewerFormat instead.
var ErrInventory = errors.New("container: malformed inventory")

// FileState says whether the pointer the inventory was written for names a file
// (Live) or whether it was superseded during the run that wrote it and is
// removed by the next run's start sweep (Retained) (P2b-C9).
type FileState uint8

const (
	Live FileState = iota + 1
	Retained
)

func (s FileState) String() string {
	switch s {
	case Live:
		return "live"
	case Retained:
		return "retained"
	}
	return "unknown"
}

// Digest is a file's ciphertext SHA-256 and size.
type Digest struct {
	SHA256 [32]byte
	Size   int64
}

// File is one line of a shard: an archive-relative path under units/ or chunks/.
type File struct {
	Path string
	Digest
	State FileState
}

// ShardRef is one line of the top file: the shard holding the files whose name
// starts with Prefix, its file name, its digest and its line count.
type ShardRef struct {
	Prefix  string
	Name    string
	SHA256  [32]byte
	Entries int
}

// Top is the inventory's top file: the generation it was written for, the
// digests of the pointer and its mirror (nil: the mirror's write failed), the
// fan-out and the shards.
type Top struct {
	Gen     uint64
	Pointer Digest
	Mirror  *Digest
	Fanout  int
	Shards  []ShardRef
}

// Shard lists, sorted by path, every file whose name starts with its prefix.
type Shard struct {
	Prefix string
	Files  []File
}

// ShardName is the immutable file name of the shard for prefix written at gen.
func ShardName(prefix string, gen uint64) string {
	return InventoryDir + "/" + prefix + "." + strconv.FormatUint(gen, 10)
}

// filePath is a unit or chunk path: units/<hh>/<32 hex> or chunks/<hh>/<32 hex>.
var filePath = regexp.MustCompile(`^(units|chunks)/([0-9a-f]{2})/([0-9a-f]{32})$`)

var shardName = regexp.MustCompile(`^inventory/([0-9a-f]{2,4})\.([1-9][0-9]*)$`)

// PrefixOf is the shard prefix, at fan-out d, of a unit or chunk path.
func PrefixOf(path string, d int) (string, error) {
	m := filePath.FindStringSubmatch(path)
	if m == nil || m[2] != m[3][:2] {
		return "", fmt.Errorf("%w: %q is not a unit or chunk path", ErrInventory, path)
	}
	if d < MinFanout || d > MaxFanout {
		return "", fmt.Errorf("%w: fan-out %d", ErrInventory, d)
	}
	return m[3][:d], nil
}

func hexPrefix(s string, d int) bool {
	if len(s) != d {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Marshal is the top file's bytes. It refuses a top file a reader would refuse.
func (t Top) Marshal() ([]byte, error) {
	if err := t.check(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %d\n", inventoryHead, InventoryVersion)
	fmt.Fprintf(&b, "gen %d\n", t.Gen)
	fmt.Fprintf(&b, "pointer %x %d\n", t.Pointer.SHA256, t.Pointer.Size)
	if t.Mirror == nil {
		b.WriteString("mirror none\n")
	} else {
		fmt.Fprintf(&b, "mirror %x %d\n", t.Mirror.SHA256, t.Mirror.Size)
	}
	fmt.Fprintf(&b, "fanout %d\n", t.Fanout)
	for _, s := range t.Shards {
		fmt.Fprintf(&b, "%s %s %x %d\n", s.Prefix, s.Name, s.SHA256, s.Entries)
	}
	return b.Bytes(), nil
}

// ParseTop strictly decodes a top file. The version line is read first: a newer
// version is ErrNewerFormat, never "damaged" (P2b-C11).
func ParseTop(raw []byte) (Top, error) {
	lines, err := splitLines(raw, maxShards+5)
	if err != nil {
		return Top{}, err
	}
	bad := func(why string) (Top, error) { return Top{}, fmt.Errorf("%w: top file: %s", ErrInventory, why) }
	if len(lines) == 0 {
		return bad("empty")
	}
	f := strings.Fields(lines[0])
	if len(f) != 2 || f[0] != inventoryHead {
		return bad("not an inventory")
	}
	v, err := strconv.Atoi(f[1])
	if err != nil || v < 1 {
		return bad("version")
	}
	if v > InventoryVersion {
		return Top{}, fmt.Errorf("%w (inventory version %d, this MailArchive reads %d)", ErrNewerFormat, v, InventoryVersion)
	}
	if len(lines) < 5 {
		return bad("truncated")
	}
	var t Top
	if t.Gen, err = keyedUint(lines[1], "gen"); err != nil {
		return bad(err.Error())
	}
	if t.Pointer, err = keyedDigest(lines[2], "pointer"); err != nil {
		return bad(err.Error())
	}
	if lines[3] == "mirror none" {
		t.Mirror = nil
	} else {
		d, err := keyedDigest(lines[3], "mirror")
		if err != nil {
			return bad(err.Error())
		}
		t.Mirror = &d
	}
	fan, err := keyedUint(lines[4], "fanout")
	if err != nil {
		return bad(err.Error())
	}
	t.Fanout = int(fan)
	for _, l := range lines[5:] {
		f := strings.Fields(l)
		if len(f) != 4 {
			return bad("shard line " + strconv.Quote(l))
		}
		sum, err := hex32(f[2])
		if err != nil {
			return bad("shard digest")
		}
		n, err := strconv.Atoi(f[3])
		if err != nil {
			return bad("shard entries")
		}
		t.Shards = append(t.Shards, ShardRef{Prefix: f[0], Name: f[1], SHA256: sum, Entries: n})
	}
	if err := t.check(); err != nil {
		return Top{}, err
	}
	again, _ := t.Marshal()
	if !bytes.Equal(again, raw) {
		return bad("not in canonical form")
	}
	return t, nil
}

func (t Top) check() error {
	bad := func(why string) error { return fmt.Errorf("%w: top file: %s", ErrInventory, why) }
	if t.Gen == 0 {
		return bad("generation 0")
	}
	if t.Pointer.Size <= 0 || t.Pointer.Size > MaxPointerSize || (t.Mirror != nil && (t.Mirror.Size <= 0 || t.Mirror.Size > MaxPointerSize)) {
		return bad("pointer size")
	}
	if t.Fanout < MinFanout || t.Fanout > MaxFanout {
		return bad("fan-out " + strconv.Itoa(t.Fanout))
	}
	if len(t.Shards) > maxShards {
		return bad("too many shards")
	}
	for j, s := range t.Shards {
		m := shardName.FindStringSubmatch(s.Name)
		switch {
		case !hexPrefix(s.Prefix, t.Fanout):
			return bad("shard prefix " + strconv.Quote(s.Prefix))
		case j > 0 && s.Prefix <= t.Shards[j-1].Prefix:
			return bad("shards out of order at " + s.Prefix)
		case m == nil || m[1] != s.Prefix:
			return bad("shard name " + strconv.Quote(s.Name))
		case s.Entries < 1 || s.Entries > maxShardLines:
			return bad("shard entries")
		}
		if g, _ := strconv.ParseUint(m[2], 10, 64); g > t.Gen {
			return bad("shard " + s.Name + " is newer than the inventory")
		}
	}
	return nil
}

// Marshal is a shard's bytes. It refuses a shard a reader would refuse.
func (s Shard) Marshal() ([]byte, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	for _, f := range s.Files {
		fmt.Fprintf(&b, "%s %x %d %s\n", f.Path, f.SHA256, f.Size, f.State)
	}
	return b.Bytes(), nil
}

// ParseShard strictly decodes the shard for prefix.
func ParseShard(prefix string, raw []byte) (Shard, error) {
	lines, err := splitLines(raw, maxShardLines)
	if err != nil {
		return Shard{}, err
	}
	s := Shard{Prefix: prefix}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) != 4 {
			return Shard{}, fmt.Errorf("%w: shard %s: line %q", ErrInventory, prefix, l)
		}
		sum, err := hex32(f[1])
		if err != nil {
			return Shard{}, fmt.Errorf("%w: shard %s: digest", ErrInventory, prefix)
		}
		size, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			return Shard{}, fmt.Errorf("%w: shard %s: size", ErrInventory, prefix)
		}
		var st FileState
		switch f[3] {
		case "live":
			st = Live
		case "retained":
			st = Retained
		default:
			return Shard{}, fmt.Errorf("%w: shard %s: state %q", ErrInventory, prefix, f[3])
		}
		s.Files = append(s.Files, File{Path: f[0], Digest: Digest{SHA256: sum, Size: size}, State: st})
	}
	if err := s.check(); err != nil {
		return Shard{}, err
	}
	again, _ := s.Marshal()
	if !bytes.Equal(again, raw) {
		return Shard{}, fmt.Errorf("%w: shard %s: not in canonical form", ErrInventory, prefix)
	}
	return s, nil
}

func (s Shard) check() error {
	if len(s.Prefix) < MinFanout || len(s.Prefix) > MaxFanout || !hexPrefix(s.Prefix, len(s.Prefix)) {
		return fmt.Errorf("%w: shard prefix %q", ErrInventory, s.Prefix)
	}
	if len(s.Files) == 0 || len(s.Files) > maxShardLines {
		return fmt.Errorf("%w: shard %s holds %d files", ErrInventory, s.Prefix, len(s.Files))
	}
	for j, f := range s.Files {
		p, err := PrefixOf(f.Path, len(s.Prefix))
		switch {
		case err != nil:
			return err
		case p != s.Prefix:
			return fmt.Errorf("%w: %s does not belong in shard %s", ErrInventory, f.Path, s.Prefix)
		case j > 0 && f.Path <= s.Files[j-1].Path:
			return fmt.Errorf("%w: shard %s out of order at %s", ErrInventory, s.Prefix, f.Path)
		case f.Size <= 0:
			return fmt.Errorf("%w: %s has size %d", ErrInventory, f.Path, f.Size)
		case f.State != Live && f.State != Retained:
			return fmt.Errorf("%w: %s has no state", ErrInventory, f.Path)
		}
	}
	return nil
}

// splitLines splits raw into lines, refusing more than max. Line endings, empty
// lines and letter case need no check here: every parser re-marshals what it
// read and refuses anything that is not byte-for-byte its canonical form.
func splitLines(raw []byte, max int) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 4096), 4096)
	for sc.Scan() {
		if len(lines) == max {
			return nil, fmt.Errorf("%w: more than %d lines", ErrInventory, max)
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInventory, err)
	}
	return lines, nil
}

func keyedUint(line, key string) (uint64, error) {
	f := strings.Fields(line)
	if len(f) != 2 || f[0] != key {
		return 0, fmt.Errorf("want %q", key)
	}
	return strconv.ParseUint(f[1], 10, 64)
}

func keyedDigest(line, key string) (Digest, error) {
	f := strings.Fields(line)
	if len(f) != 3 || f[0] != key {
		return Digest{}, fmt.Errorf("want %q", key)
	}
	sum, err := hex32(f[1])
	if err != nil {
		return Digest{}, err
	}
	n, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil {
		return Digest{}, err
	}
	return Digest{SHA256: sum, Size: n}, nil
}

func hex32(s string) ([32]byte, error) {
	var out [32]byte
	if len(s) != 64 {
		return out, fmt.Errorf("digest %q", s)
	}
	_, err := hex.Decode(out[:], []byte(s))
	return out, err
}
