package container

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"mail-archive-tool/internal/vault"
)

// indexChunkSize is the sealed search index's positional chunk size (design
// phase2b §6): SQLite never moves an existing page, so an unchanged chunk keeps its
// bytes from one commit to the next and is not re-sealed. 256 KiB to start; the
// measurement the design asks for may change it (a format change re-seals all).
const indexChunkSize = 256 << 10

// ErrIndexImage refuses a sealed index section that does not reassemble: a chunk
// missing, damaged or of the wrong size, or a length unlike the pointer's.
var ErrIndexImage = errors.New("container: the sealed search index is damaged")

// IndexImage is the archive's sealed search index, decrypted and checked chunk by
// chunk against the pointer — nil when the record holds no index. A damaged image
// is ErrIndexImage: the caller goes on with an empty index and marks it behind
// (P2b-C16), never stops the backup for it.
func (s *Store) IndexImage() ([]byte, error) {
	if len(s.ptr.Index) == 0 {
		return nil, nil
	}
	img := make([]byte, 0, s.ptr.IndexImage)
	digests := make([][32]byte, 0, len(s.ptr.Index))
	for i, e := range s.ptr.Index {
		if e.I != uint32(i) {
			return nil, fmt.Errorf("%w: chunk %d is listed at position %d", ErrIndexImage, i, e.I)
		}
		sealed, _, err := readChunk(s.out, e)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrIndexImage, err)
		}
		plain, err := s.opts.Key.OpenChunk(vault.ContainerIndex, e.Gen, e.I, sealed)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrIndexImage, err)
		}
		if i < len(s.ptr.Index)-1 && len(plain) != int(s.ptr.IndexChunk) {
			return nil, fmt.Errorf("%w: chunk %d is %d bytes, not the chunk size", ErrIndexImage, i, len(plain))
		}
		img = append(img, plain...)
		digests = append(digests, sha256.Sum256(plain))
	}
	if int64(len(img)) != s.ptr.IndexImage {
		return nil, fmt.Errorf("%w: %d bytes, the pointer says %d", ErrIndexImage, len(img), s.ptr.IndexImage)
	}
	s.indexDigests = digests
	return img, nil
}

// SetIndex registers the run's index: each commit asks it for its image — changed
// false when nothing was written since the last capture, so an unchanged index is
// neither captured nor re-sealed — and seals the chunks that differ.
func (s *Store) SetIndex(image func() (img []byte, changed bool, err error)) { s.indexSource = image }

// HasIndex reports whether the record holds a sealed search index.
func (s *Store) HasIndex() bool { return len(s.ptr.Index) > 0 }

// IndexBehind reports whether the sealed index is known to miss records (P2b-C16).
func (s *Store) IndexBehind() bool { return s.hdr.IndexBehind != nil }

// MarkIndexBehind records that the sealed index misses records from this run on:
// it could not be loaded, a run with indexing off committed records it did not
// add, or a commit could not seal it. Only a keyed rebuild clears it (§6).
func (s *Store) MarkIndexBehind() {
	if s.hdr.IndexBehind == nil {
		s.hdr.IndexBehind = &Behind{Since: s.attempted + 1}
		s.notice("the archive's search index is missing messages; a rebuild brings it back")
	}
}

// stagedIndex is a commit's index step, applied once its pointer has landed.
type stagedIndex struct {
	table    []Entry
	length   int64
	digests  [][32]byte
	captured bool
}

// stageIndex captures the registered index's image and seals, at gen, each
// positional chunk whose plaintext differs from the one the pointer names; the rest
// keep their entries. Without a registered index the table is unchanged. A capture
// or sealing failure keeps the old table and marks the index behind.
func (s *Store) stageIndex(gen uint64) stagedIndex {
	keep := stagedIndex{table: s.ptr.Index, length: s.ptr.IndexImage, digests: s.indexDigests}
	if s.indexSource == nil {
		return keep
	}
	img, changed, err := s.indexSource()
	if err != nil {
		s.MarkIndexBehind()
		return keep
	}
	if !changed {
		return keep
	}
	var table []Entry
	var digests [][32]byte
	for off, i := 0, 0; off < len(img); off, i = off+indexChunkSize, i+1 {
		end := min(off+indexChunkSize, len(img))
		plain := img[off:end]
		d := sha256.Sum256(plain)
		digests = append(digests, d)
		if i < len(s.indexDigests) && i < len(s.ptr.Index) && s.indexDigests[i] == d {
			table = append(table, s.ptr.Index[i])
			continue
		}
		e, err := s.writeChunk(vault.ContainerIndex, gen, uint32(i), plain)
		if err != nil {
			s.MarkIndexBehind()
			return keep
		}
		table = append(table, e)
	}
	return stagedIndex{table: table, length: int64(len(img)), digests: digests, captured: true}
}
