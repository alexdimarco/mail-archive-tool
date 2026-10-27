package container

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The keyed check's reads (design-encrypted-archives-phase3 §8; P3-2b, MA-799).

// checkedLoad is a read-only load whose units the given lister names.
func checkedLoad(t *testing.T, out string, units UnitFiles) *Store {
	t.Helper()
	_, s, err := LoadManifest(out, LoadOptions{Key: keys[0], ReadOnly: true, Units: units})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// covers: MA-799, R37
// CheckChunks opens every chunk the pointer names and names each one it cannot —
// its file, its part (section, position, which of a manifest section's two
// copies) and how it failed — never stopping at the first; a whole record has
// none, and a locked key ends it with the key's error.
func TestCheckChunksNamesEveryChunkItCannotOpen(t *testing.T) {
	out := readable(t)
	s := checkedLoad(t, out, unitLister)
	if p, err := s.CheckChunks(context.Background()); err != nil || len(p) != 0 {
		t.Fatalf("positive twin: %v %v", p, err)
	}
	var second Entry
	for _, e := range s.ptr.Manifest {
		if e.I == 0 {
			second = e // the last listed copy of section 0 is copy 2
		}
	}
	b, _ := os.ReadFile(filepath.Join(out, second.Name.Path()))
	b[0] ^= 1
	os.WriteFile(filepath.Join(out, second.Name.Path()), b, 0o600)
	os.Remove(filepath.Join(out, s.ptr.History[0].Name.Path()))
	ix := s.ptr.Index[1]
	ib, _ := os.ReadFile(filepath.Join(out, ix.Name.Path()))
	os.WriteFile(filepath.Join(out, ix.Name.Path()), ib[:len(ib)-1], 0o600)

	p, err := s.CheckChunks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ChunkProblem{
		second.Name.Path():           {Path: second.Name.Path(), Section: vault.ContainerManifest, Position: 0, Copy: 2, Kind: ChunkChanged},
		s.ptr.History[0].Name.Path(): {Path: s.ptr.History[0].Name.Path(), Section: vault.ContainerHistory, Position: 0, Kind: ChunkMissing},
		ix.Name.Path():               {Path: ix.Name.Path(), Section: vault.ContainerIndex, Position: 1, Kind: ChunkChanged},
	}
	if len(p) != len(want) {
		t.Fatalf("problems: %+v", p)
	}
	for _, got := range p {
		if want[got.Path] != got {
			t.Fatalf("got %+v, want %+v", got, want[got.Path])
		}
	}
	k := keyCopy(t)
	_, ls, err := LoadManifest(out, LoadOptions{Key: k, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	_, err = ls.CheckChunks(context.Background())
	assure.Reached(t, errors.Is(err, vault.ErrKeyDestroyed), "a locked key is the key's error")
}

// covers: MA-799, R37
// CrossCheck compares the files the authenticated record names — units and chunks —
// with the keyless file list written for the same generation: a whole archive
// agrees; a unit the record names and the list does not, one the list names and
// the record does not, and one listed with other bytes are each named; a file list
// one save behind cannot be compared.
func TestCrossCheckComparesTheRecordWithItsFileList(t *testing.T) {
	out := readable(t)
	if p, ok := checkedLoad(t, out, unitLister).CrossCheck(); !ok || len(p) != 0 {
		t.Fatalf("positive twin: %v %v", p, ok)
	}
	for name, lister := range map[string]struct {
		units UnitFiles
		kind  string
	}{
		"named, not listed": {func(e map[string]state.Record) (map[string]Digest, error) {
			u, err := unitLister(e)
			u["units/ff/ffffffffffffffffffffffffffffffff"] = Digest{Size: 1}
			return u, err
		}, ListUnlisted},
		"listed, not named": {func(e map[string]state.Record) (map[string]Digest, error) {
			u, err := unitLister(e)
			for p := range u {
				delete(u, p)
				break
			}
			return u, err
		}, ListUnnamed},
		"listed with other bytes": {func(e map[string]state.Record) (map[string]Digest, error) {
			u, err := unitLister(e)
			for p, d := range u {
				d.Size++
				u[p] = d
				break
			}
			return u, err
		}, ListDifferent},
	} {
		p, ok := checkedLoad(t, out, lister.units).CrossCheck()
		if !ok || len(p) != 1 || p[0].Kind != lister.kind {
			t.Fatalf("%s: %+v %v", name, p, ok)
		}
	}
	// A load that listed no units compares the chunks alone.
	if p, ok := checkedLoad(t, out, nil).CrossCheck(); !ok || len(p) != 0 {
		t.Fatalf("chunks alone: %v %v", p, ok)
	}
	// A list written for an earlier generation (a commit whose list write failed),
	// and none at all: not comparable.
	s := checkedLoad(t, out, unitLister)
	behind := *s.inv
	behind.Gen--
	s.inv = &behind
	_, ok1 := s.CrossCheck()
	s.inv = nil
	_, ok2 := s.CrossCheck()
	assure.Reached(t, !ok1 && !ok2, "a list behind, or none, is not compared")
}
