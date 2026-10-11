package source

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	pst "github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// The fixture's folders, as go-pst reads them (see TestPSTLoopsMatchGoPstOnTheHealthyFixture):
// ROOT_FOLDER (290) > "Top of Personal Folders" (32802) > Drafts (33058, 6 messages),
// "Sent Messages" (33346, 11 messages) and a dozen empty ones; plus a search folder.
const (
	rootFolder   pst.Identifier = 290
	topFolder    pst.Identifier = 32802
	draftsFolder pst.Identifier = 33058
	sentFolder   pst.Identifier = 33346
	recovFolder  pst.Identifier = 33410
)

// covers: MA-315, R10, R1, S46
// PST per-row isolation (H1, FC7): the F23 PoC's corrupted copies of support.pst —
// the message block at #0, #3 or #11 broken — each yield 16 real messages plus ONE
// pst-message-unreadable gap whose locator is pst-nid:<store-uid>/<nid>, and no stub:
// the loop reads every row by index, never calling Next(), so the sticky error that
// ended the store (go-pst's own loop yields 0, 3 and 11 messages) cannot arise.
// Twin first: the uncorrupted fixture yields 17 and no gap.
func TestPSTMessageRowCorruptionCostsOneItem(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	r0 := openData(t, data)
	uid := assure.Reached(t, r0.uid, "the fixture's store uid")

	twin := walkOnce(t, r0, WalkOptions{})
	if twin.err != nil || len(twin.msgs) != 17 || len(twin.faults) != 0 {
		t.Fatalf("twin: the healthy fixture yielded %d messages, %d faults, err %v; want 17, 0, nil", len(twin.msgs), len(twin.faults), twin.err)
	}

	for _, idx := range []int{0, 3, 11} {
		nid := ref.messages[idx]
		mut := corruptMessage(t, data, nid)
		// What the replaced loop did with this very file (F23): it ended the store at
		// the bad row, so it yielded exactly the messages before it.
		if n, stopped := goPstYield(mut); !stopped || n != idx {
			t.Fatalf("precondition: go-pst's own loop over the copy with #%d corrupted yielded %d and stopped=%v; the F23 PoC yields %d and stops", idx, n, stopped, idx)
		}

		out := walkOnce(t, openData(t, mut), WalkOptions{})
		if out.err != nil || len(out.msgs) != 16 || len(out.faults) != 1 {
			t.Fatalf("#%d corrupted: %d messages, %d faults, err %v; want 16 real messages, 1 gap, nil", idx, len(out.msgs), len(out.faults), out.err)
		}
		f := out.faults[0]
		want := fmt.Sprintf("pst-nid:%s/%d", uid, nid)
		if f.Reason != "pst-message-unreadable" || f.Scope != state.GapScopeItem || f.Class != state.ClassTerminal || f.Locator != want {
			t.Errorf("#%d: fault %+v; want a terminal item gap pst-message-unreadable at %s", idx, f, want)
		}
		if f.Detail == "" {
			t.Errorf("#%d: the gap carries no cause", idx)
		}
		for _, m := range out.msgs {
			if m.Subject == "(unreadable message)" || nidOf(t, m.Locator) == nid {
				t.Errorf("#%d: the corrupted node still reached the handler as %q (a stub): FC7 forbids it", idx, m.Subject)
			}
		}
	}
}

// covers: MA-315, MA-319, R10, R1, S46
// A store whose identity cannot be read (a store with no PidTagRecordKey, an
// -outlook scratch PST rebuilt every run, an .ost Outlook rebuilds in place) counts
// its zero-content failures in ONE per-folder aggregate gap, pst-messages-unreadable,
// with a count and a bounded NID list, stable across NID churn (FC8): no item gap,
// no locator that could ever match another store's item.
func TestPSTAggregatesWhereTheStoreHasNoStableIdentity(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	nid := ref.messages[3]
	mut := corruptMessage(t, data, nid)

	for name, setup := range map[string]func(*Reader) WalkOptions{
		"the caller says the store is rebuilt": func(*Reader) WalkOptions { return WalkOptions{NoStoreUID: true} },
		"the store has no readable uid":        func(r *Reader) WalkOptions { r.uid = ""; return WalkOptions{} },
	} {
		r := openData(t, mut)
		out := walkOnce(t, r, setup(r))
		if out.err != nil || len(out.msgs) != 16 || len(out.faults) != 1 {
			t.Fatalf("%s: %d messages, %d faults, err %v; want 16, one aggregate, nil", name, len(out.msgs), len(out.faults), out.err)
		}
		f := out.faults[0]
		if f.Reason != "pst-messages-unreadable" || f.Scope != state.GapScopeFolder || f.Skipped != 1 || f.Class != state.ClassTerminal ||
			!strings.Contains(f.Detail, fmt.Sprint(nid)) || strings.Contains(f.Locator, "pst-nid:") {
			t.Errorf("%s: fault %+v; want a folder-scope pst-messages-unreadable counting 1, naming NID %d, with no pst-nid locator", name, f, nid)
		}
		for _, m := range out.msgs {
			if m.Locator != "" {
				t.Errorf("%s: a message carries locator %q although the store has no stable identity", name, m.Locator)
			}
		}
	}
}

// covers: MA-315, MA-319, R10, R1, S46
// S2a verifies that go-pst reads PidTagRecordKey (0x0FF9) on the fixture (FC8): if
// it could not, the per-folder aggregate would apply to every PST.
func TestPSTStoreUIDIsReadOnTheFixture(t *testing.T) {
	r := openData(t, fixtureData(t))
	if r.uid != "4207c2fa04ed104f963c71d91da319d8" {
		t.Errorf("the fixture's store uid = %q; go-pst v6.0.2 reads PidTagRecordKey as 4207c2fa04ed104f963c71d91da319d8", r.uid)
	}
	// An all-zero or over-long key is no identity.
	if allZero(make([]byte, 16)) != true || allZero([]byte{0, 1}) {
		t.Error("allZero is wrong")
	}
}

// covers: MA-315, R10, R1, S46
// The F23 PoC's 1656-mutation sweep of block #0 (every third byte of its 4968
// bytes XORed): every mutation yields 17 items, 17 real or 16 real + 1 gap; none
// aborts the walk, none hangs it, none yields a stub. The F23 emulation of the
// replaced loop gave 1563 at 17 and 93 at 16 (79 errors and 14 panics aborted the
// store); here every mutation that breaks go-pst's own read of the node is one
// gap, and a mutation that leaves it readable never costs more than that one item.
func TestPSTBlockSweepNeverAbortsOrHangs(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	nid := ref.messages[0]
	off, size := blockOf(t, data, nid)
	if size/3 != 1656 {
		t.Fatalf("block #0 is %d bytes; the F23 sweep XORs every third of 4968 bytes (1656 mutations)", size)
	}
	at17, at16, broken := 0, 0, 0
	for k := int64(0); k < size; k += 3 {
		data[off+k] ^= 0xFF
		r, err := openPSTReader(bytes.NewReader(data), nopCloser{}, "sweep.pst")
		if err != nil {
			t.Fatalf("mutation +%d: the file no longer opens: %v", k, err)
		}
		reads := goPstReads(data, nid)
		out := walkOnce(t, r, WalkOptions{})
		r.Close()
		data[off+k] ^= 0xFF

		items := 0
		for _, f := range out.faults {
			if f.Scope == state.GapScopeItem {
				items++
			}
		}
		switch {
		case out.err != nil:
			t.Fatalf("mutation +%d aborted the walk: %v", k, out.err)
		case len(out.msgs)+items != 17 || len(out.faults) != items:
			t.Fatalf("mutation +%d: %d messages + %d gaps (%d faults); want 17 items", k, len(out.msgs), items, len(out.faults))
		case !reads && items != 1:
			t.Fatalf("mutation +%d breaks go-pst's own read of the node but cost %d gaps; want exactly 1", k, items)
		}
		for _, m := range out.msgs {
			if m.Subject == "(unreadable message)" {
				t.Fatalf("mutation +%d yielded a stub", k)
			}
		}
		if items == 0 {
			at17++
		} else {
			at16++
		}
		if !reads {
			broken++
		}
	}
	t.Logf("1656 mutations: %d yield 17 real, %d yield 16 real + 1 gap (go-pst's own read broken in %d); F23's emulation of the old loop: 1563 and 93", at17, at16, broken)
	if at16 < broken {
		t.Errorf("only %d mutations produced a gap, but go-pst's own read of the node failed in %d", at16, broken)
	}
}

// covers: MA-315, R10, R1, S46
// FC30: the loops are copies of go-pst's, pinned at v6.0.2, and on the healthy
// fixture they yield EXACTLY go-pst's own folder set and message set — a gate
// dropped from the copy would show here or in the gates test below.
func TestPSTLoopsMatchGoPstOnTheHealthyFixture(t *testing.T) {
	gomod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), "github.com/mooijtech/go-pst/v6 v6.0.2") {
		t.Error("go-pst is no longer pinned at v6.0.2: the reader owns copies of its loops (FC30), which must be re-read against the new version before it moves")
	}
	data := fixtureData(t)
	ref := referenceOf(t, data)
	out := walkOnce(t, openData(t, data), WalkOptions{})
	if out.err != nil || len(out.faults) != 0 {
		t.Fatalf("healthy walk: err %v, faults %v", out.err, out.faults)
	}
	var visited []pst.Identifier
	for id := range out.w.visited {
		visited = append(visited, id)
	}
	if got, want := fmt.Sprint(sortedIDs(visited)), fmt.Sprint(sortedIDs(ref.folders)); got != want {
		t.Errorf("folders the loops reached\n got %s\nwant %s (go-pst's WalkFolders)", got, want)
	}
	if got, want := fmt.Sprint(out.nids(t)), fmt.Sprint(ref.messages); got != want {
		t.Errorf("messages the loops yielded, in order\n got %s\nwant %s (go-pst's MessageIterator)", got, want)
	}
}

// covers: MA-315, R10, R1, S46
// Every upstream gate is kept (FC30): a folder with no sub-folders has its
// hierarchy table left unread, a folder whose count is zero its message table, a
// search folder its messages even when its count says otherwise.
func TestPSTLoopsKeepEveryUpstreamGate(t *testing.T) {
	r := openData(t, fixtureData(t))
	real := r.ops
	searchID := pst.Identifier(77<<5 | 3)
	if searchID.GetType() != pst.IdentifierTypeSearchFolder {
		t.Fatal("test setup: not a search folder id")
	}
	known := map[pst.Identifier]subRow{}
	var tableCalls, subCalls []pst.Folder
	r.ops.messageTable = func(f *pst.Folder) (pst.TableContext, error) {
		tableCalls = append(tableCalls, *f)
		return real.messageTable(f)
	}
	r.ops.subFolderRows = func(f *pst.Folder) ([]subRow, error) {
		subCalls = append(subCalls, *f)
		rows, err := real.subFolderRows(f)
		if f.Identifier == topFolder {
			// A search folder whose count claims messages, and a leaf and an empty folder.
			rows = append(rows, subRow{id: searchID, name: "A search folder", count: 5})
		}
		for _, row := range rows {
			known[row.id] = row
		}
		return rows, err
	}
	out := walkOnce(t, r, WalkOptions{})
	if out.err != nil || len(out.faults) != 0 || len(out.msgs) != 17 {
		t.Fatalf("walk: %d messages, faults %v, err %v", len(out.msgs), out.faults, out.err)
	}
	for _, f := range tableCalls {
		if f.MessageCount <= 0 || f.Identifier.GetType() == pst.IdentifierTypeSearchFolder {
			t.Errorf("the message table of folder %d (count %d, type %d) was read: the MessageCount > 0 and search-folder gates are not kept", f.Identifier, f.MessageCount, f.Identifier.GetType())
		}
	}
	for _, f := range subCalls {
		if !f.HasSubFolders {
			t.Errorf("the hierarchy table of leaf folder %d was read: the HasSubFolders gate is not kept", f.Identifier)
		}
	}
	if len(tableCalls) != 2 || len(subCalls) < 2 {
		t.Errorf("message tables read for %d folders (want Drafts and Sent Messages), hierarchy tables for %d", len(tableCalls), len(subCalls))
	}
	if _, ok := known[searchID]; !ok {
		t.Error("test setup: the search folder row was not offered")
	}
}

// covers: MA-316, R10, R6, R1, S46
// A corrupted PST sub-folder table row costs that sub-folder only: the name cell of
// Drafts' row in the hierarchy table is broken (a real byte patch), so Drafts cannot
// be named — a folder-unreadable gap at its row — and its siblings, Sent Messages
// among them, are still walked (go-pst's GetSubFolders would have failed the whole
// table and with it every sibling).
func TestPSTSubFolderRowCostsThatSubFolderOnly(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	blockOff, row, dec := hierarchyRow(t, data, topFolder, draftsFolder)
	mut := append([]byte(nil), data...)
	for i := 8; i < 12; i++ { // the row's name cell
		mut[blockOff+int64(row+i)] = encode(dec[row+i] ^ 0xFF)
	}
	file, err := pst.New(bytes.NewReader(mut))
	if err != nil {
		t.Fatal(err)
	}
	top := pst.Folder{Identifier: topFolder, HasSubFolders: true, File: file}
	if _, gerr := top.GetSubFolders(); gerr == nil {
		t.Fatal("precondition: go-pst's own GetSubFolders should fail the whole table on the broken row")
	}

	out := walkOnce(t, openData(t, mut), WalkOptions{})
	if out.err != nil {
		t.Fatalf("walk: %v", out.err)
	}
	if len(out.faults) != 1 {
		t.Fatalf("faults %+v; want exactly one", out.faults)
	}
	f := out.faults[0]
	if f.Reason != "folder-unreadable" || f.Scope != state.GapScopeFolder || f.Class != state.ClassTerminal || !strings.Contains(f.Locator, "#subfolders/row") {
		t.Errorf("fault %+v; want a terminal folder-unreadable at the damaged row", f)
	}
	// Drafts' six messages are lost with its name; the other eleven are read.
	if got, want := fmt.Sprint(sortedIDs(out.nids(t))), fmt.Sprint(sortedIDs(ref.messages[6:])); got != want {
		t.Errorf("messages read %s; want the 11 of Sent Messages %s", got, want)
	}
}

// covers: MA-316, R10, R6, R1, S46
// A failure to read ONE folder's message table records folder-unreadable and the
// folder's sub-folders still walk (go-pst's iterator error ended the store). Real
// patch: the root's hierarchy row for "Top of Personal Folders" claims one message,
// so the walk asks for a message table that folder does not have; the 17 messages of
// the folders below it are all read.
func TestPSTMessageTableFailureKeepsTheSubFolders(t *testing.T) {
	data := fixtureData(t)
	mut := patchCell(t, data, rootFolder, topFolder, 12, 1)
	file, err := pst.New(bytes.NewReader(mut))
	if err != nil {
		t.Fatal(err)
	}
	root := pst.Folder{Identifier: rootFolder, HasSubFolders: true, File: file}
	subs, gerr := root.GetSubFolders()
	if gerr != nil || subs[0].Identifier != topFolder || subs[0].MessageCount != 1 {
		t.Fatalf("precondition: the patch must make Top report one message: %v %+v", gerr, subs)
	}
	out := walkOnce(t, openData(t, mut), WalkOptions{})
	if out.err != nil || len(out.msgs) != 17 {
		t.Fatalf("walk: %d messages, err %v; want all 17 below the folder whose table failed", len(out.msgs), out.err)
	}
	if len(out.faults) != 1 || out.faults[0].Reason != "folder-unreadable" || out.faults[0].Class != state.ClassTerminal || out.faults[0].Locator != "folder:#messages" ||
		!strings.Contains(out.faults[0].Detail, "message table") {
		t.Errorf("faults %+v; want one terminal folder-unreadable for the message table of Top", out.faults)
	}
}

// tableOf is a message table of rows, each holding one NID cell (inline data, so
// reading it needs no heap).
func tableOf(r *Reader, nids ...pst.Identifier) pst.TableContext {
	rows := make([][]pst.Property, len(nids))
	for i, nid := range nids {
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(nid))
		rows[i] = []pst.Property{{ID: pidLtpRowID, Type: pst.PropertyTypeInteger32, Data: buf}}
	}
	return pst.TableContext{Properties: rows, File: r.file}
}

// onlyFolder replaces the message table of one folder, delegating every other.
func onlyFolder(r *Reader, id pst.Identifier, table pst.TableContext) {
	real := r.ops.messageTable
	r.ops.messageTable = func(f *pst.Folder) (pst.TableContext, error) {
		if f.Identifier == id {
			return table, nil
		}
		return real(f)
	}
}

// bogus is the NID of a normal message the node B-tree does not hold.
func bogus(i int) pst.Identifier {
	return pst.Identifier((900000+i)<<5 | int(pst.IdentifierTypeNormalMessage))
}

// covers: MA-316, R10, R6, R1, S46
// The row breaker (FC9): 100 consecutive STRUCTURALLY bad rows — here NIDs that are
// not a normal message's — close the folder as ONE folder-partially-read with a count
// of the rows not read, after one aggregate gap that counts the bad rows; no further
// entries, no message, whatever follows. Another folder is unaffected.
func TestPSTHundredStructuralBadRowsAreOneFolderFault(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	r := openData(t, data)
	bad := make([]pst.Identifier, 100)
	for i := range bad {
		bad[i] = pst.Identifier(i<<5 | 2) // a folder's id, not a message's
	}
	onlyFolder(r, draftsFolder, tableOf(r, append(bad, ref.messages[:6]...)...))
	out := walkOnce(t, r, WalkOptions{})
	if out.err != nil || len(out.msgs) != 11 {
		t.Fatalf("walk: %d messages, err %v; want only Sent Messages' 11", len(out.msgs), out.err)
	}
	if len(out.faults) != 2 {
		t.Fatalf("faults %+v; want one aggregate and one folder-partially-read", out.faults)
	}
	agg, partial := out.faults[0], out.faults[1]
	if agg.Reason != "pst-row-invalid" || agg.Scope != state.GapScopeFolder || agg.Skipped != 99 {
		t.Errorf("aggregate %+v; want pst-row-invalid counting the 99 rows before the breaker", agg)
	}
	if partial.Reason != "folder-partially-read" || partial.Scope != state.GapScopeFolder || partial.Class != state.ClassTerminal || partial.Skipped != 106-99 {
		t.Errorf("breaker fault %+v; want folder-partially-read with Skipped = the 7 rows from the 100th on", partial)
	}
	// 99 rows is not enough to trip it: every row after is read.
	r2 := openData(t, data)
	onlyFolder(r2, draftsFolder, tableOf(r2, append(bad[:99], ref.messages[:6]...)...))
	out = walkOnce(t, r2, WalkOptions{})
	if len(out.msgs) != 17 || len(out.faults) != 1 || out.faults[0].Reason != "pst-row-invalid" || out.faults[0].Skipped != 99 {
		t.Errorf("99 bad rows: %d messages, faults %+v; want all 17 messages and the aggregate only", len(out.msgs), out.faults)
	}
}

// covers: MA-316, R10, R6, R1, S46
// FC9: an interleaved table — 99 bad rows, then a duplicate NID, repeated — is
// still closed by the breaker (a duplicate is structure, so it counts), and what it
// leaves in the ledger is bounded (an aggregate and one partial-read entry) with no
// record: a bogus or hostile table costs one folder entry, never one per row.
func TestPSTInterleavedBadRowsStayBounded(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	r := openData(t, data)
	nids := []pst.Identifier{ref.messages[0]}
	for g := 0; g < 5; g++ {
		for i := 0; i < 99; i++ {
			nids = append(nids, bogus(g*100+i))
		}
		nids = append(nids, ref.messages[0]) // the duplicate
	}
	onlyFolder(r, draftsFolder, tableOf(r, nids...))
	out := walkOnce(t, r, WalkOptions{})
	if out.err != nil || len(out.msgs) != 1+11 {
		t.Fatalf("walk: %d messages, err %v; want the first row's message and Sent Messages' 11", len(out.msgs), out.err)
	}
	if len(out.faults) != 2 || out.faults[0].Reason != "pst-row-invalid" || out.faults[1].Reason != "folder-partially-read" {
		t.Errorf("faults %+v; want one aggregate and one folder-partially-read, however many rows are bad", out.faults)
	}
	for _, f := range out.faults {
		if f.Scope != state.GapScopeFolder {
			t.Errorf("fault %+v is item scope: a bad row has no item to list", f)
		}
	}
}

// covers: MA-316, R10, R6, R1, S46
// FC9: a 150-row run of REAL damaged nodes in the middle of a folder is 150 gaps and
// every later row is archived: damage is not structure, it costs its own entry and
// never counts toward the breaker.
func TestPSTRunOfDamagedNodesDoesNotTripTheBreaker(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	r := openData(t, data)
	nodes := r.ops
	damaged := func(nid pst.Identifier) bool {
		return nid.GetType() == pst.IdentifierTypeNormalMessage && nid>>5 >= 500000
	}
	r.ops.nodeExists = func(nid pst.Identifier) bool { return damaged(nid) || nodes.nodeExists(nid) }
	r.ops.getMessage = func(nid pst.Identifier) (*pst.Message, error) {
		if damaged(nid) {
			return nil, fmt.Errorf("the node's block fails its checks")
		}
		return nodes.getMessage(nid)
	}
	nids := append([]pst.Identifier{}, ref.messages[:2]...)
	for i := 0; i < 150; i++ {
		nids = append(nids, pst.Identifier((500000+i)<<5|4))
	}
	nids = append(nids, ref.messages[2:6]...)
	onlyFolder(r, draftsFolder, tableOf(r, nids...))
	out := walkOnce(t, r, WalkOptions{})
	if out.err != nil || len(out.msgs) != 17 {
		t.Fatalf("walk: %d messages, err %v; want every real row read, before and after the run", len(out.msgs), out.err)
	}
	locators := map[string]bool{}
	for _, f := range out.faults {
		if f.Reason != "pst-message-unreadable" || f.Scope != state.GapScopeItem {
			t.Errorf("fault %+v; want only item gaps", f)
		}
		locators[f.Locator] = true
	}
	if len(out.faults) != 150 || len(locators) != 150 {
		t.Errorf("%d faults with %d distinct locators; want 150 and 150", len(out.faults), len(locators))
	}
}

// covers: MA-316, R10, R6, R1, S46
// FC9: a corrupt hierarchy table — 120 unreadable rows after five good ones — is
// closed by the same breaker: the good rows are walked, 99 bad rows are each a
// folder-unreadable gap and the 100th, with the rest, one folder-partially-read.
func TestPSTCorruptHierarchyTableIsClosedByTheBreaker(t *testing.T) {
	data := fixtureData(t)
	r := openData(t, data)
	real := r.ops.subFolderRows
	r.ops.subFolderRows = func(f *pst.Folder) ([]subRow, error) {
		rows, err := real(f)
		if f.Identifier != topFolder || err != nil {
			return rows, err
		}
		rows = rows[:5]
		for i := 0; i < 120; i++ {
			rows = append(rows, subRow{err: fmt.Errorf("row %d: no folder id", i)})
		}
		return rows, nil
	}
	out := walkOnce(t, r, WalkOptions{})
	if out.err != nil {
		t.Fatalf("walk: %v", out.err)
	}
	var unreadable, partial int
	for _, f := range out.faults {
		switch f.Reason {
		case "folder-unreadable":
			unreadable++
		case "folder-partially-read":
			partial++
			if f.Skipped != 120-99 || !strings.HasSuffix(f.Locator, "#subfolders") {
				t.Errorf("breaker fault %+v; want Skipped = 21 (the 100th bad row and the 20 after it)", f)
			}
		default:
			t.Errorf("unexpected fault %+v", f)
		}
	}
	if unreadable != 99 || partial != 1 {
		t.Errorf("%d folder-unreadable and %d folder-partially-read; want 99 and 1", unreadable, partial)
	}
}

// covers: MA-317, R10, S47
// PST cycle and depth (F27): the walk of a hierarchy whose child NID points back
// at the root, or at a parent, returns at once with a terminal folder-cycle gap and
// every distinct folder read once; a synthetic chain 301 deep is a folder-too-deep
// gap while one exactly at the cap completes. The walks run in a CHILD test process
// (os/exec of this test binary with -test.run), because a stack overflow is fatal:
// without the visited set or the depth cap the child dies, and this test fails
// cleanly instead of taking its binary with it.
func TestPSTCycleAndDepthAreBounded(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestPSTCycleWalkChild$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), "MAILARCHIVE_PST_CYCLE_CHILD=1")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if tail := len(text) - 1500; tail > 0 {
		text = "…" + text[tail:]
	}
	if ctx.Err() != nil {
		t.Fatalf("the child walk did not return within 60 s: a cycle with no visited set, or no depth cap\n%s", text)
	}
	if err != nil {
		t.Fatalf("the child walk failed (%v): a stack overflow is fatal, or an assertion failed\n%s", err, text)
	}
	if !strings.Contains(string(out), "--- PASS: TestPSTCycleWalkChild") || strings.Contains(string(out), "SKIP") {
		t.Errorf("the child did not run its walks:\n%s", text)
	}
}

// covers: MA-317, R10, S47
// The walks TestPSTCycleAndDepthAreBounded runs in a child process.
func TestPSTCycleWalkChild(t *testing.T) {
	if os.Getenv("MAILARCHIVE_PST_CYCLE_CHILD") != "1" {
		t.Skip("MA-317: run by TestPSTCycleAndDepthAreBounded in a child process")
	}
	data := fixtureData(t)
	ref := referenceOf(t, data)

	// The F27 PoC: the root's child NID rewritten to the root's own NID. The tree
	// below it is unreachable, but the walk RETURNS (the old one overflowed the stack
	// after 72 s) with the loop recorded.
	start := time.Now()
	out := walkOnce(t, openData(t, patchCell(t, data, rootFolder, topFolder, 0, uint32(rootFolder))), WalkOptions{})
	if d := time.Since(start); d > time.Second {
		t.Errorf("the PoC fixture took %v; want well under 1 s", d)
	}
	if out.err != nil || len(out.faults) != 1 || out.faults[0].Reason != "folder-cycle" || out.faults[0].Class != state.ClassTerminal || out.faults[0].Scope != state.GapScopeFolder {
		t.Errorf("PoC: err %v, faults %+v; want one terminal folder-cycle gap", out.err, out.faults)
	}

	// A back edge deeper down: Recoverable Items listed as its own grandparent. Every
	// distinct folder is read once — all 17 messages — and the edge is one gap.
	out = walkOnce(t, openData(t, patchCell(t, data, topFolder, recovFolder, 0, uint32(topFolder))), WalkOptions{})
	if out.err != nil || len(out.faults) != 1 || out.faults[0].Reason != "folder-cycle" {
		t.Errorf("back edge: err %v, faults %+v; want one folder-cycle gap", out.err, out.faults)
	}
	if got, want := fmt.Sprint(out.nids(t)), fmt.Sprint(ref.messages); got != want {
		t.Errorf("back edge: messages %s; want each of the 17 once, in order, %s", got, want)
	}

	// A chain of fresh folder ids: the cap is on depth, the root being 0.
	for depth, wantFault := range map[int]bool{maxFolderDepth: false, maxFolderDepth + 1: true} {
		r := openData(t, data)
		r.ops.subFolderRows = func(f *pst.Folder) ([]subRow, error) {
			d := 0
			if f.Identifier != rootFolder {
				d = int(f.Identifier) - 5000
			}
			if d >= depth {
				return nil, nil
			}
			return []subRow{{id: pst.Identifier(5000 + d + 1), name: fmt.Sprintf("f%d", d+1), hasSub: true}}, nil
		}
		out := walkOnce(t, r, WalkOptions{})
		deep := 0
		for _, f := range out.faults {
			if f.Reason == "folder-too-deep" && f.Class == state.ClassTerminal {
				deep++
			}
		}
		if out.err != nil || (deep == 1) != wantFault || len(out.faults) != deep {
			t.Errorf("a chain %d deep: err %v, faults %+v; want folder-too-deep: %v", depth, out.err, out.faults, wantFault)
		}
		if len(out.w.visited) != maxFolderDepth+1 {
			t.Errorf("a chain %d deep: %d folders read; want the root and %d levels", depth, len(out.w.visited), maxFolderDepth)
		}
	}
}

// covers: MA-318, R1, R3, R7, R10, S46
// Five unreadable PST nodes in one folder are FIVE gaps, never one collapsed stub
// (F33): the folder's other message is read, and no message carries the stub's
// subject.
func TestPSTFiveUnreadableNodesAreFiveGaps(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	mut := data
	for _, nid := range ref.messages[:5] {
		mut = corruptMessage(t, mut, nid)
	}
	out := walkOnce(t, openData(t, mut), WalkOptions{})
	if out.err != nil || len(out.msgs) != 12 || len(out.faults) != 5 {
		t.Fatalf("%d messages, %d faults, err %v; want 12, 5, nil", len(out.msgs), len(out.faults), out.err)
	}
	locators := map[string]bool{}
	for _, f := range out.faults {
		locators[f.Locator] = true
	}
	if len(locators) != 5 {
		t.Errorf("the five gaps share %d distinct locators; want five", len(locators))
	}
	for _, m := range out.msgs {
		if m.Subject == "(unreadable message)" {
			t.Error("a stub reached the handler")
		}
	}
}

// failOn makes a step of convertMessage fail for one NID's message only.
func failOnly(t *testing.T, nid pst.Identifier, step string, how func()) {
	t.Helper()
	switch step {
	case "body":
		old := readBody
		t.Cleanup(func() { readBody = old })
		readBody = func(m *pst.Message, mp *properties.Message, msg *model.Message) {
			if m.Identifier == nid {
				how()
			}
			old(m, mp, msg)
		}
	case "attachments":
		old := readAttachments
		t.Cleanup(func() { readAttachments = old })
		readAttachments = func(m *pst.Message) []model.Attachment {
			if m.Identifier == nid {
				how()
			}
			return old(m)
		}
	case "envelope":
		old := readEnvelope
		t.Cleanup(func() { readEnvelope = old })
		readEnvelope = func(m *pst.Message, mp *properties.Message, catID uint16, hasCat bool) (*model.Message, error) {
			if m.Identifier == nid {
				how()
			}
			return old(m, mp, catID, hasCat)
		}
	}
}

// covers: MA-318, R1, R3, R7, R10, S46
// The inward recovers (§4.4): each step of reading a message runs under its own
// recover, so a failure costs the smallest thing. A panic reading the BODY keeps the
// envelope (subject, sender, dates, attachments) with unreadable:body and the
// item's Locator; a failure of the ATTACHMENT TABLE (an error, a panic) keeps the
// body and sets no attachments, with unreadable:attachments, and never fails the
// message; a failure of the ENVELOPE is nothing reliable read: a gap and no message.
func TestPSTInwardRecoversKeepWhatWasRead(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	uid := openData(t, data).uid
	withAtt := ref.messages[11] // "IMPORTANT: Support portal downtime for maintenance", 2 attachments, an HTML body
	plain := ref.messages[0]

	healthy := walkOnce(t, openData(t, data), WalkOptions{})
	byNID := map[pst.Identifier]*model.Message{}
	for _, m := range healthy.msgs {
		byNID[nidOf(t, m.Locator)] = m
	}
	if h := byNID[withAtt]; h == nil || len(h.Attachments) != 2 || h.HTMLBody == "" || len(h.ReadGaps) != 0 {
		t.Fatalf("twin: the healthy message with attachments = %+v", h)
	}

	t.Run("body panic keeps the envelope and the attachments", func(t *testing.T) {
		failOnly(t, withAtt, "body", func() { panic("rtf: index out of range") })
		out := walkOnce(t, openData(t, data), WalkOptions{})
		m := outByNID(t, out)[withAtt]
		h := byNID[withAtt]
		if m == nil || len(out.faults) != 0 {
			t.Fatalf("message %v, faults %+v; want it kept and no gap", m, out.faults)
		}
		if m.Subject != h.Subject || m.SenderEmail != h.SenderEmail || !m.Received.Equal(h.Received) || len(m.Attachments) != 2 {
			t.Errorf("envelope or attachments lost: %+v", m)
		}
		if got := strings.Join(m.ReadGaps, ","); got != state.LabelUnreadableBody || m.Locator != fmt.Sprintf("pst-nid:%s/%d", uid, withAtt) ||
			!strings.Contains(m.ReadError, "rtf: index out of range") {
			t.Errorf("ReadGaps %q, Locator %q, ReadError %q; want unreadable:body, the item's locator and the cause", got, m.Locator, m.ReadError)
		}
	})
	t.Run("attachment table error keeps the body", func(t *testing.T) {
		failOnly(t, withAtt, "attachments", func() { panic(errStepFailed{fmt.Errorf("attachment table: bad checksum")}) })
		out := walkOnce(t, openData(t, data), WalkOptions{})
		m := outByNID(t, out)[withAtt]
		if m == nil || len(out.faults) != 0 {
			t.Fatalf("message %v, faults %+v; want it kept and no gap", m, out.faults)
		}
		if m.HTMLBody == "" || m.Subject == "" || len(m.Attachments) != 0 {
			t.Errorf("body lost or attachments kept: html %d bytes, %d attachments", len(m.HTMLBody), len(m.Attachments))
		}
		if got := strings.Join(m.ReadGaps, ","); got != state.LabelUnreadableAttachments || !strings.Contains(m.ReadError, "bad checksum") {
			t.Errorf("ReadGaps %q, ReadError %q; want unreadable:attachments and the cause", got, m.ReadError)
		}
	})
	t.Run("envelope failure is a gap and no message", func(t *testing.T) {
		failOnly(t, plain, "envelope", func() { panic("subject index out of range") })
		out := walkOnce(t, openData(t, data), WalkOptions{})
		if len(out.msgs) != 16 || len(out.faults) != 1 {
			t.Fatalf("%d messages, %d faults; want 16 and one gap", len(out.msgs), len(out.faults))
		}
		if f := out.faults[0]; f.Reason != "pst-message-unreadable" || f.Locator != fmt.Sprintf("pst-nid:%s/%d", uid, plain) || !strings.Contains(f.Detail, "subject index out of range") {
			t.Errorf("fault %+v; want pst-message-unreadable at the NID with the cause", f)
		}
	})
}

func outByNID(t *testing.T, o walkOut) map[pst.Identifier]*model.Message {
	t.Helper()
	m := map[pst.Identifier]*model.Message{}
	for _, msg := range o.msgs {
		m[nidOf(t, msg.Locator)] = msg
	}
	return m
}

// covers: MA-315, R10, R1, S46
// Reader faults reach the caller (FC29). A caller that registers no callback gets
// them as a *FaultsError when Walk returns — non-nil on purpose, so ignoring faults
// is loud — with the faults in order, at most 10 000 of them and the rest counted
// (Dropped); a walk that ends in Walk's outer recover (a panic in the traversal, here
// the handler's) returns the faults already met TOGETHER WITH the store-level error;
// and Abort, asked between rows, ends the walk with its error even where every row
// is unreadable and the handler is never reached.
func TestWalkFaultsReachACallerWithoutACallback(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	mut := corruptMessage(t, data, ref.messages[0])

	t.Run("no callback", func(t *testing.T) {
		var got int
		err := openData(t, mut).Walk(func(_ []string, m *model.Message) error { got++; return nil })
		var fe *FaultsError
		if !errors.As(err, &fe) || len(fe.Faults) != 1 || fe.Dropped != 0 || fe.Err != nil || got != 16 {
			t.Fatalf("Walk = %v, %d messages; want a *FaultsError holding the one fault and no store error", err, got)
		}
		if fe.Faults[0].Reason != "pst-message-unreadable" {
			t.Errorf("fault %+v", fe.Faults[0])
		}
		if healthy := openData(t, data).Walk(func([]string, *model.Message) error { return nil }); healthy != nil {
			t.Errorf("a healthy walk returned %v; want nil", healthy)
		}
	})
	t.Run("the list is bounded and the rest counted", func(t *testing.T) {
		r := openData(t, data)
		damaged := func(nid pst.Identifier) bool { return nid>>5 >= 700000 }
		real := r.ops
		r.ops.nodeExists = func(nid pst.Identifier) bool { return damaged(nid) || real.nodeExists(nid) }
		r.ops.getMessage = func(nid pst.Identifier) (*pst.Message, error) {
			if damaged(nid) {
				return nil, errors.New("damaged")
			}
			return real.getMessage(nid)
		}
		nids := make([]pst.Identifier, maxFaults+50)
		for i := range nids {
			nids[i] = pst.Identifier((700000+i)<<5 | 4)
		}
		onlyFolder(r, draftsFolder, tableOf(r, nids...))
		err := r.Walk(func([]string, *model.Message) error { return nil })
		var fe *FaultsError
		if !errors.As(err, &fe) || len(fe.Faults) != maxFaults || fe.Dropped != 50 {
			t.Fatalf("Walk = %v; want %d faults kept and 50 dropped", err, maxFaults)
		}
	})
	t.Run("a panic in the traversal keeps the faults already met", func(t *testing.T) {
		calls := 0
		err := openData(t, mut).Walk(func(_ []string, m *model.Message) error {
			if calls++; calls == 3 {
				panic("handler fault")
			}
			return nil
		})
		var fe *FaultsError
		if !errors.As(err, &fe) || len(fe.Faults) != 1 || fe.Err == nil || !strings.Contains(fe.Err.Error(), "handler fault") {
			t.Fatalf("Walk = %v; want the store-level error together with the one fault met before it", err)
		}
	})
	t.Run("Abort ends a stretch of unreadable rows", func(t *testing.T) {
		r := openData(t, data)
		onlyFolder(r, draftsFolder, tableOf(r, func() []pst.Identifier {
			n := make([]pst.Identifier, 50)
			for i := range n {
				n[i] = pst.Identifier(i<<5 | 2)
			}
			return n
		}()...))
		stop := errors.New("the run was cancelled")
		rows := 0
		err := r.WalkWithFaults(func([]string, *model.Message) error { return nil }, WalkOptions{
			Faults: func(Fault) {},
			Abort: func() error {
				if rows++; rows > 10 {
					return stop
				}
				return nil
			},
		})
		if !errors.Is(err, stop) {
			t.Errorf("Walk = %v; want the abort's error", err)
		}
	})
}

// covers: R1
// N9 (design 3, owner ruling J7): a folder whose corrupted MessageCount reads 0 hides
// its messages from every reader of go-pst, because the count is trusted: a trust gap
// of the PST library, OPEN and NOT accepted. The copy of support.pst whose Drafts row
// says 0 messages (a real byte patch) must still yield Drafts' six messages, or at
// least record that the folder holds more than its count says; today it yields neither.
func TestCorruptedMessageCountHidesAFolder(t *testing.T) {
	data := fixtureData(t)
	ref := referenceOf(t, data)
	mut := patchCell(t, data, topFolder, draftsFolder, 12, 0)
	out := walkOnce(t, openData(t, mut), WalkOptions{})
	if len(out.msgs) != 17 && len(out.faults) == 0 {
		t.Errorf("%d of %d messages read and no gap: the folder's messages vanished silently", len(out.msgs), len(ref.messages))
	}
}
