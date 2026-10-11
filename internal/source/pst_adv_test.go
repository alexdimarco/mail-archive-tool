package source

import (
	"bytes"
	"fmt"
	"testing"

	pst "github.com/mooijtech/go-pst/v6/pkg"

	"mail-archive-tool/internal/state"
)

// The FI-S2a adversarial pass's tombstones for the PST reader
// (docs/review-fault-isolation-s2a-adversarial.md).

// tableBlock is where the first data block of table node nid sits in the file: the
// table's TCINFO starts 20 bytes in (after the heap header and the allocation
// table), and its row size is the two bytes at +8 of it.
func tableBlock(t *testing.T, data []byte, nid pst.Identifier) (off int64, size int64) {
	t.Helper()
	f, err := pst.New(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	nb, err := f.GetNodeBTreeNode(nid)
	if err != nil {
		t.Fatalf("no node %d: %v", nid, err)
	}
	bb, err := f.GetBlockBTreeNode(nb.DataIdentifier)
	if err != nil {
		t.Fatal(err)
	}
	dec := decode(data[bb.FileOffset : bb.FileOffset+int64(bb.Size)])
	if len(dec) < 40 || dec[20] != 0x7C {
		t.Fatalf("node %d: no TCINFO at +20 of its first block", nid)
	}
	return bb.FileOffset, int64(bb.Size)
}

// patchRowSize sets the row size (TCI_bm) of table node nid to v: the 2-byte table
// header lie of S2A-AO2 (the count cells stay right and only the geometry lies).
func patchRowSize(t *testing.T, data []byte, nid pst.Identifier, v uint16) []byte {
	t.Helper()
	off, _ := tableBlock(t, data, nid)
	mut := append([]byte(nil), data...)
	mut[off+28] = encode(byte(v))
	mut[off+29] = encode(byte(v >> 8))
	return mut
}

// trueRowSize is the healthy row size of table node nid.
func trueRowSize(t *testing.T, data []byte, nid pst.Identifier) uint16 {
	t.Helper()
	off, size := tableBlock(t, data, nid)
	dec := decode(data[off : off+size])
	return uint16(dec[28]) | uint16(dec[29])<<8
}

// silentLoss reports whether the walk of data lost a message and said nothing: the
// R1 failure (fewer than 17 messages and no fault, or an error that is no gap).
func silentLoss(t *testing.T, data []byte) (silent bool, out walkOut) {
	t.Helper()
	out = walkOnce(t, openData(t, data), WalkOptions{})
	return out.err == nil && len(out.msgs) < 17 && len(out.faults) == 0, out
}

// covers: MA-316, MA-315, R1, R10
// S2A-AO2 and S2A-AC2 (and K7): a PST that LIES about a folder's table (a 2-byte row
// size in the table header, a column descriptor, a count cell) must never lose a
// folder, or part of one, without a gap. go-pst derives the row count from the row
// size and finds the sub-folder and count columns by their descriptors, so each lie
// reads as a store, a folder or part of a folder that is simply empty: no error, no
// fault, and a completed walk then closes every older gap as "no longer in the
// source". The reader checks a table's geometry against its own columns, a folder's
// count against the rows its table lists, and the folders that say they are empty
// against the tables the file holds for them.
func TestPSTTableHeaderLiesAreNeverSilent(t *testing.T) {
	data := fixtureData(t)
	if silent, out := silentLoss(t, data); silent || len(out.msgs) != 17 || len(out.faults) != 0 {
		t.Fatalf("twin: the healthy fixture yielded %d messages, %d faults", len(out.msgs), len(out.faults))
	}

	// The flips of the finding: one byte of a table header or column descriptor, XOR
	// 0xFF and 0x01, at the offsets its sweeps found silent.
	offsets := map[string]int{
		"root hierarchy row size":      18972,
		"root hierarchy row size (hi)": 18973,
		"root column descriptor":       19044,
		"root column descriptor (b)":   19045,
		"Drafts contents row size":     266653,
		"Sent contents row size":       771869,
		"Top hierarchy descriptor":     317214,
		"descriptor sweep (d)":         318130,
		"descriptor sweep (e)":         318967,
	}
	for name, off := range offsets {
		for _, x := range []byte{0xFF, 0x01} {
			mut := append([]byte(nil), data...)
			mut[off] ^= x
			if silent, out := silentLoss(t, mut); silent {
				t.Errorf("%s (byte %d ^ %#x): %d of 17 messages read, %d faults, err %v: the loss is silent", name, off, x, len(out.msgs), len(out.faults), out.err)
			}
		}
	}

	// The row-size matrix, sampled: on the tables of the root (one row), Top (the
	// sixteen sub-folders), Drafts and Sent Messages (their messages). The healthy
	// value reads the 17; every other value reads 17 or says so.
	tables := map[string]pst.Identifier{
		"root hierarchy":  rootFolder + 11,
		"Top hierarchy":   topFolder + 11,
		"Drafts contents": draftsFolder + 12,
		"Sent contents":   sentFolder + 12,
	}
	for name, nid := range tables {
		healthy := trueRowSize(t, data, nid)
		silentN, loudN := 0, 0
		for v := 0; v < 65536; v += 53 {
			mut := patchRowSize(t, data, nid, uint16(v))
			silent, out := silentLoss(t, mut)
			if silent {
				silentN++
				if silentN <= 3 {
					t.Errorf("%s with row size %d (healthy %d): %d of 17 messages and no fault", name, v, healthy, len(out.msgs))
				}
			} else if len(out.faults) > 0 {
				loudN++
			}
		}
		if silentN > 0 {
			t.Errorf("%s: %d sampled row sizes lost messages silently (%d were loud)", name, silentN, loudN)
		}
	}
}

// covers: MA-316, R1
// K7 (N9, owner ruling J7), closed: a folder's MessageCount is the file's claim, and
// the file's own table is the file's content. A count that reads 0 over a table that
// lists rows no longer hides the folder: its messages are read. A count that says more
// than the table lists is a gap with the number missing (folder-partially-read, the
// count in Skipped, locator #messages-listed), and the rows the table does list are
// read. A count that says less is no loss (the rows are read).
func TestPSTCountLiesAreReadThroughTheTable(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	for _, tc := range []struct {
		name      string
		count     uint32
		missing   int // faults' Skipped, 0 = none
		wantFault bool
	}{
		{"zero over six rows", 0, 0, false},
		{"three over six rows", 3, 0, false},
		{"nine over six rows", 9, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mut := patchCell(t, data, topFolder, draftsFolder, 12, tc.count)
			out := walkOnce(t, openData(t, mut), WalkOptions{})
			if out.err != nil || len(out.msgs) != 17 {
				t.Fatalf("%d messages, err %v; want all 17 (Drafts' count says %d, its table lists 6)", len(out.msgs), out.err, tc.count)
			}
			if got, want := fmt.Sprint(out.nids(t)), fmt.Sprint(ref.messages); got != want {
				t.Errorf("messages %s; want %s", got, want)
			}
			if !tc.wantFault {
				if len(out.faults) != 0 {
					t.Errorf("faults %+v; want none: the table is the truth and was read", out.faults)
				}
				return
			}
			if len(out.faults) != 1 {
				t.Fatalf("faults %+v; want one", out.faults)
			}
			f := out.faults[0]
			if f.Reason != "folder-partially-read" || f.Scope != state.GapScopeFolder || f.Class != state.ClassTerminal || f.Skipped != tc.missing || f.Locator != "folder:Drafts#messages-listed" {
				t.Errorf("fault %+v; want a terminal folder-partially-read at Drafts counting %d missing", f, tc.missing)
			}
		})
	}
}

// covers: R1
// ESC-S2A-1 (open, NOT accepted): a sub-folder flag (PidTagSubfolders) that reads
// false over a folder whose hierarchy table lists sub-folders hides the whole subtree
// with no trace: go-pst's gate, kept by FC30 and pinned by
// TestPSTLoopsKeepEveryUpstreamGate, believes the flag. The healthy fixture itself
// holds hierarchy tables under flagged leaves (the three hidden folders below
// Recoverable Items), so "read the table when it exists" would also read those: a
// change of what is archived that the owner rules on. Closes when that is ruled; the
// walk then reads the table of every leaf and this test passes unchanged.
func TestHasSubFoldersFlagLieHidesASubtree(t *testing.T) {
	t.Skip("R1 (S2A-AC2 residual, ESC-S2A-1): a sub-folder flag that reads false over a hierarchy table that lists folders hides the subtree and nothing records it; reading leaf tables would also read the hidden Recoverable Items folders, which is the owner's call")
	data := fixtureData(t)
	mut := append([]byte(nil), data...)
	mut[19166] ^= 0x01 // the root's row for Top of Personal Folders: its flag, true -> false
	if silent, out := silentLoss(t, mut); silent {
		t.Errorf("%d of 17 messages read, %d faults: the subtree vanished silently", len(out.msgs), len(out.faults))
	}
}
