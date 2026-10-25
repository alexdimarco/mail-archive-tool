// Package container holds the on-disk formats of an encrypted archive's sealed
// record (Phase 2b): the root pointer that commits the manifest, history and
// index containers by one rename, and the keyless inventory beside it
// (design-encrypted-archives-phase2b §1, §3). The codecs here are pure: they
// encode and strictly decode bytes and touch no file.
package container

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"mail-archive-tool/internal/vault"
)

// The root pointer and its mirror (A21), and the folder holding every chunk.
const (
	PointerName = ".mailarchive-manifest.ptr"
	MirrorName  = ".mailarchive-manifest.ptr.bak"
	ChunksDir   = "chunks"
)

// Bounds a reader enforces and a writer refuses to exceed (P2b-C18).
const (
	MaxPointerSize = 64 << 20
	MaxChunkSize   = 64 << 20
	maxEntries     = 1 << 20
	maxHeadSize    = 4096
)

const (
	pointerMagic = "MAPF"
	fileHeadSize = len(pointerMagic) + 2 + 8 // magic ‖ file_format u16 ‖ gen u64
	entrySize    = 64                        // i u32 ‖ name [16] ‖ gen u64 ‖ sha256 [32] ‖ size u32
)

var (
	// ErrNotPointer refuses bytes that are not a root pointer file at all.
	ErrNotPointer = errors.New("container: not a MailArchive record pointer")
	// ErrNewerFormat refuses a file written by a newer MailArchive (P2b-C11):
	// the remedy is to upgrade, never to treat it as damaged or absent.
	ErrNewerFormat = errors.New("container: written by a newer MailArchive — upgrade MailArchive to open it")
	// ErrPointer refuses a pointer that authenticates but breaks the format's rules.
	ErrPointer = errors.New("container: malformed record pointer")
)

// Name is a chunk file's opaque name: 128 random bits, drawn fresh for every
// chunk written and recorded by the pointer.
type Name [16]byte

// NewName draws a fresh chunk name.
func NewName() (Name, error) {
	var n Name
	if _, err := io.ReadFull(rand.Reader, n[:]); err != nil {
		return Name{}, fmt.Errorf("container: draw chunk name: %w", err)
	}
	return n, nil
}

func (n Name) String() string { return hex.EncodeToString(n[:]) }

// Path is the chunk's archive-relative path, chunks/<hh>/<32 hex>.
func (n Name) Path() string {
	h := n.String()
	return ChunksDir + "/" + h[:2] + "/" + h
}

// Entry is one chunk a pointer names: its position in its section, its file
// name, the generation it was sealed at (A2 refined), and its ciphertext digest
// and size.
type Entry struct {
	I      uint32
	Name   Name
	Gen    uint64
	SHA256 [32]byte
	Size   uint32
}

// Head is the pointer's small JSON head.
type Head struct {
	Format uint16 `json:"format"`
	Gen    uint64 `json:"gen"`
	// RunStart is the start (Unix seconds) of the run that sealed this pointer.
	RunStart int64 `json:"run_start"`
	// Inv is the generation of the last inventory this writer saw become
	// durable before it staged this pointer (P2b-C7).
	Inv uint64 `json:"inv"`
	// Buckets is the manifest's record-bucket count.
	Buckets uint32 `json:"buckets"`
	// IndexMeta is the search index's meta version, so a run can skip
	// RepairKeys without opening the index (P2b-C15).
	IndexMeta int `json:"index_meta"`
	// IndexImage is the index image's length in bytes; IndexChunk its chunk size.
	IndexImage int64  `json:"index_image"`
	IndexChunk uint32 `json:"index_chunk"`
}

// Pointer is the root pointer: its head and the three sections' chunk tables.
type Pointer struct {
	Head
	Manifest []Entry
	History  []Entry
	Index    []Entry
}

// Seal encodes p and seals it under k at p.Gen, giving the pointer file's
// bytes: "MAPF" ‖ file_format u16 ‖ gen u64 ‖ SealPointer(ContainerManifest,
// gen, plaintext). It refuses a pointer that breaks the format's rules, so a
// writer never commits what a reader would refuse.
func Seal(k *vault.Key, p Pointer) ([]byte, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	plain, err := p.encode()
	if err != nil {
		return nil, err
	}
	sealed, err := k.SealPointer(vault.ContainerManifest, p.Gen, plain)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, fileHeadSize+len(sealed))
	out = append(out, pointerMagic...)
	out = binary.BigEndian.AppendUint16(out, vault.FormatVersion)
	out = binary.BigEndian.AppendUint64(out, p.Gen)
	out = append(out, sealed...)
	if len(out) > MaxPointerSize {
		return nil, fmt.Errorf("%w: %d bytes is over the %d-byte bound", ErrPointer, len(out), MaxPointerSize)
	}
	return out, nil
}

// Peek reads a pointer file's cleartext format and generation without the key.
// A keyless reader trusts the generation only as far as comparing it (§4); a
// keyed reader trusts it only after Open authenticates it.
func Peek(raw []byte) (format uint16, gen uint64, err error) {
	if len(raw) < fileHeadSize || string(raw[:len(pointerMagic)]) != pointerMagic {
		return 0, 0, ErrNotPointer
	}
	format = binary.BigEndian.Uint16(raw[4:6])
	gen = binary.BigEndian.Uint64(raw[6:14])
	if format > vault.FormatVersion {
		return format, gen, fmt.Errorf("%w (record pointer format %d, this MailArchive reads %d)", ErrNewerFormat, format, vault.FormatVersion)
	}
	if format != vault.FormatVersion || gen == 0 {
		return format, gen, fmt.Errorf("%w: format %d, generation %d", ErrPointer, format, gen)
	}
	return format, gen, nil
}

// Open authenticates a pointer file under k and decodes it strictly. A newer
// format is ErrNewerFormat before any decryption; bytes that fail to
// authenticate are vault.ErrAuth.
func Open(k *vault.Key, raw []byte) (Pointer, error) {
	if len(raw) > MaxPointerSize {
		return Pointer{}, fmt.Errorf("%w: %d bytes is over the %d-byte bound", ErrPointer, len(raw), MaxPointerSize)
	}
	_, gen, err := Peek(raw)
	if err != nil {
		return Pointer{}, err
	}
	plain, err := k.OpenPointer(vault.ContainerManifest, gen, raw[fileHeadSize:])
	if err != nil {
		return Pointer{}, err
	}
	p, err := decode(plain)
	if err != nil {
		return Pointer{}, err
	}
	if p.Gen != gen {
		return Pointer{}, fmt.Errorf("%w: head generation %d, file generation %d", ErrPointer, p.Gen, gen)
	}
	if err := p.check(); err != nil {
		return Pointer{}, err
	}
	return p, nil
}

// check is every rule a pointer must keep: the current format, a generation, a
// bucket count the vault accepts once there are manifest chunks, and chunk
// entries that name each file once, in section order, sealed no later than the
// pointer itself.
func (p Pointer) check() error {
	if p.Format != vault.FormatVersion {
		return fmt.Errorf("%w: head format %d, want %d", ErrPointer, p.Format, vault.FormatVersion)
	}
	if p.Gen == 0 {
		return fmt.Errorf("%w: generation 0", ErrPointer)
	}
	if p.Inv > p.Gen {
		return fmt.Errorf("%w: inventory generation %d is after the pointer's %d", ErrPointer, p.Inv, p.Gen)
	}
	if len(p.Manifest) > 0 && (p.Buckets < vault.MinBuckets || p.Buckets > vault.MaxBuckets || p.Buckets&(p.Buckets-1) != 0) {
		return fmt.Errorf("%w: bucket count %d", ErrPointer, p.Buckets)
	}
	if p.IndexImage < 0 {
		return fmt.Errorf("%w: index image length %d", ErrPointer, p.IndexImage)
	}
	seen := map[Name]bool{}
	for _, sec := range []struct {
		name    string
		entries []Entry
	}{{"manifest", p.Manifest}, {"history", p.History}, {"index", p.Index}} {
		if len(sec.entries) > maxEntries {
			return fmt.Errorf("%w: %d %s entries", ErrPointer, len(sec.entries), sec.name)
		}
		for j, e := range sec.entries {
			switch {
			case e.Name == Name{}:
				return fmt.Errorf("%w: %s entry %d has no name", ErrPointer, sec.name, j)
			case seen[e.Name]:
				return fmt.Errorf("%w: chunk %s is named twice", ErrPointer, e.Name)
			case e.Gen == 0 || e.Gen > p.Gen:
				return fmt.Errorf("%w: %s chunk %s sealed at generation %d, pointer at %d", ErrPointer, sec.name, e.Name, e.Gen, p.Gen)
			case e.Size == 0 || e.Size > MaxChunkSize:
				return fmt.Errorf("%w: %s chunk %s is %d bytes", ErrPointer, sec.name, e.Name, e.Size)
			case j > 0 && e.I < sec.entries[j-1].I:
				return fmt.Errorf("%w: %s entries out of order at %d", ErrPointer, sec.name, j)
			}
			seen[e.Name] = true
		}
	}
	return nil
}

// encode is the plaintext: u32 head length ‖ head JSON ‖ then, for each of
// manifest, history and index, u32 count ‖ count × 64-byte entries.
func (p Pointer) encode() ([]byte, error) {
	head, err := json.Marshal(p.Head)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Grow(4 + len(head) + 12 + entrySize*(len(p.Manifest)+len(p.History)+len(p.Index)))
	b.Write(binary.BigEndian.AppendUint32(nil, uint32(len(head))))
	b.Write(head)
	for _, sec := range [][]Entry{p.Manifest, p.History, p.Index} {
		b.Write(binary.BigEndian.AppendUint32(nil, uint32(len(sec))))
		for _, e := range sec {
			var row [entrySize]byte
			binary.BigEndian.PutUint32(row[0:4], e.I)
			copy(row[4:20], e.Name[:])
			binary.BigEndian.PutUint64(row[20:28], e.Gen)
			copy(row[28:60], e.SHA256[:])
			binary.BigEndian.PutUint32(row[60:64], e.Size)
			b.Write(row[:])
		}
	}
	return b.Bytes(), nil
}

// decode is encode's strict inverse: exact lengths, no trailing bytes, and a
// head with no unknown fields.
func decode(plain []byte) (Pointer, error) {
	bad := func(why string) (Pointer, error) { return Pointer{}, fmt.Errorf("%w: %s", ErrPointer, why) }
	if len(plain) < 4 {
		return bad("truncated")
	}
	n := int(binary.BigEndian.Uint32(plain))
	plain = plain[4:]
	if n > maxHeadSize || n > len(plain) {
		return bad("head length")
	}
	var p Pointer
	dec := json.NewDecoder(bytes.NewReader(plain[:n]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p.Head); err != nil {
		return bad("head: " + err.Error())
	}
	if dec.More() {
		return bad("trailing head data")
	}
	plain = plain[n:]
	for _, dst := range []*[]Entry{&p.Manifest, &p.History, &p.Index} {
		if len(plain) < 4 {
			return bad("truncated table")
		}
		count := int(binary.BigEndian.Uint32(plain))
		plain = plain[4:]
		if count > maxEntries || count*entrySize > len(plain) {
			return bad("table length")
		}
		entries := make([]Entry, count)
		for j := range entries {
			row := plain[j*entrySize : (j+1)*entrySize]
			entries[j].I = binary.BigEndian.Uint32(row[0:4])
			copy(entries[j].Name[:], row[4:20])
			entries[j].Gen = binary.BigEndian.Uint64(row[20:28])
			copy(entries[j].SHA256[:], row[28:60])
			entries[j].Size = binary.BigEndian.Uint32(row[60:64])
		}
		if count > 0 {
			*dst = entries
		}
		plain = plain[count*entrySize:]
	}
	if len(plain) != 0 {
		return bad("trailing bytes")
	}
	return p, nil
}
