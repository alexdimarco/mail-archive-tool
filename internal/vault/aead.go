// Package vault is the cryptographic core of encrypted archives
// (docs/design-encrypted-archives.md, the "Re-review amendments" A1–A14 govern).
// It holds no engine knowledge: it seals and opens bytes for a named place in
// an archive, and it keeps the archive's key material in the vault sidecar.
//
// The key hierarchy: a random master key encrypts the archive. It is stored only
// wrapped — once under a key derived from the password and once under a key
// derived from the recovery key (Argon2id, A3) — in `.mailarchive-vault.json`,
// mirrored to `.mailarchive-vault.bak.json`. Changing the password re-wraps the
// master key; no archive data is re-encrypted. The vault's public fields are
// authenticated by a header MAC under a key derived from the master key (A7).
//
// Every seal is XChaCha20-Poly1305 with a fresh random 192-bit nonce (A1), and
// every seal's associated data names the format version, the archive and the
// exact place the bytes belong (A2), so sealed bytes cannot be moved between
// places, kinds or archives without failing to open.
package vault

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// FormatVersion is the encrypted-archive format every AAD carries (A2).
const FormatVersion uint16 = 1

const (
	keySize   = chacha20poly1305.KeySize    // 32
	nonceSize = chacha20poly1305.NonceSizeX // 24
	overhead  = chacha20poly1305.Overhead   // 16

	// maxUnitIDLen bounds a unit identity so its length fits the u16 prefix.
	maxUnitIDLen = 1<<16 - 1
)

// ErrAuth is the refusal of sealed bytes that do not authenticate: altered,
// truncated, moved to another place, or sealed for another archive or key.
var ErrAuth = errors.New("vault: sealed data failed authentication (altered, truncated, moved, or from another archive)")

// randReader is the source of every nonce, key, salt and archive id; a var so a
// test can observe each draw and inject a failing source.
var randReader io.Reader = rand.Reader

// ID is an archive's stable random identity (C7): drawn once at encrypt time,
// carried in the vault so it travels with the archive, bound into every AAD, and
// the account the remembered key is filed under.
type ID [16]byte

func (id ID) String() string { return hex.EncodeToString(id[:]) }

func parseID(s string) (ID, error) {
	var id ID
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(id) {
		return id, fmt.Errorf("archive id %q is not 32 hex characters", s)
	}
	copy(id[:], b)
	return id, nil
}

// UnitKind names what a sealed unit holds.
type UnitKind uint8

const (
	UnitMessage    UnitKind = 1 // a message body page
	UnitAttachment UnitKind = 2 // a message's attachments
)

// ContainerKind names a chunked container (C6, A11).
type ContainerKind uint8

const (
	ContainerManifest ContainerKind = 1
	ContainerIndex    ContainerKind = 2
)

// entryKind names a key-wrap entry in the vault.
type entryKind uint8

const (
	entryPassword entryKind = 1
	entryRecovery entryKind = 2
)

// aadPrefix is the 4-byte domain tag, the format version and the archive id that
// start every AAD (A2).
func aadPrefix(tag string, id ID) []byte {
	b := make([]byte, 0, 4+2+len(id)+16)
	b = append(b, tag...)
	b = binary.BigEndian.AppendUint16(b, FormatVersion)
	return append(b, id[:]...)
}

// unitAAD is `"MAU1" ‖ format u16 ‖ archive_id ‖ unit_kind u8 ‖ len u16 ‖ unit_id`.
func unitAAD(id ID, kind UnitKind, unitID string) ([]byte, error) {
	if len(unitID) == 0 || len(unitID) > maxUnitIDLen {
		return nil, fmt.Errorf("vault: unit id must be 1..%d bytes, got %d", maxUnitIDLen, len(unitID))
	}
	b := append(aadPrefix("MAU1", id), byte(kind))
	b = binary.BigEndian.AppendUint16(b, uint16(len(unitID)))
	return append(b, unitID...), nil
}

// chunkAAD is `"MAC1" ‖ format u16 ‖ archive_id ‖ container_kind u8 ‖ generation u64 ‖ chunk_index u32`.
func chunkAAD(id ID, kind ContainerKind, gen uint64, index uint32) []byte {
	b := append(aadPrefix("MAC1", id), byte(kind))
	b = binary.BigEndian.AppendUint64(b, gen)
	return binary.BigEndian.AppendUint32(b, index)
}

// pointerAAD is `"MAP1" ‖ format u16 ‖ archive_id ‖ container_kind u8 ‖ generation u64`.
func pointerAAD(id ID, kind ContainerKind, gen uint64) []byte {
	b := append(aadPrefix("MAP1", id), byte(kind))
	return binary.BigEndian.AppendUint64(b, gen)
}

// wrapAAD is `"MAW1" ‖ format u16 ‖ archive_id ‖ entry_kind u8`.
func wrapAAD(id ID, kind entryKind) []byte {
	return append(aadPrefix("MAW1", id), byte(kind))
}

// seal encrypts plaintext under key with a fresh random nonce, returning
// nonce ‖ ciphertext ‖ tag. A nonce is never derived, counted or reused (A1).
func seal(key, aad, plaintext []byte) ([]byte, error) {
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("vault: cipher: %w", err)
	}
	out := make([]byte, nonceSize, nonceSize+len(plaintext)+overhead)
	if _, err := io.ReadFull(randReader, out); err != nil {
		return nil, fmt.Errorf("vault: draw nonce: %w", err)
	}
	return a.Seal(out, out, plaintext, aad), nil
}

// open authenticates and decrypts the output of seal, or returns ErrAuth.
func open(key, aad, sealed []byte) ([]byte, error) {
	if len(sealed) < nonceSize+overhead {
		return nil, ErrAuth
	}
	a, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("vault: cipher: %w", err)
	}
	pt, err := a.Open(nil, sealed[:nonceSize], sealed[nonceSize:], aad)
	if err != nil {
		return nil, ErrAuth
	}
	return pt, nil
}

// Key is an unlocked archive: its master key, the sub-keys derived from it, and
// the archive it belongs to. Archive data is sealed under the data sub-key, the
// vault header is authenticated under the MAC sub-key (HKDF-SHA256 domain
// separation), and the master key itself is only ever wrapped or remembered.
type Key struct {
	id     ID
	master [keySize]byte
	data   [keySize]byte
	mac    [keySize]byte
}

// HKDF info strings: the sub-key domains.
const (
	infoData = "mailarchive data v1"
	infoMAC  = "mailarchive vault header v1"
)

func newKey(id ID, master []byte) (*Key, error) {
	if len(master) != keySize {
		return nil, fmt.Errorf("vault: master key is %d bytes, want %d", len(master), keySize)
	}
	k := &Key{id: id}
	copy(k.master[:], master)
	for _, sub := range []struct {
		info string
		dst  *[keySize]byte
	}{{infoData, &k.data}, {infoMAC, &k.mac}} {
		b, err := hkdf.Key(sha256.New, master, nil, sub.info, keySize)
		if err != nil {
			return nil, fmt.Errorf("vault: derive %s: %w", sub.info, err)
		}
		copy(sub.dst[:], b)
	}
	return k, nil
}

// ArchiveID is the archive this key unlocks.
func (k *Key) ArchiveID() ID { return k.id }

// Destroy overwrites the key material held in memory.
func (k *Key) Destroy() {
	clear(k.master[:])
	clear(k.data[:])
	clear(k.mac[:])
}

// SealUnit seals one message body or attachment set for the unit named unitID
// (its stable manifest identity).
func (k *Key) SealUnit(kind UnitKind, unitID string, plaintext []byte) ([]byte, error) {
	aad, err := unitAAD(k.id, kind, unitID)
	if err != nil {
		return nil, err
	}
	return seal(k.data[:], aad, plaintext)
}

// OpenUnit opens what SealUnit sealed for the same kind and unitID.
func (k *Key) OpenUnit(kind UnitKind, unitID string, sealed []byte) ([]byte, error) {
	aad, err := unitAAD(k.id, kind, unitID)
	if err != nil {
		return nil, err
	}
	return open(k.data[:], aad, sealed)
}

// SealChunk seals chunk index of a container at generation gen. Every call
// draws a fresh nonce, so re-sealing a changed chunk never reuses one (A1).
func (k *Key) SealChunk(kind ContainerKind, gen uint64, index uint32, plaintext []byte) ([]byte, error) {
	return seal(k.data[:], chunkAAD(k.id, kind, gen, index), plaintext)
}

// OpenChunk opens what SealChunk sealed for the same kind, gen and index.
func (k *Key) OpenChunk(kind ContainerKind, gen uint64, index uint32, sealed []byte) ([]byte, error) {
	return open(k.data[:], chunkAAD(k.id, kind, gen, index), sealed)
}

// SealPointer seals a container's pointer (its ordered chunk list) at gen.
func (k *Key) SealPointer(kind ContainerKind, gen uint64, plaintext []byte) ([]byte, error) {
	return seal(k.data[:], pointerAAD(k.id, kind, gen), plaintext)
}

// OpenPointer opens what SealPointer sealed for the same kind and gen.
func (k *Key) OpenPointer(kind ContainerKind, gen uint64, sealed []byte) ([]byte, error) {
	return open(k.data[:], pointerAAD(k.id, kind, gen), sealed)
}
