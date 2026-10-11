package source

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	pst "github.com/mooijtech/go-pst/v6/pkg"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// The PST reader owns the loops that go-pst's MessageIterator and
// Folder.GetSubFolders used to run (fault-isolation §4.4, F23, F27, FC30). Both
// stop the whole store at the first row that fails: Next() sets a sticky error and
// does not advance, and GetSubFolders returns no folder at all if one row of the
// table fails. These loops are copies of theirs, pinned at go-pst v6.0.2 (go.mod),
// that read every row by index, each under its own recover, so one bad row, node
// or folder costs exactly that. Every gate go-pst has is kept:
//   - Folder.HasSubFolders: a leaf folder has no hierarchy node to read;
//   - MessageCount > 0: a folder that says it holds no messages has no table;
//   - a search folder (IdentifierTypeSearchFolder) holds no messages of its own;
//   - IdentifierTypeNormalMessage: only a normal message's node is read as one.
// TestPSTLoopsMatchGoPstOnTheHealthyFixture shows the loops yield exactly go-pst's
// own folder and message sets on the healthy fixture.

const (
	// maxFolderDepth bounds the folder hierarchy (P3): the depth Exchange Online is
	// understood to publish. A real store nests nowhere near it; a deeper level is a
	// folder-too-deep gap and its subtree is not read.
	maxFolderDepth = 300
	// rowBreaker is how many STRUCTURALLY bad rows in a row close a table as one
	// folder fault (FC9). Real damage — a node that exists but cannot be read — never
	// counts: it costs its own ledger entry only.
	rowBreaker = 100
	// maxAggregateIDs bounds the NIDs a per-folder aggregate gap lists.
	maxAggregateIDs = 20

	// pidLtpRowID is the table property that carries a row's folder or message id
	// (0x67F2): the only column the message table asks for.
	pidLtpRowID = 26610
	// The hierarchy table's columns, as go-pst's GetSubFolders reads them.
	pidSubName     = 12289 // 0x3001 PidTagDisplayName
	pidSubCount    = 13826 // 0x3602 PidTagContentCount
	pidSubHasChild = 13834 // 0x360A PidTagSubfolders
)

// subRow is one row of a folder's hierarchy table; err is why it could not be read
// (a bad row costs that sub-folder only).
type subRow struct {
	id     pst.Identifier
	name   string
	count  int32
	hasSub bool
	// noSubCell: the row holds no sub-folder flag at all (its column is missing or
	// its cell is absent), so hasSub is go-pst's default, not the file's word.
	noSubCell bool
	err       error
}

// pstOps is what the walk asks of go-pst. fileOps is the production wiring; a
// test substitutes a table or a node that a byte patch cannot model precisely (a
// hundred bad rows, a hierarchy 301 deep) while everything else stays real.
type pstOps struct {
	messageTable  func(f *pst.Folder) (pst.TableContext, error)
	nodeExists    func(nid pst.Identifier) bool
	getMessage    func(nid pst.Identifier) (*pst.Message, error)
	subFolderRows func(f *pst.Folder) ([]subRow, error)
	// contentsRows says how many rows the message table the file holds for a folder
	// lists, for a folder whose own row SAYS it holds no messages: the row is the file's
	// claim and the table is the file's content, so a claim that the table contradicts
	// is a lie, and the table wins (K7). (0, nil) is "the file holds no such table or
	// an empty one"; an error means the table exists and cannot be read, and a
	// *tableGeometryError travels with the count. nil: no probe (a test's substitute),
	// which says nothing.
	contentsRows func(f *pst.Folder) (int, error)
}

func fileOps(file *pst.File) pstOps {
	return pstOps{
		messageTable: func(f *pst.Folder) (pst.TableContext, error) { return f.GetMessageTableContext() },
		nodeExists: func(nid pst.Identifier) bool {
			_, err := file.GetNodeBTreeNode(nid)
			return err == nil
		},
		getMessage:    file.GetMessage,
		subFolderRows: func(f *pst.Folder) ([]subRow, error) { return readSubFolderRows(file, f) },
		contentsRows: func(f *pst.Folder) (int, error) {
			return tableProbe(file, f.Identifier+12, func() (pst.TableContext, error) { return f.GetMessageTableContext() })
		},
	}
}

// readSubFolderRows reads a folder's hierarchy table the way GetSubFolders does,
// but a row that fails is returned as a row with an error instead of failing the
// table. A failure of the table itself is the returned error; a table whose header
// does not fit its columns is returned with its rows and a *tableGeometryError, and a
// folder that says it has sub-folders whose table lists none is an error: the rows
// the file holds are the truth, and none is not what the folder says.
func readSubFolderRows(file *pst.File, f *pst.Folder) (rows []subRow, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			rows, err = nil, fmt.Errorf("go-pst panicked reading the hierarchy table: %v", rec)
		}
	}()
	tc, err := f.GetSubFoldersTableContext()
	if err != nil {
		return nil, err
	}
	rows = make([]subRow, len(tc.Properties))
	for i, row := range tc.Properties {
		rows[i] = parseSubFolderRow(file, &tc, row)
	}
	geometry := checkTableGeometry(&tc)
	if len(rows) == 0 && f.HasSubFolders {
		if geometry != nil {
			return nil, fmt.Errorf("the folder says it has sub-folders but its table lists none: %w", geometry)
		}
		return nil, errors.New("the folder says it has sub-folders but its table lists none")
	}
	return rows, geometry
}

func parseSubFolderRow(file *pst.File, tc *pst.TableContext, row []pst.Property) (out subRow) {
	defer func() {
		if rec := recover(); rec != nil {
			out.err = fmt.Errorf("go-pst panicked reading the row: %v", rec)
		}
	}()
	sawID, sawSub := false, false
	defer func() { out.noSubCell = !sawSub }()
	for _, property := range row {
		pr, err := tc.GetPropertyReader(property)
		if err != nil {
			out.err = fmt.Errorf("property reader: %w", err)
			return out
		}
		switch property.ID {
		case pidSubName:
			// TODO in go-pst: this is String8 on an ANSI file; kept as upstream reads it.
			name, err := pr.GetString()
			if err != nil {
				out.err = fmt.Errorf("folder name: %w", err)
				return out
			}
			out.name = name
		case pidLtpRowID:
			id, err := pr.GetInteger32()
			if err != nil {
				out.err = fmt.Errorf("folder id: %w", err)
				return out
			}
			out.id, sawID = pst.Identifier(uint32(id)), true
		case pidSubCount:
			n, err := pr.GetInteger32()
			if err != nil {
				out.err = fmt.Errorf("message count: %w", err)
				return out
			}
			out.count = n
		case pidSubHasChild:
			b, err := pr.GetBoolean()
			if err != nil {
				out.err = fmt.Errorf("has sub-folders: %w", err)
				return out
			}
			out.hasSub, sawSub = b, true
		}
	}
	if !sawID {
		out.err = errors.New("the row holds no folder id")
	}
	return out
}

// pstWalk is one walk of one store.
type pstWalk struct {
	r       *Reader
	handler MessageHandler
	opts    WalkOptions
	sink    faultSink
	uid     string // "" = zero-content failures aggregate per folder (FC8)

	visited map[pst.Identifier]bool // folders reached, to refuse a cycle (F27)
	yielded map[pst.Identifier]bool // message NIDs already read, so a duplicate row is invalid (FC9)
}

func (r *Reader) newWalk(handler MessageHandler, opts WalkOptions) *pstWalk {
	uid := r.uid
	if opts.NoStoreUID {
		uid = ""
	}
	return &pstWalk{
		r: r, handler: handler, opts: opts, uid: uid,
		sink:    faultSink{cb: opts.Faults},
		visited: map[pst.Identifier]bool{},
		yielded: map[pst.Identifier]bool{},
	}
}

// run walks the store. A panic that escapes every inner recover (the traversal
// itself: GetRootFolder) is the last resort: a store-level error, returned with
// the faults already met, so those gaps are kept (FC29).
func (w *pstWalk) run() (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("go-pst panicked reading store %q: %v", w.r.store, rec)
		}
		err = w.sink.result(err)
	}()
	root, gerr := w.r.file.GetRootFolder()
	if gerr != nil {
		return fmt.Errorf("get root folder: %w", gerr)
	}
	return w.walk(&root, nil, 0)
}

func (w *pstWalk) abort() error {
	if w.opts.Abort != nil {
		return w.opts.Abort()
	}
	return nil
}

func (w *pstWalk) fault(f Fault) { w.sink.emit(f) }

// folderLocator is the locator of a folder-scope gap: the folder's path and the
// part of it that failed, so the entry is the same on every run that meets the
// same fault (and a message-table fault never shares an entry with a hierarchy
// one).
func folderLocator(path []string, part string) string {
	return "folder:" + strings.Join(path, "/") + "#" + part
}

// walk reads folder (at depth) and then its sub-folders.
func (w *pstWalk) walk(folder *pst.Folder, path []string, depth int) error {
	w.visited[folder.Identifier] = true
	folderPath := path
	if name := strings.TrimSpace(folder.Name); name != "" && !syntheticFolders[name] {
		folderPath = append(append([]string{}, path...), util.SanitizeSegment(name))
	}

	// go-pst's gate: a folder that says it holds no messages has no table to read. The
	// row is the file's CLAIM, though, and a claim the file's own table contradicts is
	// a lie that would hide the folder with no trace (K7, S2A-AC2): a folder that says
	// it is empty is read anyway when the file holds a table that lists rows for it.
	read := folder.MessageCount > 0
	if !read && folder.Identifier.GetType() != pst.IdentifierTypeSearchFolder {
		n, perr := w.probe(w.r.ops.contentsRows, folder)
		switch {
		case perr != nil && !isGeometry(perr):
			w.fault(Fault{Reason: "folder-unreadable", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
				Locator: folderLocator(folderPath, "messages"), Detail: "the folder says it holds no messages, but the message table the file holds for it could not be read: " + perr.Error()})
		case n > 0:
			read = true
		case perr != nil: // an empty table with a lying header: its rows may be hidden
			w.fault(Fault{Reason: "folder-partially-read", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
				Locator: folderLocator(folderPath, "messages-header"), Detail: perr.Error()})
		}
	}
	if read {
		if err := w.walkMessages(folder, folderPath); err != nil {
			return err
		}
	}
	// go-pst's gate: a leaf folder has no hierarchy node, so reading its table would
	// fail for a folder that is simply fine. Unlike the messages' gate this one is
	// kept as it is: a healthy file also holds hierarchy tables under flagged leaves
	// (the hidden folders below Recoverable Items), which go-pst has never read and
	// FC30 keeps unread, so a flag that reads false is believed. A flag that lies is
	// the owner's to rule on (ESC-S2A-1); a flag whose whole COLUMN is gone is caught
	// below, in the table that lost it.
	if !folder.HasSubFolders {
		return nil
	}
	rows, err := w.subFolderRows(folder)
	if isGeometry(err) {
		// The rows are still read; the lie is recorded, because rows may be missing.
		w.fault(Fault{Reason: "folder-partially-read", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "subfolders-header"), Detail: err.Error()})
		err = nil
	}
	if err != nil {
		// The sub-tree is lost, never the siblings: the parent's loop goes on.
		w.fault(Fault{Reason: "folder-unreadable", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "subfolders"), Detail: "the sub-folder table could not be read: " + err.Error()})
		return nil
	}
	// A table whose rows carry no sub-folder flag AT ALL has lost that column (its
	// descriptor is damaged, S2A-AC2): every folder below would read as a leaf, with
	// nothing recorded. A healthy table has the flag for most rows.
	readable, noFlag := 0, 0
	for _, row := range rows {
		if row.err == nil {
			readable++
			if row.noSubCell {
				noFlag++
			}
		}
	}
	if readable > 0 && noFlag == readable {
		w.fault(Fault{Reason: "folder-partially-read", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "subfolders-columns"),
			Detail: fmt.Sprintf("none of the %d sub-folder rows carries the sub-folder flag: its column is damaged, so the folders below these may not be listed", readable)})
	}
	streak := 0
	for i, row := range rows {
		if err := w.abort(); err != nil {
			return err
		}
		if row.err != nil {
			streak++
			if streak >= rowBreaker {
				w.fault(Fault{Reason: "folder-partially-read", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
					Locator: folderLocator(folderPath, "subfolders"), Skipped: len(rows) - i,
					Detail: fmt.Sprintf("%d sub-folder rows in a row could not be read; the rest of the table was not read", rowBreaker)})
				return nil
			}
			w.fault(Fault{Reason: "folder-unreadable", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
				Locator: folderLocator(folderPath, "subfolders/row"+strconv.Itoa(i)), Detail: "a sub-folder row could not be read: " + row.err.Error()})
			continue
		}
		streak = 0
		childPath := append(append([]string{}, folderPath...), util.SanitizeSegment(strings.TrimSpace(row.name)))
		switch {
		case w.visited[row.id]:
			w.fault(Fault{Reason: "folder-cycle", Scope: scopeFolder, Class: classTerm, Folder: childPath,
				Locator: folderLocator(childPath, "cycle"), Detail: fmt.Sprintf("the folder tree loops back to folder %d, which was already read", row.id)})
			continue
		case depth+1 > maxFolderDepth:
			w.fault(Fault{Reason: "folder-too-deep", Scope: scopeFolder, Class: classTerm, Folder: childPath,
				Locator: folderLocator(childPath, "deep"), Detail: fmt.Sprintf("the folder is nested more than %d levels deep; it and the folders below it were not read", maxFolderDepth)})
			continue
		}
		child := pst.Folder{Identifier: row.id, Name: row.name, HasSubFolders: row.hasSub, MessageCount: row.count, File: w.r.file}
		if err := w.walk(&child, folderPath, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (w *pstWalk) subFolderRows(f *pst.Folder) (rows []subRow, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			rows, err = nil, fmt.Errorf("go-pst panicked reading the hierarchy table: %v", rec)
		}
	}()
	return w.r.ops.subFolderRows(f)
}

// probe asks one of the ops' table probes (nil: none, which says nothing) under its
// own recover, like every other call into go-pst.
func (w *pstWalk) probe(fn func(f *pst.Folder) (int, error), f *pst.Folder) (n int, err error) {
	if fn == nil {
		return 0, nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			n, err = 0, fmt.Errorf("go-pst panicked probing the table: %v", rec)
		}
	}()
	return fn(f)
}

// isGeometry says err is (or wraps) a table header that does not fit its columns.
func isGeometry(err error) bool {
	var ge *tableGeometryError
	return errors.As(err, &ge)
}

func (w *pstWalk) messageTable(f *pst.Folder) (tc pst.TableContext, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			tc, err = pst.TableContext{}, fmt.Errorf("go-pst panicked reading the message table: %v", rec)
		}
	}()
	return w.r.ops.messageTable(f)
}

// aggregate counts what a folder lost without listing it, with a bounded sample.
type aggregate struct {
	count int
	ids   []string
}

func (a *aggregate) add(id string) {
	a.count++
	if len(a.ids) < maxAggregateIDs {
		a.ids = append(a.ids, id)
	}
}

func (a *aggregate) detail(what, ids string) string {
	d := fmt.Sprintf("%d %s; %s %s", a.count, what, ids, strings.Join(a.ids, ", "))
	if a.count > len(a.ids) {
		d += fmt.Sprintf(" (+%d more)", a.count-len(a.ids))
	}
	return d
}

// rowKind is what reading one row of a message table found.
type rowKind int

const (
	rowMessage    rowKind = iota // a message to hand to the handler
	rowNotMail                   // a node that is not a mail item (an appointment, a contact): skipped
	rowUnreadable                // STRUCTURAL: the row's NID cannot be read
	rowInvalid                   // STRUCTURAL: not a normal message's NID, already read, or absent from the node B-tree
	rowDamaged                   // a real node whose data cannot be read: its own gap, never counted
)

type rowResult struct {
	kind rowKind
	nid  pst.Identifier
	msg  *model.Message
	err  error
}

// walkMessages reads a folder's message table by row index. It never calls
// Next(), so the sticky error that ended a store at the first bad row cannot
// arise (H1).
func (w *pstWalk) walkMessages(f *pst.Folder, folderPath []string) error {
	if f.Identifier.GetType() == pst.IdentifierTypeSearchFolder {
		return nil // go-pst's gate: a search folder holds no messages of its own
	}
	tc, err := w.messageTable(f)
	if err != nil {
		w.fault(Fault{Reason: "folder-unreadable", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "messages"), Detail: "the message table could not be read: " + err.Error()})
		return nil
	}
	// A table that lists fewer rows than its folder says it holds, or whose header
	// does not fit its columns, has lost rows with no error (S2A-AO2): the rows that
	// are listed are read, and the folder says what the table does not.
	rows := tc.Properties
	if gerr := checkTableGeometry(&tc); gerr != nil {
		w.fault(Fault{Reason: "folder-partially-read", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "messages-header"), Detail: gerr.Error()})
	}
	if f.MessageCount > 0 && len(rows) < int(f.MessageCount) {
		w.fault(Fault{Reason: "folder-partially-read", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "messages-listed"), Skipped: int(f.MessageCount) - len(rows),
			Detail: fmt.Sprintf("the folder says it holds %d messages but its table lists %d", f.MessageCount, len(rows))})
	}
	var unreadable, invalid, damaged aggregate
	streak, skipped := 0, 0
	for i := range rows {
		if err := w.abort(); err != nil {
			return err
		}
		res := w.readRow(&tc, rows[i])
		switch res.kind {
		case rowUnreadable, rowInvalid:
			streak++
			if streak >= rowBreaker {
				skipped = len(rows) - i
			} else if res.kind == rowUnreadable {
				unreadable.add("row " + strconv.Itoa(i))
			} else {
				invalid.add(strconv.FormatInt(int64(res.nid), 10))
			}
		case rowDamaged:
			// Real damage: not structure, so it neither counts toward the breaker nor
			// resets it. It costs its own entry (a store with a uid) or a count.
			if w.uid != "" {
				w.fault(Fault{Reason: "pst-message-unreadable", Scope: scopeItem, Class: classTerm, Folder: folderPath,
					Locator: w.nidLocator(res.nid), Detail: errText(res.err)})
			} else {
				damaged.add(strconv.FormatInt(int64(res.nid), 10))
			}
		case rowNotMail:
			streak = 0
		case rowMessage:
			streak = 0
			if err := w.handler(folderPath, res.msg); err != nil {
				return err
			}
		}
		if skipped > 0 {
			break
		}
	}
	if unreadable.count > 0 {
		w.fault(Fault{Reason: "pst-row-unreadable", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "rows-unreadable"), Skipped: unreadable.count,
			Detail: unreadable.detail("table row(s) whose message id could not be read", "at")})
	}
	if invalid.count > 0 {
		w.fault(Fault{Reason: "pst-row-invalid", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "rows-invalid"), Skipped: invalid.count,
			Detail: invalid.detail("table row(s) that name no readable message", "NIDs")})
	}
	if damaged.count > 0 {
		w.fault(Fault{Reason: "pst-messages-unreadable", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "nodes"), Skipped: damaged.count,
			Detail: damaged.detail("message(s) could not be read", "NIDs")})
	}
	if skipped > 0 {
		w.fault(Fault{Reason: "folder-partially-read", Scope: scopeFolder, Class: classTerm, Folder: folderPath,
			Locator: folderLocator(folderPath, "messages"), Skipped: skipped,
			Detail: fmt.Sprintf("%d table rows in a row were not a readable message; the rest of the table was not read", rowBreaker)})
	}
	return nil
}

// nidLocator is the locator of one PST item: its store's uid and its NID.
func (w *pstWalk) nidLocator(nid pst.Identifier) string {
	if w.uid == "" {
		return ""
	}
	return "pst-nid:" + w.uid + "/" + strconv.FormatInt(int64(nid), 10)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// readRow reads one row under its own recover: a panic in go-pst costs this row.
func (w *pstWalk) readRow(tc *pst.TableContext, row []pst.Property) (res rowResult) {
	stage := rowUnreadable
	defer func() {
		if rec := recover(); rec != nil {
			res = rowResult{kind: stage, nid: res.nid, err: fmt.Errorf("go-pst panicked reading the row: %v", rec)}
		}
	}()

	// 1. The row's NID.
	var prop pst.Property
	found := false
	for _, p := range row {
		if p.ID == pidLtpRowID {
			prop, found = p, true
			break
		}
	}
	if !found {
		return rowResult{kind: rowUnreadable, err: errors.New("the row holds no message id")}
	}
	pr, err := tc.GetPropertyReader(prop)
	if err != nil {
		return rowResult{kind: rowUnreadable, err: err}
	}
	v, err := pr.GetInteger32()
	if err != nil {
		return rowResult{kind: rowUnreadable, err: err}
	}
	nid := pst.Identifier(uint32(v))
	res.nid = nid

	// 2. The NID must name a normal message, once, that the node B-tree holds.
	switch {
	case nid.GetType() != pst.IdentifierTypeNormalMessage:
		return rowResult{kind: rowInvalid, nid: nid, err: errors.New("not a normal message's id")}
	case w.yielded[nid]:
		return rowResult{kind: rowInvalid, nid: nid, err: errors.New("already read in this store")}
	case !w.r.ops.nodeExists(nid):
		return rowResult{kind: rowInvalid, nid: nid, err: errors.New("absent from the node B-tree")}
	}
	w.yielded[nid] = true

	// 3. The node. From here a failure is real damage to a node that exists.
	stage = rowDamaged
	m, err := w.r.ops.getMessage(nid)
	if err != nil {
		return rowResult{kind: rowDamaged, nid: nid, err: err}
	}
	msg, err := convertMessage(m, w.r.catPropID, w.r.hasCatProp, w.nidLocator(nid))
	if err != nil {
		return rowResult{kind: rowDamaged, nid: nid, err: err}
	}
	if msg == nil {
		return rowResult{kind: rowNotMail, nid: nid}
	}
	return rowResult{kind: rowMessage, nid: nid, msg: msg}
}
