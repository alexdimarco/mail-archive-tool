package source

import (
	"encoding/binary"
	"errors"
	"fmt"

	pst "github.com/mooijtech/go-pst/v6/pkg"
)

// A table's header can lie without any read failing (S2A-AO2, S2A-AC2): go-pst
// derives a table's row count from the row size in its header (the size of the
// row matrix divided by it), so a 2-byte change reads a table as empty, or as
// shorter, and nothing errors. MS-PST lays a row out as its fixed-size columns
// followed by the cell existence bitmap (one bit per column), and the header names
// both ends: the bitmap starts at TCI_1b and the row ends at TCI_bm. A header whose
// row size is not "the end of the 1-byte columns plus the bitmap" does not describe
// the table it heads. Such a table is still read, as go-pst reads it, so nothing
// that was read before is lost; the walk records that its rows may not all be
// listed (a folder-partially-read gap), so a table that lies is never silent.

// tableGeometryError says a table's header does not fit its columns. It travels
// with the rows that were read, never instead of them.
type tableGeometryError struct {
	rowSize, columnsEnd int
}

func (e *tableGeometryError) Error() string {
	return fmt.Sprintf("the table's header does not fit its columns (row size %d, columns end at %d): its rows may not all be listed", e.rowSize, e.columnsEnd)
}

// tciHeaderLen is the TCINFO header that precedes the column descriptors: type,
// column count, the four end offsets, and the three heap ids.
const tciHeaderLen = 22

// checkTableGeometry reads the TCINFO header of tc and returns a
// *tableGeometryError when its row size is not the end of its columns' bitmap. A
// table that is not a real go-pst table context (a substitute made by a test) has
// no header and is sound.
func checkTableGeometry(tc *pst.TableContext) (err error) {
	defer func() {
		if recover() != nil {
			err = nil
		}
	}()
	if tc == nil || tc.HeapOnNode == nil || tc.HeapOnNode.Reader == nil || tc.File == nil {
		return nil
	}
	// go-pst read this very header to build tc, so a failure to read it again says
	// nothing about the geometry: it is not a lie.
	hid, err := tc.HeapOnNode.GetHIDUserRoot()
	if err != nil {
		return nil
	}
	rd, err := tc.File.GetHeapOnNodeReaderFromHNID(hid, *tc.HeapOnNode.Reader)
	if err != nil {
		return nil
	}
	hdr := make([]byte, tciHeaderLen)
	if _, err := rd.ReadAt(hdr, 0); err != nil {
		return nil
	}
	cols := int(hdr[1])
	end1b := int(binary.LittleEndian.Uint16(hdr[6:]))
	rowSize := int(binary.LittleEndian.Uint16(hdr[8:]))
	if want := end1b + (cols+7)/8; rowSize != want {
		return &tableGeometryError{rowSize: rowSize, columnsEnd: want}
	}
	return nil
}

// tableProbe says how many rows the table node the file holds for a folder lists,
// for a folder whose own row says it is empty. A folder whose file holds no such
// node is empty, as it says; an empty table (go-pst's ErrTableContextNoRows) lists
// none. A node that exists and cannot be read is the error. The table's header
// geometry is checked on the way: a *tableGeometryError comes back WITH the row
// count.
func tableProbe(file *pst.File, nid pst.Identifier, get func() (pst.TableContext, error)) (rows int, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			rows, err = 0, fmt.Errorf("go-pst panicked reading the table: %v", rec)
		}
	}()
	if _, nerr := file.GetNodeBTreeNode(nid); nerr != nil {
		return 0, nil
	}
	tc, gerr := get()
	if gerr != nil {
		if errors.Is(gerr, pst.ErrTableContextNoRows) {
			return 0, nil
		}
		return 0, gerr
	}
	return len(tc.Properties), checkTableGeometry(&tc)
}
