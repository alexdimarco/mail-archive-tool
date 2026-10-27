package container

import (
	"context"
	"errors"
	"io/fs"
	"sort"
	"strings"

	"mail-archive-tool/internal/vault"
)

// The keyed check's reads (design-encrypted-archives-phase3 §8, tier 2): every
// chunk the committed pointer names opened with the key, and the files the record
// names compared with the archive's file list. Read-only: they write nothing and
// change nothing the Store will commit.

// Chunk problem kinds.
const (
	ChunkMissing   = "missing"   // the file is gone
	ChunkChanged   = "modified"  // its size or bytes are not the ones the pointer names
	ChunkNotOpened = "unreadable" // its bytes are the pointer's, but it does not open with the key
)

// ChunkProblem is a chunk the check could not read: its file, which part of the
// record it is, and how it failed.
type ChunkProblem struct {
	Path     string
	Section  vault.ContainerKind // manifest, history or index
	Position uint32
	Copy     int // 1 or 2 for a manifest section's two copies; 0 for the others' one
	Kind     string
}

// CheckChunks reads and opens every chunk the committed pointer names — both copies
// of every manifest section, every history and index chunk — and returns each one
// it could not, never stopping at the first (§8). A locked key ends it with the
// key's error, never a problem; so does a stopped ctx.
func (s *Store) CheckChunks(ctx context.Context) ([]ChunkProblem, error) {
	var out []ChunkProblem
	copyOf := map[uint32]int{}
	for _, sec := range []struct {
		kind    vault.ContainerKind
		entries []Entry
	}{{vault.ContainerManifest, s.ptr.Manifest}, {vault.ContainerHistory, s.ptr.History}, {vault.ContainerIndex, s.ptr.Index}} {
		for i, e := range sec.entries {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			p := ChunkProblem{Path: e.Name.Path(), Section: sec.kind, Position: e.I}
			if sec.kind == vault.ContainerManifest {
				copyOf[e.I]++
				p.Copy = copyOf[e.I]
			}
			sealed, _, err := readChunk(s.out, e)
			if err == nil {
				var plain []byte
				plain, err = s.opts.Key.OpenChunk(sec.kind, e.Gen, e.I, sealed)
				if errors.Is(err, vault.ErrKeyDestroyed) {
					return out, err
				}
				if err == nil && sec.kind == vault.ContainerIndex && i < len(sec.entries)-1 && len(plain) != int(s.ptr.IndexChunk) {
					err = ErrIndexImage // a chunk of the wrong size: the image does not assemble
				}
			}
			switch {
			case err == nil:
				continue
			case errors.Is(err, fs.ErrNotExist):
				p.Kind = ChunkMissing
			case errors.Is(err, ErrStore):
				p.Kind = ChunkChanged
			default:
				p.Kind = ChunkNotOpened
			}
			out = append(out, p)
		}
	}
	return out, nil
}

// List problem kinds.
const (
	ListUnlisted  = "not in the file list" // the record names it; the file list does not
	ListUnnamed   = "not named by the record"
	ListDifferent = "listed with other bytes"
)

// ListProblem is a file on which the record and the file list disagree.
type ListProblem struct {
	Path string
	Kind string
}

// CrossCheck compares the files the record names — its sealed units, when the load
// listed them (LoadOptions.Units), and its chunks — with the live files of the file
// list written for this very generation: the authenticated record against the
// keyless list (§8 step 5). ok is false when they cannot be compared: no whole file
// list, or one written for an earlier generation.
func (s *Store) CrossCheck() (problems []ListProblem, ok bool) {
	if s.inv == nil || s.shards == nil || s.inv.Gen != s.ptr.Gen {
		return nil, false
	}
	listed := map[string]File{}
	for prefix, b := range s.shards {
		sh, err := ParseShard(prefix, b)
		if err != nil {
			return nil, false
		}
		for _, f := range sh.Files {
			// A load that did not list the units compares the chunks alone.
			if f.State == Live && (s.opts.Units != nil || !strings.HasPrefix(f.Path, "units/")) {
				listed[f.Path] = f
			}
		}
	}
	for path, d := range s.prevLive {
		f, in := listed[path]
		switch {
		case !in:
			problems = append(problems, ListProblem{Path: path, Kind: ListUnlisted})
		case f.Digest != d:
			problems = append(problems, ListProblem{Path: path, Kind: ListDifferent})
		}
	}
	for path := range listed {
		if _, named := s.prevLive[path]; !named {
			problems = append(problems, ListProblem{Path: path, Kind: ListUnnamed})
		}
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].Path < problems[j].Path })
	return problems, true
}
