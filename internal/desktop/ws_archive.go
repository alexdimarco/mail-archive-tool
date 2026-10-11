package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/util"
)

// The archive source's endpoints (extraction design §4.2, §4.3, §4.6, §4.7, slice E4): the
// Source control's three choices (Microsoft 365, the configured archive, another archive folder,
// read-only [ruling W1]), the tree and counts from the archive's manifest snapshot, the message
// list from search.db one request at a time, and the plan resolved with the redaction belt. The
// folder is untrusted input: nothing in it is written, and every read goes through the engine's
// gates (copyexport_archive.go).

// wsSourceReq is the source a request names.
type wsSourceReq struct {
	Kind    string `json:"kind"`    // "m365" (or empty), "archive" (the configured archive) or "archive-other"
	Mailbox string `json:"mailbox"` // m365: the signer the selection was made under
	Archive string `json:"archive"` // archive: the folder the selection was made from, as "Load folders" reported it
}

func (s wsSourceReq) isArchive() bool { return s.Kind == "archive" || s.Kind == "archive-other" }

// pathKey is a path as two spellings of one place compare: absolute, cleaned and, where the
// file system folds case, lower-cased.
func pathKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}

func samePath(a, b string) bool { return pathKey(a) == pathKey(b) }

// archiveState describes the configured archive for the Source control's availability line. It
// reads nothing: one Lstat of the manifest.
func (d *dashboard) archiveState() map[string]any {
	out := d.cfg.effectiveOut()
	if out == "" || !app.ArchiveAt(out) {
		r := refusalO(out)
		return map[string]any{"state": "none", "path": out, "note": r.Text}
	}
	return map[string]any{"state": "ready", "path": out}
}

// otherPath checks the path typed or browsed for Another archive folder: a full path, with no NUL.
func (d *dashboard) otherPath(raw string) (string, *refusal) {
	p := strings.TrimSpace(raw)
	if p == "" || strings.ContainsRune(p, 0) || !filepath.IsAbs(p) {
		r := refusalO3Path(p, app.DestExampleFor(d.cfg.goos()))
		return "", &r
	}
	return filepath.Clean(p), nil
}

func storesOut(a *app.ArchiveSource) []map[string]any {
	var out []map[string]any
	for _, s := range a.Stores() {
		out = append(out, map[string]any{"store": s.Token, "mailbox": s.Mailbox, "label": s.Label()})
	}
	return out
}

// sourceOther is the Source action for {otherArchive: path}: it validates the folder as design
// §4.3 describes and returns its mailboxes, or R-o3, R-o4 or R-p. It reads in the folder and
// writes nothing there.
func (d *dashboard) sourceOther(w http.ResponseWriter, raw string) {
	path, ref := d.otherPath(raw)
	if ref != nil {
		d.refuse(w, *ref)
		return
	}
	if app.ArchiveAt(path) { // named for the Source control: Create folder keeps out of it from now on (E4-AI4)
		d.ws.rememberOther(path)
	}
	a, err := app.OpenArchive(path, app.ArchiveOptions{Other: true})
	if err != nil {
		d.refuse(w, *copyRefusal(err))
		return
	}
	defer a.Close()
	d.reply(w, map[string]any{"ok": true, "archive": map[string]any{"state": "ready", "path": a.Dir(), "other": true, "stores": storesOut(a), "records": a.Records()}})
}

// archiveFolders is "Load folders" for an archive source: the tree and its counts come from the
// manifest snapshot, read through the archive's root (XC20), and the snapshot is kept for the
// session. Nothing is written in the archive.
func (d *dashboard) archiveFolders(w http.ResponseWriter, r *http.Request, src wsSourceReq) {
	var path string
	other := src.Kind == "archive-other"
	if other {
		p, ref := d.otherPath(src.Archive)
		if ref != nil {
			d.refuse(w, *ref)
			return
		}
		path = p
		if app.ArchiveAt(path) {
			d.ws.rememberOther(path)
		}
	} else if path = d.cfg.effectiveOut(); path == "" {
		d.refuse(w, refusalO(""))
		return
	}
	d.ws.listMu.Lock()
	defer d.ws.listMu.Unlock()
	a, err := app.OpenArchive(path, app.ArchiveOptions{Other: other})
	if err != nil {
		d.refuse(w, *copyRefusal(err))
		return
	}
	var infos []graph.FolderInfo
	for _, n := range a.Tree() {
		infos = append(infos, graph.FolderInfo{ID: n.ID, ParentID: n.Parent, DisplayName: n.Name, TotalItemCount: n.Count})
	}
	t := buildTree(infos)
	sess := newArchSession(a, other, d.now())
	sess.setTree(t)
	d.ws.setArch(sess)
	labels := map[string]string{}
	for _, s := range a.Stores() {
		labels[s.Token] = s.Label()
	}
	out := make([]wsFolderOut, 0, len(t.order))
	for _, id := range t.order {
		f := t.byID[id]
		o := wsFolderOut{ID: f.ID, Parent: f.ParentID, Name: f.DisplayName, Count: f.TotalItemCount, Children: len(f.children), Label: labels[f.ID]}
		if t.byID[f.ParentID] == nil {
			o.Parent = ""
		}
		out = append(out, o)
	}
	d.reply(w, map[string]any{"ok": true, "archive": a.Dir(), "other": other, "stores": storesOut(a), "records": a.Records(), "folders": out})
}

func (ws *workspace) archNow() *wsSession {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.arch
}

// otherNow is the archive folder the workspace has open as its read-only source, or "".
func (ws *workspace) otherNow() string {
	if s := ws.archNow(); s != nil && s.archOther && s.archSource() != nil {
		return s.archPath
	}
	return ""
}

// maxOthers is how many archive folders the workspace remembers the owner named.
const maxOthers = 8

// rememberOther notes an archive folder the owner has named for Another archive folder, validated or
// loaded or not: nothing is ever written there (RR2-C13), so Create folder refuses it for as long as the
// page lives, not only while an archive session for it exists (E4-AI4).
func (ws *workspace) rememberOther(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for i, o := range ws.others {
		if samePath(o, path) {
			ws.others = append(ws.others[:i:i], ws.others[i+1:]...)
			break
		}
	}
	ws.others = append(ws.others, path)
	if len(ws.others) > maxOthers {
		ws.others = ws.others[len(ws.others)-maxOthers:]
	}
}

// otherPaths are the archive folders the owner has named, and the one open now, newest last.
func (ws *workspace) otherPaths() []string {
	ws.mu.Lock()
	out := append([]string(nil), ws.others...)
	ws.mu.Unlock()
	if now := ws.otherNow(); now != "" {
		out = append(out, now)
	}
	return out
}

// setArch makes s the archive session. The session it replaces is released unless a preview is
// still reading it: that one is left for its goroutine to finish.
func (ws *workspace) setArch(s *wsSession) {
	ws.mu.Lock()
	old, job := ws.arch, ws.job
	ws.arch = s
	ws.mu.Unlock()
	if old != nil && (job == nil || !job.running()) {
		old.release()
	}
}

// archiveSession is the archive session a selection's source names: a selection is bound to the
// archive it was made from, by path, never by the folder's dev+inode, which a remount changes
// [RR2-C4]. The configured archive moved by a Settings change, and another folder that is no
// longer at its path, are R-l2; a session that is not loaded (released after 30 idle minutes, or
// never) is the page being out of date, R-j2.
func (d *dashboard) archiveSession(src wsSourceReq) (*wsSession, *refusal) {
	s := d.ws.archNow()
	stale := func() (*wsSession, *refusal) {
		r := refusalJ2("folder")
		return nil, &r
	}
	if src.Kind == "archive" {
		cur := d.cfg.effectiveOut()
		was := src.Archive
		if strings.TrimSpace(was) == "" {
			was = cur
		}
		if !samePath(was, cur) {
			r := refusalL2Configured(was, cur)
			return nil, &r
		}
		if s == nil || s.archOther || s.archSource() == nil || !samePath(s.archPath, cur) {
			return stale()
		}
	} else {
		if !archiveStillAt(src.Archive) {
			r := refusalL2Other(src.Archive)
			return nil, &r
		}
		if s == nil || !s.archOther || s.archSource() == nil || !samePath(s.archPath, src.Archive) {
			return stale()
		}
	}
	s.touch(d.now())
	return s, nil
}

// archiveStillAt reports whether an archive folder is still at its path: a folder that holds its
// manifest.
func archiveStillAt(path string) bool { return strings.TrimSpace(path) != "" && app.ArchiveAt(path) }

// archiveRunSource opens the archive a preview was bound to for its run, after the same
// binding check Start repeats (R-l2).
func (d *dashboard) archiveRunSource(b wsBind) (*app.ArchiveSource, *refusal) {
	if b.other {
		if !archiveStillAt(b.path) {
			r := refusalL2Other(b.path)
			return nil, &r
		}
	} else if cur := d.cfg.effectiveOut(); !samePath(b.path, cur) {
		r := refusalL2Configured(b.path, cur)
		return nil, &r
	}
	a, err := app.OpenArchiveForRun(b.path, app.ArchiveOptions{Other: b.other})
	if err != nil {
		return nil, copyRefusal(err)
	}
	return a, nil
}

// beginRebuild is what the Rebuild job does first: it waits for the list request in flight to
// close search.db, then sets rebuilding, so that no list request opens it again until endRebuild
// (XC20: the workspace never holds search.db across requests, or inside Rebuild's rename window).
func (d *dashboard) beginRebuild() {
	d.listGate.Lock()
	d.rebuilding.Store(true)
	d.listGate.Unlock()
}

func (d *dashboard) endRebuild() { d.rebuilding.Store(false) }

// ---- the message list ----------------------------------------------------------------

const (
	listNoteRebuilding = "The search index is being rebuilt, so the message list is unavailable for a moment. Whole-folder selection works, or try again when Maintenance says the rebuild is done."
	listNoteWAL        = "Recent messages may be missing from this list; whole-folder selection includes them."
)

// listUnavailable words why an archive's message list cannot be shown while whole-folder
// selection still works from the manifest (F85). Rebuild search index (Maintenance) acts on the
// configured archive: for another archive folder the words do not send the owner to it (E4-AI8).
func listUnavailable(err error, other bool) string {
	var ue *index.ListUnavailableError
	switch {
	case errors.Is(err, os.ErrNotExist) && other:
		return "This archive folder has no search index, so there is no message list. Whole-folder selection works."
	case errors.Is(err, os.ErrNotExist):
		return "This archive has no search index, so there is no message list. Whole-folder selection works; Rebuild search index (Maintenance) brings the list back."
	case errors.As(err, &ue) && index.Busy(ue.Cause):
		return "The search index is in use right now, so the message list is unavailable for a moment. Whole-folder selection works; try the list again in a few seconds."
	case index.ReadOnly(err):
		return "The search index can't be opened on this medium, so there is no message list. Whole-folder selection works."
	case other:
		return "This archive folder's search index can't be used for a message list. Whole-folder selection works."
	}
	return "This archive's search index can't be used for a message list. Whole-folder selection works; Rebuild search index (Maintenance) fixes the list."
}

// rebuildAdvice is the clause that sends the owner to Maintenance's Rebuild, which acts on the configured
// archive: it is not said of another archive folder (E4-AI8).
func rebuildAdvice(other bool, clause string) string {
	if other {
		return "."
	}
	return clause
}

// archiveMessages is one page of an archive folder, newest first, at most 100 rows, from
// index.ListFolder (design §4.3): the index is opened for this request, read-only, and closed at its end.
func (d *dashboard) archiveMessages(w http.ResponseWriter, r *http.Request, in wsMessagesIn) {
	sess, ref := d.archiveSession(in.Source)
	if ref != nil {
		d.refuse(w, *ref)
		return
	}
	a, tree := sess.archSource(), sess.treeNow()
	var folder *wsFolder
	if tree != nil {
		folder = tree.byID[in.Folder]
	}
	if a == nil || folder == nil { // a folder the session's tree never listed: refused (XC17)
		d.refuse(w, refusalJ2("folder"))
		return
	}
	token, path, _ := a.Locate(in.Folder)
	out := map[string]any{"ok": true, "rows": []wsRowOut{}, "more": false, "count": folder.TotalItemCount}
	if path == "" && folder.ParentID == "" && len(folder.children) > 0 && folder.TotalItemCount == 0 {
		d.reply(w, out) // a store's own node holds no messages: its folders do
		return
	}
	d.ws.listMu.Lock()
	defer d.ws.listMu.Unlock()
	now := d.now()
	offset := 0
	if in.Cursor != "" {
		c, live := sess.takeCursor(in.Cursor, now)
		if !live || c.folder != in.Folder {
			d.refuse(w, refusalX(folder.DisplayName))
			return
		}
		offset = c.offset
	}
	d.listGate.RLock()
	defer d.listGate.RUnlock()
	if d.rebuilding.Load() {
		out["listUnavailable"] = listNoteRebuilding
		d.reply(w, out)
		return
	}
	h, err := a.OpenList()
	if err != nil {
		var ni *index.NewerIndexError
		if errors.As(err, &ni) {
			d.refuse(w, *copyRefusal(newerIndexRefusal(ni)))
			return
		}
		out["listUnavailable"] = listUnavailable(err, a.Other())
		d.reply(w, out)
		return
	}
	raw, total, err := h.ListFolder(token, path, offset, listPageRows)
	version := h.Version
	h.Close() // closed at the end of the request, never held (XC20)
	if err != nil {
		out["listUnavailable"] = listUnavailable(err, a.Other())
		d.reply(w, out)
		return
	}
	var rows []wsRowOut
	notInSnapshot := 0
	for _, row := range raw {
		key, ok := a.MapKey(version, row)
		if !ok {
			notInSnapshot++
			continue // a row the manifest does not list: it cannot be selected (R13, R21)
		}
		// the text of a row is the folder's: shown with no control or bidi control in it (E4-AO7)
		subj, from, fromName := plainRowText(row.Subject), plainRowText(row.SenderEmail), plainRowText(row.SenderName)
		hnd := sess.seen.add(seenEntry{id: key, folder: in.Folder, subject: subj, from: from, fromName: fromName,
			received: row.Date, size: -1})
		rows = append(rows, wsRowOut{H: hnd, Subject: subj, From: from, FromName: fromName, Received: row.Date.UTC().Format(time.RFC3339), Size: -1})
	}
	if len(raw) > 0 && len(rows) == 0 && offset == 0 && version < 2 {
		out["listUnavailable"] = "This archive's search index predates its format; whole-folder selection works" + rebuildAdvice(a.Other(), "; Rebuild search index fixes the list.")
		d.reply(w, out)
		return
	}
	if rows == nil {
		rows = []wsRowOut{}
	}
	out["rows"] = rows
	out["listed"] = total
	if next := offset + len(raw); next < total {
		out["more"] = true
		out["cursor"] = sess.newCursor(&wsCursor{folder: in.Folder, offset: next}, now)
	}
	var notes []string
	if total < folder.TotalItemCount {
		notes = append(notes, fmt.Sprintf("%s messages in this folder are not in the search index%s", commaInt(folder.TotalItemCount-total), rebuildAdvice(a.Other(), " — Rebuild search index fixes the list.")))
	}
	if a.Other() && a.WALBeside() {
		notes = append(notes, listNoteWAL)
	}
	// The list is the index as it is now, the tree the manifest as it was loaded (E4-AC5): a page that
	// holds rows the snapshot does not know (newest first, so they come first) is shown without them, and
	// the owner is told why, and that a list paged by offset may skip or repeat a message while the
	// archive changes.
	changed := false
	if size, mod, serr := a.StampNow(); serr == nil {
		if s0, m0 := a.Stamp(); size != s0 || !mod.Equal(m0) {
			changed = true
		}
	}
	if notInSnapshot > 0 {
		if changed {
			notes = append(notes, fmt.Sprintf("%s messages the search index lists for this folder are not shown: they are not in the archive as you loaded its folders (a backup or capture ran since). Load folders again to include them.", commaInt(notInSnapshot)))
		} else {
			// the manifest is as it was loaded, so loading again may include nothing: the index is written ahead of the
			// manifest while a capture runs (loading again, when it has finished, includes those), and it lists a
			// record the manifest no longer has until it is rebuilt (loading again never includes that one, and it
			// cannot be exported). Both causes are named, each with its remedy.
			notes = append(notes, fmt.Sprintf("%s messages the search index lists for this folder are not shown: the archive's manifest, as you loaded it, does not list them. A capture still running may have added them (Load folders again, when it has finished, includes them); a message removed from the archive stays in the search index until it is brought in step%s", commaInt(notInSnapshot),
				rebuildAdvice(a.Other(), " (Rebuild search index in Maintenance does that).")))
		}
	}
	if changed {
		notes = append(notes, "This archive changed since you loaded its folders (a backup or capture ran), so this list may skip or repeat a message. Load folders again.")
	}
	if len(notes) > 0 {
		out["listNote"] = strings.Join(notes, " ")
	}
	sess.touch(now)
	d.reply(w, out)
}

// plainRowText is a row's text as the page shows it.
func plainRowText(s string) string { return util.StripBidi(util.StripControl(s)) }

// newerIndexRefusal is R-p for a search.db a newer MailArchive wrote.
func newerIndexRefusal(ni *index.NewerIndexError) error {
	return &app.CopyRefusal{ID: "R-p", Message: fmt.Sprintf("The archive's search index was written by a newer MailArchive (format %d) — update MailArchive Desktop to export from it.", ni.Version)}
}

// ---- the preview ---------------------------------------------------------------------------

// noNames is what an export without the archive's index says of its names and its manifest.
const noNames = "most messages in this export carry no subject, sender or date in their names and in the manifest."

// metaUnavailable is the preview's warning that the index could not be read, in words true of the
// archive it is about (E4-AI8): an index a newer MailArchive wrote is said to be that (and to update, never to
// rebuild: a rebuild by this build would replace it with one of the older format), and another archive
// folder is never sent to Maintenance's Rebuild, which acts on the configured archive.
func metaUnavailable(err error, other bool) string {
	var ni *index.NewerIndexError
	switch {
	case errors.As(err, &ni):
		return fmt.Sprintf("The archive's search index was written by a newer MailArchive (format %d), so %s Update MailArchive Desktop to read it.", ni.Version, noNames)
	case other && errors.Is(err, os.ErrNotExist):
		return "This archive folder has no search index, so " + noNames
	case other:
		return "This archive folder's search index can't be read, so " + noNames
	}
	return "The archive's search index can't be read (" + strings.TrimSuffix(listUnavailable(err, false), ".") + "), so " + noNames
}

// archiveMeta reads what the index knows of each message of the selected folders (subject, sender,
// date), for the names and the manifest: one read-only open, closed before the plan is made. Without
// the index, the names carry what the manifest holds, and the preview says so.
func (d *dashboard) archiveMeta(ctx context.Context, a *app.ArchiveSource, ids []string) (map[string]index.ListRow, string, error) {
	d.listGate.RLock()
	defer d.listGate.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if d.rebuilding.Load() {
		return nil, "The search index is being rebuilt, so most messages in this export carry no subject, sender or date in their names and in the manifest.", nil
	}
	h, err := a.OpenList()
	if err != nil {
		return nil, metaUnavailable(err, a.Other()), nil
	}
	defer h.Close()
	meta := map[string]index.ListRow{}
	for _, id := range ids {
		token, path, ok := a.Locate(id)
		if !ok {
			continue
		}
		if err := h.EachInFolder(token, path, func(row index.ListRow) error {
			if err := ctx.Err(); err != nil { // a cancelled preview stops reading, and lets Rebuild in (E4-AC7)
				return err
			}
			if key, ok := a.MapKey(h.Version, row); ok {
				meta[key] = row
			}
			return nil
		}); err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return nil, "", cerr
			}
			return meta, "Part of the archive's search index can't be read, so some messages in this export carry no subject, sender or date in their names and in the manifest.", nil
		}
	}
	return meta, "", ctx.Err()
}

// resolveAndPlanArchive turns the selection into the plan (design §4.4, §4.7): the folder set from
// the snapshot's tree, each folder's records from the manifest minus the exclusions plus the picks,
// the redaction belt, then the engine sizes and names it. It reads the archive and writes nothing.
func (d *dashboard) resolveAndPlanArchive(ctx context.Context, job *wsPreviewJob, sess *wsSession, sel *wsSelection, opts app.CopyOptions) (*app.CopyPlan, map[string]any, *refusal) {
	a, tree := sess.archSource(), sess.treeNow()
	if a == nil || tree == nil {
		r := refusalJ2("folder")
		return nil, nil, &r
	}
	picks, excl, handles, ref := sess.resolveHandles(sel)
	if ref != nil {
		return nil, nil, ref
	}
	sess.seen.pin(handles)
	ids, _, bad := folderSet(sel, tree, tree)
	if bad {
		r := refusalJ2("folder")
		return nil, nil, &r
	}
	type found struct {
		folder   string
		subs     int
		reported int
		excluded int
	}
	var keys []string
	listed := map[string]bool{}
	var perFolder []found
	byID := map[string]int{}
	for i, id := range ids {
		f := tree.byID[id]
		job.set(func() { job.progress = fmt.Sprintf("Reading %s (%d of %d folders)", f.DisplayName, i+1, len(ids)) })
		fc := found{folder: strings.Join(folderNames(tree.path(id)), "/"), reported: f.TotalItemCount, subs: len(f.children)}
		for _, k := range a.Keys(id) {
			if listed[k] {
				continue
			}
			listed[k] = true
			if _, drop := excl[k]; drop {
				fc.excluded++
				continue
			}
			keys = append(keys, k)
		}
		byID[id] = len(perFolder)
		perFolder = append(perFolder, fc)
		if len(keys) > d.planLimit() {
			ref := refusalI(len(ids) == 1 && len(picks) == 0)
			return nil, nil, &ref
		}
	}
	pickedOutside := map[string]bool{}
	for _, e := range picks { // the individual picks outside the folder set
		if listed[e.id] {
			continue
		}
		listed[e.id] = true
		keys = append(keys, e.id)
		pickedOutside[e.id] = true
	}
	if len(keys) > d.planLimit() {
		ref := refusalI(len(ids) == 1 && len(picks) == 0)
		return nil, nil, &ref
	}
	job.set(func() { job.progress = "Reading the archive's search index…" })
	meta, metaNote, err := d.archiveMeta(ctx, a, ids)
	var res app.ArchiveResolved
	if err == nil {
		res, err = a.Messages(ctx, keys, meta, func(done, total int) {
			job.set(func() {
				job.progress = fmt.Sprintf("Checking the archive's files (%s of %s)", commaInt(done), commaInt(total))
			})
		})
	}
	if err != nil {
		r := refusedWith("R-timeout", "MailArchive stopped reading the archive before it finished — try Preview copy export again.")
		return nil, nil, &r
	}
	msgs := res.Messages
	order := map[string]int{}
	for i, id := range tree.order {
		order[id] = i
	}
	sort.SliceStable(msgs, func(i, j int) bool { // tree order, then newest first
		ai, bi := folderIndex(order, msgs[i]), folderIndex(order, msgs[j])
		if ai != bi {
			return ai < bi
		}
		return msgs[i].Received.After(msgs[j].Received)
	})
	// what was found in each folder, after the belt
	count := map[string]int{}
	picked := 0
	for _, m := range msgs {
		id := ""
		if len(m.Folder) > 0 {
			id = m.Folder[len(m.Folder)-1].ID
		}
		if _, in := byID[id]; in && !pickedOutside[m.SourceID] {
			count[id]++
		} else {
			picked++
		}
	}
	var planFolders []app.CopyPlanFolder
	var folderOut []foundOut
	reportedTotal := 0
	for _, id := range ids {
		fc := perFolder[byID[id]]
		planFolders = append(planFolders, app.CopyPlanFolder{Folder: fc.folder, Subfolders: fc.subs, Excluded: fc.excluded, Messages: count[id], Reported: fc.reported})
		folderOut = append(folderOut, foundOut{Folder: fc.folder, Reported: fc.reported, Found: count[id], Subs: fc.subs, Excluded: fc.excluded})
		reportedTotal += fc.reported
	}
	if picked > 0 {
		planFolders = append(planFolders, app.CopyPlanFolder{Folder: "Individually picked messages", Messages: picked, Reported: picked})
		folderOut = append(folderOut, foundOut{Folder: "Individually picked messages", Reported: picked, Found: picked})
		reportedTotal += picked
	}
	var warns, infos []string
	if res.Skipped > 0 {
		warns = append(warns, fmt.Sprintf("%s records whose files were removed are skipped%s", commaInt(res.Skipped),
			rebuildAdvice(a.Other(), " — run Rebuild search index (Maintenance) to bring the list and the counts back in step.")))
	}
	if metaNote != "" {
		warns = append(warns, metaNote)
	}
	// the snapshot is cached by the manifest's size and mtime (XC18): a capture that ran since "Load folders"
	// is not in it, and the preview says so instead of leaving the owner to wonder where a new message went
	if size, mod, serr := a.StampNow(); serr == nil {
		if s0, m0 := a.Stamp(); size != s0 || !mod.Equal(m0) {
			warns = append(warns, "This archive changed since you loaded its folders (a backup or capture ran), so messages added since are not in this selection. Load folders again to include them.")
		}
	}
	if a.Other() && a.WALBeside() {
		warns = append(warns, "A recent change in this archive folder is in a file MailArchive does not read from another archive folder, so the subject and date of recent messages may be missing from the export's names. The messages themselves are included.")
	}
	dest := strings.TrimSpace(sel.Dest)
	other := ""
	if a.Other() {
		other = a.Dir()
	}
	in := app.CopyPlanInput{Source: a.Describe(), Folders: planFolders, Messages: msgs, Options: opts, Dest: dest,
		Archives: d.archivesWith(other), OtherArchive: other, ArchiveSkipped: res.Skipped}
	plan, perr := d.copyEnv().PlanCopyExport(in)
	if perr != nil {
		return nil, nil, copyRefusal(perr)
	}
	infos = archiveInfos(plan.Archive, len(msgs))
	return plan, planResult(plan, job.id, folderOut, reportedTotal, len(msgs), warns, infos), nil
}

// archiveInfos are the preview's lines about what the archive holds (design §4.7 step 4, §7.4).
func archiveInfos(c *app.CopyArchiveCounts, selected int) []string {
	if c == nil {
		return nil
	}
	var out []string
	line := fmt.Sprintf("Of %s selected, %s have an original .eml", commaInt(selected), commaInt(c.WithOriginal))
	if c.WithoutOriginal > 0 {
		line += fmt.Sprintf("; %s will be copied as the archived page and attachment zip (no original was kept)", commaInt(c.WithoutOriginal))
	}
	out = append(out, line+".")
	if c.Gone > 0 {
		out = append(out, fmt.Sprintf("%s are no longer in the mailbox (kept in the archive).", commaInt(c.Gone)))
	}
	if c.Quarantined > 0 {
		out = append(out, fmt.Sprintf("%s archived pages don't begin with the security policy a page of MailArchive carries, or hold a live refresh, so they will be copied as text (.archived-page.txt) and not as pages.", commaInt(c.Quarantined)))
	}
	if c.Unattested > 0 {
		out = append(out, fmt.Sprintf("%s originals have no checksum recorded in the archive, so they are copied as stored (Verify records one).", commaInt(c.Unattested)))
	}
	return out
}

// ---- retry ------------------------------------------------------------------------------------

// retryArchive builds a retry preview of an archive export (design §4.13, XC2): the keys come from
// the run's record, never the destination's manifest, and each is resolved against what the archive
// holds now, so the plan's fixity is the archive's today and a record a capture rewrote since is
// exported as it is now. An archive that is not there is not an error to retry until it is back.
func (d *dashboard) retryArchive(w http.ResponseWriter, in runBody, info app.CopySourceInfo) {
	input, err := d.ex.env.RetryInput(in.RunID, in.Which, "")
	if err != nil {
		d.refuseRun(w, in.RunID, engineRefusal(err))
		return
	}
	if dest := strings.TrimSpace(in.Dest); dest != "" {
		input.Dest = dest
	}
	if !samePath(info.Archive, d.cfg.effectiveOut()) && !info.Other {
		r := refusalL2Configured(info.Archive, d.cfg.effectiveOut())
		d.refuseRun(w, in.RunID, r)
		return
	}
	if info.Other && app.ArchiveAt(info.Archive) { // a retry names the folder as the Source control does: a dashboard started since has not heard of it (E4-AI4)
		d.ws.rememberOther(info.Archive)
	}
	a, oerr := app.OpenArchive(info.Archive, app.ArchiveOptions{Other: info.Other})
	if oerr != nil {
		ref := *copyRefusal(oerr)
		if ref.ID == "R-o" || ref.ID == "R-o3" { // the archive is not there: reconnect it, then try again
			ref = refusedWith("R-o2", fmt.Sprintf("The archive at %s isn't available (it was disconnected or moved) — nothing was previewed. Reconnect it, then click '%s' again.", info.Archive, app.RetryButton(in.Which)))
		}
		d.refuseRun(w, in.RunID, ref)
		return
	}
	keys := make([]string, 0, len(input.Messages))
	was := map[string]app.CopyMessage{}
	for _, m := range input.Messages {
		keys = append(keys, m.SourceID)
		was[m.SourceID] = m
	}
	res, rerr := a.Messages(d.life.Context(), keys, nil, nil)
	if rerr != nil || len(keys) == 0 {
		a.Close()
		d.refuseRun(w, in.RunID, refusedWith("R-h", "None of those messages is in the archive any more — nothing was previewed."))
		return
	}
	for i := range res.Messages { // the names and the metadata the first export was made with
		m := &res.Messages[i]
		if old, ok := was[m.SourceID]; ok {
			m.Subject, m.From, m.FromName, m.Received = old.Subject, old.From, old.FromName, old.Received
		}
	}
	// A message the archive no longer has stays in the plan, as the first plan had it: the run lists it as
	// removed from the archive, which settles it for the whole set, where a message left out of every part
	// was offered by the first part's card for ever (E4-AC10). That is a message whose files were removed since
	// (a redaction: the run opens them behind the gate, and they are gone) and a record the manifest no longer
	// lists (it is marked, and none of its files is read, whatever is left of them). A retry of messages that
	// are all gone is a retry like any other, and settles them.
	vanished := map[string]bool{}
	for _, k := range res.Vanished {
		vanished[k] = true
	}
	msgs, resolved := make([]app.CopyMessage, 0, len(keys)), 0
	for _, k := range keys {
		switch {
		case resolved < len(res.Messages) && res.Messages[resolved].SourceID == k:
			msgs = append(msgs, res.Messages[resolved])
			resolved++
		case vanished[k]:
			msgs = append(msgs, was[k])
		default:
			m := was[k]
			m.Removed = true
			msgs = append(msgs, m)
		}
	}
	gone := len(keys) - len(res.Messages) // the messages the archive no longer has

	input.Messages, input.Folders = msgs, app.PlanFoldersOf(msgs)
	input.Source = a.Describe()
	other := ""
	if info.Other {
		other = a.Dir()
	}
	input.Archives, input.OtherArchive, input.ArchiveSkipped = d.archivesWith(other), other, 0
	bind := wsBind{arch: true, path: a.Dir(), other: info.Other}
	job := d.ws.startJob(func(ctx context.Context, job *wsPreviewJob) (*app.CopyPlan, map[string]any, *refusal) {
		defer a.Close()
		job.set(func() { job.which = in.Which })
		plan, perr := d.copyEnv().PlanCopyExport(*input)
		if perr != nil {
			return nil, nil, copyRefusal(perr)
		}
		var warns []string
		if gone > 0 {
			warns = append(warns, fmt.Sprintf("%s of these messages are no longer in the archive: they will be listed as removed from it and not exported.", commaInt(gone)))
		}
		out := planResult(plan, job.id, foldersOut(input.Folders), len(input.Messages), len(input.Messages), warns, archiveInfos(plan.Archive, len(input.Messages)))
		out["retryWhich"] = in.Which
		return plan, out, nil
	}, bind, in.RunID)
	d.reply(w, map[string]any{"ok": true, "previewId": job.id})
}
