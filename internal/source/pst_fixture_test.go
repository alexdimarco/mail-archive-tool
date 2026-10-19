package source

import (
	"bytes"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	pst "github.com/mooijtech/go-pst/v6/pkg"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// The helpers of the PST isolation tests (MA-315..MA-318): byte-patched copies of
// testdata/support.pst, the F23/F27 PoC technique. The file is permute-encrypted
// (go-pst's compressible encryption is a byte substitution), so a byte of a decoded
// structure is patched through the inverse substitution.

var (
	fixtureOnce sync.Once
	fixtureRaw  []byte
	fixtureErr  error
)

// fixtureData returns a private copy of the healthy fixture's bytes.
func fixtureData(t *testing.T) []byte {
	t.Helper()
	fixtureOnce.Do(func() { fixtureRaw, fixtureErr = os.ReadFile(fixture) })
	if fixtureErr != nil {
		t.Skipf("fixture missing: %v", fixtureErr)
	}
	return append([]byte(nil), fixtureRaw...)
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// openData parses data as the PST reader would parse a file.
func openData(t *testing.T, data []byte) *Reader {
	t.Helper()
	registerCharsets()
	r, err := openPSTReader(bytes.NewReader(data), io.Closer(nopCloser{}), "fixture.pst")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// encode is the byte that decodes to d under go-pst's compressible encryption.
var (
	invOnce sync.Once
	invTab  [256]byte
)

func encode(d byte) byte {
	invOnce.Do(func() {
		dec := &pst.HeapOnNodeReader{}
		for b := 0; b < 256; b++ {
			invTab[dec.DecodeCompressibleEncryption([]byte{byte(b)})[0]] = byte(b)
		}
	})
	return invTab[d]
}

func decode(b []byte) []byte {
	return (&pst.HeapOnNodeReader{}).DecodeCompressibleEncryption(append([]byte(nil), b...))
}

// reference is what go-pst's OWN iterators yield on a file: the folder
// identifiers WalkFolders visits and the normal-message NIDs each folder's
// MessageIterator yields, in order (FC30's differential baseline).
type reference struct {
	folders  []pst.Identifier
	messages []pst.Identifier
}

func goPstReference(t *testing.T, f *pst.File) reference {
	t.Helper()
	var ref reference
	err := f.WalkFolders(func(fo *pst.Folder) error {
		ref.folders = append(ref.folders, fo.Identifier)
		it, err := fo.GetMessageIterator()
		if err != nil {
			return nil // no messages (pst.ErrMessagesNotFound) or none readable
		}
		for it.Next() {
			ref.messages = append(ref.messages, it.Value().Identifier)
		}
		return it.Err()
	})
	if err != nil {
		t.Fatalf("go-pst's own walk of the healthy fixture: %v", err)
	}
	return ref
}

// goPstYield counts the messages go-pst's own loop yields on data before it stops
// at the first failure, and whether it stopped on one: the behaviour the reader's
// loops replace (F23: a corrupted message ended the whole store).
func goPstYield(data []byte) (n int, stopped bool) {
	defer func() {
		if recover() != nil {
			stopped = true
		}
	}()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		return 0, true
	}
	err = f.WalkFolders(func(fo *pst.Folder) error {
		it, ierr := fo.GetMessageIterator()
		if ierr != nil {
			return nil
		}
		for it.Next() {
			n++
		}
		return it.Err()
	})
	return n, err != nil
}

// goPstReads reports whether go-pst's own GetMessage reads nid on data.
func goPstReads(data []byte, nid pst.Identifier) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		return false
	}
	_, err = f.GetMessage(nid)
	return err == nil
}

// blockOf is where nid's data block sits in the file.
func blockOf(t *testing.T, data []byte, nid pst.Identifier) (off int64, size int64) {
	t.Helper()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bn, err := f.GetDataBTreeNode(nid)
	if err != nil {
		t.Fatalf("no data block for NID %d: %v", nid, err)
	}
	return bn.FileOffset, int64(bn.Size)
}

// corruptMessage is the F23 PoC mutation: the first byte of nid's block XORed.
// It breaks go-pst's GetMessage for that one node (an error, or a panic for #8)
// and no other.
func corruptMessage(t *testing.T, data []byte, nid pst.Identifier) []byte {
	t.Helper()
	off, _ := blockOf(t, data, nid)
	mut := append([]byte(nil), data...)
	mut[off] ^= 0xFF
	if goPstReads(mut, nid) {
		t.Fatalf("precondition: the mutation did not break go-pst's own read of NID %d", nid)
	}
	return mut
}

// hierarchyRow finds the row of child in parent's hierarchy table: the block's
// file offset and the position of the row's first cell (the folder id) in the
// decoded block.
func hierarchyRow(t *testing.T, data []byte, parent, child pst.Identifier) (blockOff int64, row int, decoded []byte) {
	t.Helper()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	nb, err := f.GetNodeBTreeNode(parent + 11)
	if err != nil {
		t.Fatalf("no hierarchy table for folder %d: %v", parent, err)
	}
	bb, err := f.GetBlockBTreeNode(nb.DataIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	decoded = decode(data[bb.FileOffset : bb.FileOffset+int64(bb.Size)])
	pat := []byte{byte(child), byte(child >> 8), byte(child >> 16), byte(child >> 24)}
	row = -1
	for off := 0; ; {
		i := bytes.Index(decoded[off:], pat)
		if i < 0 {
			break
		}
		row = off + i // the LAST occurrence is the row matrix's cell (the B-tree-on-heap keys come first)
		off += i + 1
	}
	if row < 0 {
		t.Fatalf("folder %d is not in folder %d's hierarchy table", child, parent)
	}
	return bb.FileOffset, row, decoded
}

// patchCell sets, in child's row of parent's hierarchy table, the four bytes at
// rowOffset (0 = the folder id, 8 = the name, 12 = the message count) to the
// little-endian value v.
func patchCell(t *testing.T, data []byte, parent, child pst.Identifier, rowOffset int, v uint32) []byte {
	t.Helper()
	blockOff, row, _ := hierarchyRow(t, data, parent, child)
	mut := append([]byte(nil), data...)
	for i := 0; i < 4; i++ {
		mut[blockOff+int64(row+rowOffset+i)] = encode(byte(v >> (8 * i)))
	}
	return mut
}

// walkOut is what one walk produced.
type walkOut struct {
	msgs   []*model.Message
	faults []Fault
	err    error
	w      *pstWalk
}

// nids are the NIDs the walk's messages carry in their locators.
func (o walkOut) nids(t *testing.T) []pst.Identifier {
	t.Helper()
	var out []pst.Identifier
	for _, m := range o.msgs {
		out = append(out, nidOf(t, m.Locator))
	}
	return out
}

func nidOf(t *testing.T, locator string) pst.Identifier {
	t.Helper()
	i := strings.LastIndexByte(locator, '/')
	if !strings.HasPrefix(locator, "pst-nid:") || i < 0 {
		t.Fatalf("locator %q is not a pst-nid: locator", locator)
	}
	n, err := strconv.ParseInt(locator[i+1:], 10, 64)
	if err != nil {
		t.Fatalf("locator %q: %v", locator, err)
	}
	return pst.Identifier(n)
}

const walkGuard = 10 * time.Second

// walkOnce walks r under the 10 s guard of MA-315: a hang fails the test instead
// of the whole binary.
func walkOnce(t *testing.T, r *Reader, opts WalkOptions) walkOut {
	t.Helper()
	var out walkOut
	if opts.Faults == nil {
		opts.Faults = func(f Fault) { out.faults = append(out.faults, f) }
	}
	out.w = r.newWalk(func(_ []string, m *model.Message) error {
		out.msgs = append(out.msgs, m)
		return nil
	}, opts)
	done := make(chan error, 1)
	go func() { done <- out.w.run() }()
	select {
	case out.err = <-done:
	case <-time.After(walkGuard):
		t.Fatalf("the walk did not return within %v: a hang", walkGuard)
	}
	return out
}

func sortedIDs(ids []pst.Identifier) []pst.Identifier {
	out := append([]pst.Identifier(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// referenceOf is the healthy fixture's go-pst reference (17 messages in two folders).
func referenceOf(t *testing.T, data []byte) reference {
	t.Helper()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	ref := goPstReference(t, f)
	assure.Reached(t, ref.messages, "the fixture's messages")
	if len(ref.messages) != 17 {
		t.Fatalf("the fixture holds %d messages by go-pst's own count, want 17", len(ref.messages))
	}
	return ref
}
