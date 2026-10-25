package container

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/vault"
)

// imageSource is a registered index of fixed bytes: changed until captured once.
type imageSource struct {
	img           []byte
	version, rows int
	captured      bool
	fail          error
}

func (s *imageSource) get() (IndexCapture, bool, error) {
	if s.fail != nil {
		return IndexCapture{}, false, s.fail
	}
	if s.captured {
		return IndexCapture{}, false, nil
	}
	s.captured = true
	return IndexCapture{Image: s.img, Version: s.version, Rows: s.rows}, true, nil
}

func setOf(paths []string) map[string]bool {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	return set
}

func pattern(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i) ^ seed
	}
	return b
}

// covers: MA-757, R37
// The search index is sealed in the record as positional chunks (Phase 2b-3, design
// phase2b §6): a registered index's image, committed by the record's save, loads back
// byte for byte; a commit re-seals only the chunks whose bytes changed; an index
// that did not change is neither captured nor re-sealed.
func TestTheSearchIndexIsSealedAsPositionalChunks(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	img := pattern(3*indexChunkSize+1000, 7) // four chunks, the last short
	src := &imageSource{img: img, version: 3, rows: 41}
	s.SetIndex(src.get)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m, s = open(t, out, opts(2))
	got, err := s.IndexImage()
	if err != nil || !bytes.Equal(got, img) {
		t.Fatalf("the sealed index loads back %d bytes (%v), want the %d sealed", len(got), err, len(img))
	}
	if v, n := s.IndexMeta(); v != 3 || n != 41 {
		t.Fatalf("the record says the index is version %d with %d rows, want 3 and 41", v, n)
	}
	if !s.HasIndex() || len(s.Chunks(vault.ContainerIndex)) != 4 {
		t.Fatalf("%d index chunks, want 4", len(s.Chunks(vault.ContainerIndex)))
	}

	before := setOf(s.Chunks(vault.ContainerIndex))
	changed := append([]byte(nil), img...)
	changed[indexChunkSize+5] ^= 0xff // one byte in the second chunk
	s.SetIndex((&imageSource{img: changed, version: 3, rows: 42}).get)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if n := newIn(setOf(s.Chunks(vault.ContainerIndex)), before); n != 1 {
		t.Fatalf("one changed chunk re-sealed %d index chunks", n)
	}
	before = chunkFiles(t, out)
	s.SetIndex((&imageSource{captured: true}).get) // nothing written since the capture
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if n := newIn(chunkFiles(t, out), before); n != 0 {
		t.Fatalf("an unchanged index re-sealed %d chunks", n)
	}
	_, s = open(t, out, opts(3))
	if got, _ := s.IndexImage(); !bytes.Equal(got, changed) {
		t.Fatal("after an unchanged commit the index must still load as last sealed")
	}
	if v, n := s.IndexMeta(); v != 3 || n != 42 {
		t.Fatalf("an unchanged commit must keep the last capture's version and rows (3, 42), got %d, %d", v, n)
	}
}

// covers: MA-758, R37
// An index capture counts as sealed only once its commit has landed: a commit that
// fails after capturing it (here, the archive lock lost before the pointer is
// written) never tells the run its index was sealed, so the next commit captures and
// seals it, with its row count.
func TestAnIndexCaptureIsSealedOnlyWhenItsCommitLands(t *testing.T) {
	out := fresh(t)
	lost := errors.New("the lock was taken")
	o := opts(1)
	o.StillHeld = func() error { return lost }
	m, s := open(t, out, o)
	sealedCalls := 0
	s.SetIndex(func() (IndexCapture, bool, error) {
		return IndexCapture{Image: pattern(indexChunkSize, 9), Version: 3, Rows: 5, Sealed: func() { sealedCalls++ }}, true, nil
	})
	if err := m.Save(); !errors.Is(err, ErrLockLost) {
		t.Fatalf("want the lock-lost refusal, got %v", err)
	}
	if sealedCalls != 0 {
		t.Fatal("a capture whose commit failed was reported sealed")
	}
	s.opts.StillHeld = nil
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if sealedCalls != 1 {
		t.Fatalf("the landed commit reported its capture sealed %d time(s), want once", sealedCalls)
	}
	if _, s = open(t, out, opts(2)); !s.HasIndex() {
		t.Fatal("the next commit did not seal the index")
	}
	if v, n := s.IndexMeta(); v != 3 || n != 5 {
		t.Fatalf("the record says version %d with %d rows, want 3 and 5", v, n)
	}
}

// covers: MA-757, R37
// The index reassembles only as its pointer describes it: an authenticated pointer
// whose image length disagrees with its chunks is ErrIndexImage, never a short or
// padded image handed to the database.
func TestAnIndexImageMustMatchItsPointer(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	s.SetIndex((&imageSource{img: pattern(indexChunkSize+10, 5)}).get)
	m.Save()
	_, s = open(t, out, opts(2))
	p := s.ptr
	p.IndexImage++
	raw, err := Seal(keys[0], p)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(out, PointerName), raw, 0o600)
	os.WriteFile(filepath.Join(out, MirrorName), raw, 0o600)
	os.Remove(filepath.Join(out, InventoryName)) // the file list named the old pointer
	p.Inv = 0
	raw, _ = Seal(keys[0], p)
	os.WriteFile(filepath.Join(out, PointerName), raw, 0o600)
	os.WriteFile(filepath.Join(out, MirrorName), raw, 0o600)
	_, s = open(t, out, opts(3))
	if _, err := s.IndexImage(); !errors.Is(err, ErrIndexImage) {
		t.Fatalf("an image length unlike the pointer's: want ErrIndexImage, got %v", err)
	}
}

// covers: MA-757, R37
// An index that cannot be read is ErrIndexImage — the run goes on with an empty one
// — and an index that cannot be captured keeps its sealed chunks; either way the
// record marks the index behind in its header, which persists across commits.
func TestADamagedOrFailedIndexIsMarkedBehind(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	s.SetIndex((&imageSource{img: pattern(2*indexChunkSize, 3)}).get)
	m.Save()
	_, s = open(t, out, opts(2))
	first := s.Chunks(vault.ContainerIndex)[0]
	p := filepath.Join(out, filepath.FromSlash(first))
	b, _ := os.ReadFile(p)
	b[len(b)-1] ^= 1
	os.WriteFile(p, b, 0o600)
	m, s = open(t, out, opts(3))
	if _, err := s.IndexImage(); !errors.Is(err, ErrIndexImage) {
		t.Fatalf("a damaged index chunk: want ErrIndexImage, got %v", err)
	}
	s.MarkIndexBehind()
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if _, s = open(t, out, opts(4)); !s.IndexBehind() {
		t.Fatal("the index-behind mark must persist in the sealed header")
	}

	out2 := fresh(t)
	m, s = open(t, out2, opts(1))
	s.SetIndex((&imageSource{img: pattern(indexChunkSize, 1), version: 3, rows: 9}).get)
	m.Save()
	m, s = open(t, out2, opts(2))
	sealed := s.Chunks(vault.ContainerIndex)
	s.SetIndex((&imageSource{fail: errors.New("serialize failed")}).get)
	if err := m.Save(); err != nil {
		t.Fatalf("a failed index capture failed the record's save: %v", err)
	}
	if got := s.Chunks(vault.ContainerIndex); len(got) != len(sealed) || got[0] != sealed[0] || !s.IndexBehind() {
		t.Fatalf("a failed capture must keep the sealed chunks and mark the index behind: %v %v %v", got, sealed, s.IndexBehind())
	}
	if v, n := s.IndexMeta(); v != 3 || n != 9 {
		t.Fatalf("a failed capture must keep the sealed version and rows (3, 9), got %d, %d", v, n)
	}
}
