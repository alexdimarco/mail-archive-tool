package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/util"
)

// The extraction workspace's endpoints (design/design-extraction-workspace.md §4.2-§4.8):
// the Graph session, the folder tree, the message list with its row handles, the counts,
// the preview, the native folder picker and Create folder. Every endpoint is a POST
// through GuardLocalPOST, reads included, because it returns mailbox content (P17, X13);
// mail-derived text reaches the page only as JSON strings (X15).

const (
	maxSelectionPicks = 10000   // picks plus exclusions per selection (R-j)
	maxBody           = 1 << 20 // every workspace request body
	previewTTL        = 30 * time.Minute
)

// workspace is the dashboard's extraction workspace state.
type workspace struct {
	d *dashboard

	mu         sync.Mutex
	sess       *wsSession
	arch       *wsSession   // the archive source's session (slice E4): the snapshot "Load folders" read
	idled      *idleRelease // the archive session the reaper let go of after 30 idle minutes, until the next Load folders
	gen        int          // counts dropSession calls: a session built across one is not kept (EW-AC5)
	job        *wsPreviewJob
	prev       *wsPreview
	pickerBusy bool
	others     []string // the archive folders the owner has named for Another archive folder, oldest first (E4-AI4)

	listMu     sync.Mutex // one listing at a time: the workspace stays inside the 4-concurrent budget (§4.14)
	reaperOnce sync.Once
}

// idleRelease is which archive session the reaper released: its folder, and whether it was another
// archive folder.
type idleRelease struct {
	path  string
	other bool
}

// idleReleased reports whether the archive at path (of this kind) is the session the reaper let go
// of for being idle, and nothing has loaded one since.
func (ws *workspace) idleReleased(path string, other bool) bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.idled != nil && ws.idled.other == other && samePath(ws.idled.path, path)
}

// wsBind is what a preview is bound to (design §4.7 step 5): the signer for a Microsoft 365 one,
// the archive folder, by path, for an archive one (MA-411, MA-507). Never the folder's dev+inode,
// which a remount changes [RR2-C4].
type wsBind struct {
	upn   string
	arch  bool
	path  string // the archive folder
	other bool   // another archive folder, read-only [ruling W1]
}

// wsPreview is the one live preview: the plan, bound to the signer it was made under, or to the
// archive it was made from.
type wsPreview struct {
	id      string
	plan    *app.CopyPlan
	upn     string
	bind    wsBind
	created time.Time
	retry   string // the run a retry preview was built from
}

func newWorkspace(d *dashboard) *workspace { return &workspace{d: d} }

// previewMessageCap is the most messages one export holds (R-i). A variable only so a test can
// lower it: serving 200,001 messages from the fake mailbox is not worth the time.
var previewMessageCap = 200000

// inRetryMode reports whether the preview the workspace holds, or the last one it was asked
// for, is a retry's: the page's buttons and so the words of a refusal differ (R-k).
func (ws *workspace) inRetryMode() bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.prev != nil {
		return ws.prev.retry != ""
	}
	return ws.job != nil && ws.job.retry != ""
}

// ---- the guarded endpoint shell ------------------------------------------------

// api wraps a workspace endpoint: POST only (405), the CSRF, Origin and Host guard (403,
// and after a restart the page is told so, R-y, never a bare 403: XC18), a 1 MiB body,
// JSON with no-store, and DC3's recover, so a crafted message or a bug never kills the
// dashboard.
func (d *dashboard) api(h func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Type", "application/json; charset=utf-8")
		hd.Set("Cache-Control", "no-store")
		hd.Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodPost {
			hd.Set("Allow", http.MethodPost)
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "POST only"})
			return
		}
		if !server.GuardLocalPOST(r, d.csrf) {
			if e := r.Header.Get("X-Epoch"); e != "" && e != d.boot && server.IsLoopback(r.Host) {
				ref := refusalY()
				writeJSONStatus(w, ref.Status, ref.body())
				return
			}
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"ok": false, "error": "refused: same-origin, tokened, local requests only"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		defer func() {
			if p := recover(); p != nil {
				writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "MailArchive Desktop stopped this request unexpectedly. Reload the page; nothing was changed."})
			}
		}()
		h(w, r)
	}
}

// readBody decodes the JSON body into v. A body over 1 MiB is refused as R-j (never a bare
// 413), and a malformed one as a plain refusal. ok is false when it has answered.
func (d *dashboard) readBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := decodeJSONBody(r, v); err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			ref := refusalJ413()
			writeJSONStatus(w, http.StatusRequestEntityTooLarge, ref.body())
			return false
		}
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "That request was not understood — reload the page and try again."})
		return false
	}
	return true
}

func refusalJ413() refusal {
	r := refusalJ(maxSelectionPicks)
	r.Text = "That selection is larger than MailArchive Desktop accepts in one request (1 MiB). " + r.Text
	return r
}

func (d *dashboard) reply(w http.ResponseWriter, v any) { writeJSON(w, v) }

func (d *dashboard) refuse(w http.ResponseWriter, ref refusal) {
	st := ref.Status
	if st == 0 {
		st = statusRefused
	}
	writeJSONStatus(w, st, ref.body())
}

// ---- the session ------------------------------------------------------------

// graphOptions are the options the workspace builds its Graph client from: the saved
// config's tenant and client id, the saved sign-in's location, and the endpoint
// overrides tests use. The sign-in mode in the config is ignored: the workspace is
// always the delegated client (P2, MA-506).
func (d *dashboard) graphOptions() (app.GraphOptions, *refusal) {
	c := d.cfg.load()
	if c == nil || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.ClientID) == "" {
		r := refusalA()
		return app.GraphOptions{}, &r
	}
	t := d.cfg.tokenCfg(c)
	return app.GraphOptions{Tenant: c.Tenant, ClientID: c.ClientID, TokenStore: t.TokenStore, TokenCachePath: t.TokenCachePath,
		BaseURL: d.cfg.BaseURL, TokenURL: d.cfg.TokenURL, DeviceAuthURL: d.cfg.DeviceAuthURL,
		RequestTimeout: d.cfg.RequestTimeout, MIMEIdleTimeout: d.cfg.MIMEIdleTimeout, MIMEHeaderTimeout: d.cfg.MIMEHeaderTimeout, MIMETimeout: d.cfg.MIMETimeout}, nil
}

// signedInNow is the saved sign-in, read locally (no network).
func (d *dashboard) signedInNow() (string, bool) {
	return d.cfg.signInState()
}

// session is the Graph session, built on first use from the saved sign-in on the
// process-lifetime context, never on a request's: NewDelegated keeps its context inside
// the refreshing token source, which oauth2 reuses on every refresh (XC6, MA-449).
func (ws *workspace) session(req context.Context) (*wsSession, *refusal) {
	d := ws.d
	_ = req // the request's context is for the request's own Graph calls, never for the session
	// A sign-in change that comes in while the session is being built (a GET /me over the
	// network, with the lock released) drops a session that is not stored yet; the build
	// would then be cached for the account that is no longer signed in. The count of drops
	// is read before the build and compared after it, and a build that crossed one is
	// thrown away and made again for whoever is signed in now (EW-AC5).
	for attempt := 0; attempt < 3; attempt++ {
		ws.mu.Lock()
		if ws.sess != nil {
			s := ws.sess
			ws.mu.Unlock()
			s.touch(d.now())
			return s, nil
		}
		gen := ws.gen
		ws.mu.Unlock()
		opts, ref := d.graphOptions()
		if ref != nil {
			return nil, ref
		}
		client, upn, err := app.OpenGraphSession(d.life.Context(), opts)
		if err != nil {
			r := graphRefusal(err, "your mailbox", d.cfgAuth())
			d.noteRefused(r)
			return nil, &r
		}
		s := newWsSession(client, upn, opts.Tenant, d.now())
		ws.mu.Lock()
		if ws.sess != nil { // another request built it first
			built := ws.sess
			ws.mu.Unlock()
			return built, nil
		}
		if ws.gen != gen { // the sign-in changed while this was built
			ws.mu.Unlock()
			continue
		}
		ws.sess = s
		ws.mu.Unlock()
		ws.reaperOnce.Do(ws.startReaper)
		return s, nil
	}
	r := refusalB() // the sign-in kept changing under the build: ask the user to sign in again
	return nil, &r
}

// startReaper releases an idle session's caches once a minute (XC18) until the process
// ends. It runs only on a lifecycle that can end; a test drives reap by hand.
func (ws *workspace) startReaper() {
	ctx := ws.d.life.Context()
	if ctx.Done() == nil {
		return
	}
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ws.reap(ws.d.now())
			}
		}
	}()
}

func (d *dashboard) cfgAuth() string {
	if c := d.cfg.load(); c != nil {
		return c.Auth
	}
	return ""
}

// dropSession forgets the session and cancels the preview job: a sign-in cleared or
// changed (XC7). A finished preview stays, bound to the account it was made under, so
// Start refuses it with R-l (or R-b when no one is signed in) naming the accounts; every
// outstanding selection is refused the same way.
func (ws *workspace) dropSession() {
	ws.mu.Lock()
	ws.sess = nil
	ws.gen++
	job := ws.job
	ws.mu.Unlock()
	if job != nil && !job.arch { // an archive preview reads no mailbox: it has no sign-in to go stale (E4-AI5)
		job.cancel()
	}
}

// reap releases the session's caches after 30 idle minutes with no live preview or run
// (XC18), and a preview that is past its 30 minutes.
func (ws *workspace) reap(now time.Time) {
	ws.mu.Lock()
	s, job, prev, a := ws.sess, ws.job, ws.prev, ws.arch
	if prev != nil && now.Sub(prev.created) > previewTTL {
		ws.prev = nil
	}
	ws.mu.Unlock()
	if (job != nil && job.running()) || ws.d.exportLive() {
		return
	}
	if s != nil && now.Sub(s.idleSince()) > sessionIdle {
		s.release()
	}
	if a != nil && now.Sub(a.idleSince()) > sessionIdle { // the archive snapshot goes too (XC18)
		a.release()
		ws.mu.Lock()
		if ws.arch == a {
			ws.arch = nil
			ws.idled = &idleRelease{path: a.archPath, other: a.archOther} // so the next click can say why (friction F15)
		}
		ws.mu.Unlock()
	}
}

// signerRefusal is the refusal of a selection or preview made under the account was,
// when the sign-in now is another or none: R-l names both accounts, R-b says to sign in.
func (d *dashboard) signerRefusal(was string) *refusal {
	now, ok := d.signedInNow()
	switch {
	case !ok:
		r := refusalB()
		return &r
	case !strings.EqualFold(now, was):
		r := refusalL(was, now)
		return &r
	}
	return nil
}

// ---- source, folders -----------------------------------------------------------

type wsSourceOut struct {
	OK       bool           `json:"ok"`
	Epoch    string         `json:"epoch"`
	M365     map[string]any `json:"m365"`
	Settings map[string]any `json:"settings"`
	Running  bool           `json:"running"`
	Account  string         `json:"account"`
	Archive  map[string]any `json:"archive"`
}

// sourceAction describes the sources' availability and the remembered settings. It reads
// the saved sign-in locally and calls nothing. With {otherArchive: path} (from Browse or typed)
// it validates that folder [ruling W1] and returns its mailboxes, or R-o3, R-o4 or R-p; it
// reads in that folder and writes nothing there.
func (d *dashboard) sourceAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OtherArchive string `json:"otherArchive"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.OtherArchive) != "" {
		d.sourceOther(w, in.OtherArchive)
		return
	}
	m := map[string]any{"state": "not-configured"}
	if c := d.cfg.load(); c != nil && strings.TrimSpace(c.Tenant) != "" && strings.TrimSpace(c.ClientID) != "" {
		m["state"], m["auth"] = "not-signed-in", authLabel(c.Auth)
		if upn, ok := d.signedInNow(); ok {
			m["state"], m["upn"] = "ready", upn
			if d.signInLapsed() != "" { // saved, but Microsoft 365 refused it: sign in again
				m["state"] = "needs-sign-in"
				m["note"] = fmt.Sprintf("Microsoft 365 refused your saved sign-in as %s (it expired or access was revoked). Click Sign in to sign in again.", upn)
			}
		}
	}
	s := d.cfg.settings()
	d.reply(w, wsSourceOut{OK: true, Epoch: d.boot, M365: m, Archive: d.archiveState(), Account: d.cfg.account(), Running: d.exportLive(),
		Settings: map[string]any{"dest": s.ExportDest, "preserve": s.Preserve(), "html": s.ReadableHTML(), "attachments": s.ExtractAttachments(), "attachmentsAs": s.AttachmentsAs()}})
}

type wsFolderOut struct {
	ID        string `json:"id"`
	Parent    string `json:"parent"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	Children  int    `json:"children"`
	WellKnown string `json:"wellKnown,omitempty"` // "deleted" or "junk": listed, labelled and unticked (ruling Q2)
	Label     string `json:"label,omitempty"`     // an archive store's name with the mailbox it holds (XC21)
}

// foldersAction is "Load folders": the whole tree with its counts, cached so the page's
// folder ids can be checked against it (XC17). For the archive source it reads the archive's
// manifest snapshot instead (ws_archive.go).
func (d *dashboard) foldersAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source wsSourceReq `json:"source"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	if in.Source.isArchive() {
		d.archiveFolders(w, r, in.Source)
		return
	}
	sess, ref := d.ws.session(r.Context())
	if ref != nil {
		d.refuse(w, *ref)
		return
	}
	d.ws.listMu.Lock()
	defer d.ws.listMu.Unlock()
	infos, err := sess.client.FolderTree(r.Context(), "", graph.MaxWorkspaceFolders)
	if err != nil {
		d.refuse(w, d.listRefusal(err, "your folders"))
		return
	}
	t := buildTree(infos)
	sess.setTree(t)
	sess.touch(d.now())
	out := make([]wsFolderOut, 0, len(t.order))
	for _, id := range t.order {
		f := t.byID[id]
		o := wsFolderOut{ID: f.ID, Parent: f.ParentID, Name: f.DisplayName, Count: f.TotalItemCount, Children: len(f.children)}
		switch f.WellKnown {
		case graph.WellKnownDeletedItems:
			o.WellKnown = "deleted"
		case graph.WellKnownJunkEmail:
			o.WellKnown = "junk"
		}
		if t.byID[f.ParentID] == nil {
			o.Parent = ""
		}
		out = append(out, o)
	}
	d.reply(w, map[string]any{"ok": true, "upn": sess.upn, "folders": out})
}

// listRefusal is graphRefusal for a listing, and drops the session when the sign-in is
// refused (the next request then reports it as the sign-in it is).
func (d *dashboard) listRefusal(err error, doing string) refusal {
	ref := graphRefusal(err, doing, d.cfgAuth())
	if ref.ID == "R-c" {
		d.noteRefused(ref)
		d.ws.dropSession()
	}
	return ref
}

// ---- the message list ---------------------------------------------------------

type wsMessagesIn struct {
	Folder string      `json:"folder"`
	Cursor string      `json:"cursor"`
	After  string      `json:"after"` // the last loaded row's handle, for a continuation after the cursor expired
	Source wsSourceReq `json:"source"`
}

type wsRowOut struct {
	H        string `json:"h"`
	Subject  string `json:"subject"`
	From     string `json:"from"`
	FromName string `json:"fromName"`
	Received string `json:"received"`
	Size     int64  `json:"size"`
}

// messagesAction is one page of a folder, newest first, at most 100 rows, with an opaque
// cursor and a short opaque handle on each row (XC17). An expired or evicted cursor
// continues with a keyset query after the last loaded row, or is R-x.
func (d *dashboard) messagesAction(w http.ResponseWriter, r *http.Request) {
	var in wsMessagesIn
	if !d.readBody(w, r, &in) {
		return
	}
	if in.Source.isArchive() {
		d.archiveMessages(w, r, in)
		return
	}
	sess, ref := d.ws.session(r.Context())
	if ref != nil {
		d.refuse(w, *ref)
		return
	}
	tree := sess.treeNow()
	var folder *wsFolder
	if tree != nil {
		folder = tree.byID[in.Folder]
	}
	if folder == nil { // a folder the session's tree never listed: refused with no Graph request (XC17)
		d.refuse(w, refusalJ2("folder"))
		return
	}
	d.ws.listMu.Lock()
	defer d.ws.listMu.Unlock()
	now := d.now()
	opts := graph.PageOpts{Top: listPageRows}
	next := ""
	var after *seenEntry
	switch {
	case in.Cursor != "":
		c, live := sess.takeCursor(in.Cursor, now)
		live = live && c.folder == in.Folder
		if live && c.next != "" {
			next, opts.Honored, opts.Guard = c.next, c.honored, c.guard
			break
		}
		afterH := in.After
		if live && c.afterH != "" { // a page cut at 100 rows continues by keyset from its last row
			afterH = c.afterH
		}
		e, ok := sess.seen.peek(afterH)
		if !ok || afterH == "" {
			d.refuse(w, refusalX(folder.DisplayName))
			return
		}
		after = &e
		opts.Before = e.received
	}
	if opts.Guard == nil {
		opts.Guard = sess.client.NewPageGuard()
	}
	refs, link, err := sess.client.MessagePage(r.Context(), "", in.Folder, next, opts)
	if err != nil {
		d.refuse(w, d.listRefusal(err, folder.DisplayName))
		return
	}
	rows := make([]wsRowOut, 0, len(refs))
	var lastReceived time.Time
	honored := opts.Honored
	truncated := false // the server answered more than 100 rows (it ignored $top): the page is cut here
	for _, m := range refs {
		if len(rows) >= listPageRows {
			truncated = true
			break
		}
		if after != nil && m.Received.Equal(after.received) && sess.seen.hasID(m.ID) {
			continue // a row the page already holds, at the keyset's own timestamp
		}
		f := m.ParentFolderID
		if f == "" {
			f = in.Folder
		}
		h := sess.seen.add(seenEntry{id: m.ID, folder: f, subject: m.Subject, from: m.From, fromName: m.FromName, imid: m.InternetMessageID,
			received: m.Received, size: m.SizeHint, state: app.StateOf(m)})
		rows = append(rows, wsRowOut{H: h, Subject: m.Subject, From: m.From, FromName: m.FromName, Received: m.Received.UTC().Format(time.RFC3339), Size: m.SizeHint})
		lastReceived = m.Received
		honored = honored || m.PhysID != ""
	}
	out := map[string]any{"ok": true, "rows": rows, "more": link != "" || truncated, "count": folder.TotalItemCount}
	switch {
	case truncated: // the rest is a fresh keyset query after the last row shown, never the server's own link
		out["cursor"] = sess.newCursor(&wsCursor{folder: in.Folder, afterH: rows[len(rows)-1].H, honored: honored, guard: opts.Guard, last: lastReceived}, now)
	case link != "":
		out["cursor"] = sess.newCursor(&wsCursor{folder: in.Folder, next: link, honored: honored, guard: opts.Guard, last: lastReceived}, now)
	}
	sess.touch(now)
	d.reply(w, out)
}

// ---- selection and counts ------------------------------------------------------

// wsSelection is a selection as the page describes it: folder entries and individual
// picks, never a message list (P3).
type wsSelection struct {
	Source  wsSourceReq `json:"source"`
	Folders []struct {
		ID      string   `json:"id"`
		Subtree bool     `json:"subtree"`
		Exclude []string `json:"exclude"`
	} `json:"folders"`
	Messages []struct {
		H string `json:"h"`
	} `json:"messages"`
	Dest    string `json:"dest"`
	Options struct {
		Preserve    *bool `json:"preserve"`
		HTML        bool  `json:"html"`
		Attachments bool  `json:"attachments"`
		// AttachmentsAs is "zip" (one zip per message, the default) or "loose" (files in a folder).
		AttachmentsAs string `json:"attachmentsAs"`
	} `json:"options"`
}

// checkBounds is R-j: more than 10,000 picks plus exclusions.
func (s *wsSelection) checkBounds() *refusal {
	n := len(s.Messages)
	for _, f := range s.Folders {
		n += len(f.Exclude)
	}
	if n > maxSelectionPicks {
		r := refusalJ(n)
		return &r
	}
	return nil
}

// folderSet is the entries' folders, each with its descendants where the subtree flag
// is on, de-duplicated across entries, in tree order. Every folder id must be in tree
// (XC17): a well-known name the page never saw, or "..", is refused. vanished lists the
// ids of entries that are in the cached tree but missing from fresh.
func folderSet(sel *wsSelection, cached, fresh *wsTree) (ids []string, vanished []string, bad bool) {
	set := map[string]bool{}
	for _, f := range sel.Folders {
		if cached == nil || cached.byID[f.ID] == nil {
			return nil, nil, true
		}
		if fresh.byID[f.ID] == nil {
			vanished = append(vanished, f.ID)
			continue
		}
		list := []string{f.ID}
		if f.Subtree {
			list = fresh.subtree(f.ID)
		}
		for _, id := range list {
			set[id] = true
		}
	}
	for _, id := range fresh.order {
		if set[id] {
			ids = append(ids, id)
		}
	}
	return ids, vanished, false
}

// resolveHandles resolves the selection's picks and exclusions through the seen-set, which
// resolution never adds to; a handle the session never listed is R-j2.
func (sess *wsSession) resolveHandles(sel *wsSelection) (picks []seenEntry, excl map[string]seenEntry, all []string, ref *refusal) {
	excl = map[string]seenEntry{}
	for _, f := range sel.Folders {
		for _, h := range f.Exclude {
			e, ok := sess.seen.peek(h)
			if !ok {
				r := refusalJ2("message")
				return nil, nil, nil, &r
			}
			excl[e.id] = e
			all = append(all, h)
		}
	}
	seen := map[string]bool{}
	for _, m := range sel.Messages {
		e, ok := sess.seen.peek(m.H)
		if !ok {
			r := refusalJ2("message")
			return nil, nil, nil, &r
		}
		all = append(all, m.H)
		if !seen[e.id] {
			seen[e.id] = true
			picks = append(picks, e)
		}
	}
	return picks, excl, all, nil
}

// countAction is the footer's count: the sum of the de-duplicated folder set's
// totalItemCount (what Microsoft 365 reports) less the exclusions inside it, plus the
// picks outside it. It reads the cached tree only and calls Graph never (XC16).
func (d *dashboard) countAction(w http.ResponseWriter, r *http.Request) {
	var sel wsSelection
	if !d.readBody(w, r, &sel) {
		return
	}
	if ref := sel.checkBounds(); ref != nil {
		d.refuse(w, *ref)
		return
	}
	var sess *wsSession
	if sel.Source.isArchive() {
		var ref *refusal
		if sess, ref = d.archiveSession(sel.Source); ref != nil {
			d.refuse(w, *ref)
			return
		}
	} else {
		if ref := d.signerRefusal(sel.Source.Mailbox); ref != nil {
			d.refuse(w, *ref)
			return
		}
		var ref *refusal
		if sess, ref = d.ws.session(r.Context()); ref != nil {
			d.refuse(w, *ref)
			return
		}
	}
	tree := sess.treeNow()
	if tree == nil { // released after 30 idle minutes, or never loaded: nothing here can be resolved
		d.refuse(w, refusalJ2("folder"))
		return
	}
	ids, _, bad := folderSet(&sel, tree, tree)
	if bad {
		d.refuse(w, refusalJ2("folder"))
		return
	}
	picks, excl, handles, ref := sess.resolveHandles(&sel)
	if ref != nil {
		d.refuse(w, *ref)
		return
	}
	sess.seen.pin(handles) // the live selection's handles survive eviction
	inSet := map[string]bool{}
	reported := 0
	for _, id := range ids {
		inSet[id] = true
		reported += tree.byID[id].TotalItemCount
	}
	for _, e := range excl {
		if inSet[e.folder] {
			reported--
		}
	}
	for _, e := range picks {
		if !inSet[e.folder] {
			reported++
		}
	}
	sess.touch(d.now())
	d.reply(w, map[string]any{"ok": true, "count": max(reported, 0), "folders": len(ids)})
}

// ---- the preview ----------------------------------------------------------------

// wsPreviewJob is the dry-run job: one at a time, a new preview cancels the old one.
type wsPreviewJob struct {
	id     string
	cancel context.CancelFunc
	arch   bool // the job reads an archive: a Microsoft 365 sign-in change does not concern it (E4-AI5)

	mu       sync.Mutex
	state    string // running | done | failed
	progress string
	result   map[string]any
	ref      *refusal
	retry    string // the run a retry preview is for, "" for a selection's
	which    string // "failed" or "rest" for a retry preview
}

func (j *wsPreviewJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state == "running"
}

func (j *wsPreviewJob) set(fn func()) {
	j.mu.Lock()
	fn()
	j.mu.Unlock()
}

// previewAction starts the dry-run job and returns its id, which is the preview id Start
// takes. It writes nothing to the destination, the archive or the config directory (P5).
func (d *dashboard) previewAction(w http.ResponseWriter, r *http.Request) {
	var sel wsSelection
	if !d.readBody(w, r, &sel) {
		return
	}
	if len(sel.Folders) == 0 && len(sel.Messages) == 0 {
		d.refuse(w, refusedWith("R-h", "Select at least one folder or message to preview."))
		return
	}
	if ref := sel.checkBounds(); ref != nil {
		d.refuse(w, *ref)
		return
	}
	archive := sel.Source.isArchive()
	if !archive {
		if ref := d.signerRefusal(sel.Source.Mailbox); ref != nil {
			d.refuse(w, *ref)
			return
		}
	}
	if st, live := d.exportState(); live {
		d.refuse(w, refusalM(st.started, st.done, st.selected))
		return
	}
	var sess *wsSession
	var ref *refusal
	if archive {
		sess, ref = d.archiveSession(sel.Source)
	} else {
		sess, ref = d.ws.session(r.Context())
	}
	if ref != nil {
		d.refuse(w, *ref)
		return
	}
	if sess.treeNow() == nil {
		d.refuse(w, refusalJ2("folder"))
		return
	}
	opts := app.CopyOptions{PreserveFolders: true}
	if sel.Options.Preserve != nil {
		opts.PreserveFolders = *sel.Options.Preserve
	}
	opts.ReadableHTML = sel.Options.HTML
	opts.Attachments = sel.Options.Attachments                                       // slice E6: OFF unless the page sends it
	opts.AttachmentsAsZip = opts.Attachments && sel.Options.AttachmentsAs != "loose" // one zip per message unless loose files were chosen
	if archive {
		job := d.ws.startJob(func(ctx context.Context, job *wsPreviewJob) (*app.CopyPlan, map[string]any, *refusal) {
			return d.resolveAndPlanArchive(ctx, job, sess, &sel, opts)
		}, wsBind{arch: true, path: sess.archPath, other: sess.archOther}, "")
		d.reply(w, map[string]any{"ok": true, "previewId": job.id})
		return
	}
	job := d.ws.startJob(func(ctx context.Context, job *wsPreviewJob) (*app.CopyPlan, map[string]any, *refusal) {
		return d.resolveAndPlan(ctx, job, sess, &sel, opts)
	}, wsBind{upn: sess.upn}, "")
	d.reply(w, map[string]any{"ok": true, "previewId": job.id})
}

// startJob runs fn as the preview job in a recovered goroutine on the process-lifetime
// context, cancelling the previous one, and keeps its plan as the one live preview.
func (ws *workspace) startJob(fn func(ctx context.Context, job *wsPreviewJob) (*app.CopyPlan, map[string]any, *refusal), bind wsBind, retry string) *wsPreviewJob {
	d := ws.d
	ctx, cancel := context.WithCancel(d.life.Context())
	job := &wsPreviewJob{id: randomID(16), cancel: cancel, arch: bind.arch, state: "running", retry: retry}
	ws.mu.Lock()
	old := ws.job
	ws.job, ws.prev = job, nil
	ws.mu.Unlock()
	if old != nil {
		old.cancel()
	}
	go func() {
		defer cancel()
		defer func() {
			if p := recover(); p != nil {
				job.set(func() {
					job.state = "failed"
					job.ref = &refusal{ID: "internal", Text: "MailArchive Desktop stopped this preview unexpectedly. Nothing was written; try again.", Status: statusRefused}
				})
			}
		}()
		plan, res, ref := fn(ctx, job)
		ws.mu.Lock()
		current := ws.job == job
		if current && ref == nil && plan != nil {
			ws.prev = &wsPreview{id: job.id, plan: plan, upn: bind.upn, bind: bind, created: d.now(), retry: retry}
		}
		ws.mu.Unlock()
		if !current {
			return // superseded by a newer preview or a dropped session
		}
		job.set(func() {
			if ref != nil {
				job.state, job.ref = "failed", ref
				return
			}
			job.state, job.result = "done", res
		})
	}()
	return job
}

// previewStatusAction polls the job: its progress, then its result or its refusal.
func (d *dashboard) previewStatusAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PreviewID string `json:"previewId"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	d.ws.mu.Lock()
	job := d.ws.job
	d.ws.mu.Unlock()
	if job == nil || job.id != in.PreviewID {
		d.refuse(w, refusalK(d.ws.inRetryMode()))
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	var out map[string]any
	switch job.state {
	case "done":
		out = map[string]any{"ok": true, "state": "done", "result": job.result}
	case "failed":
		out = job.ref.body()
		out["state"] = "failed"
	default:
		out = map[string]any{"ok": true, "state": "running", "progress": job.progress}
	}
	if job.retry != "" { // a retry's page keeps the destination controls, even when its preview is refused (F5)
		out["retryOf"], out["retryWhich"] = job.retry, job.which
	}
	d.reply(w, out)
}

// resolveAndPlan turns the selection into the plan (design §4.4, §4.7): the folder set
// from a freshly fetched tree, each folder's messages read with FolderCount before and
// after (the union of two listings when they differ), the exclusions removed and the
// picks added, then the engine sizes and names it. It issues only GETs and writes nothing.
func (d *dashboard) resolveAndPlan(ctx context.Context, job *wsPreviewJob, sess *wsSession, sel *wsSelection, opts app.CopyOptions) (*app.CopyPlan, map[string]any, *refusal) {
	cached := sess.treeNow()
	picks, excl, handles, ref := sess.resolveHandles(sel)
	if ref != nil {
		return nil, nil, ref
	}
	sess.seen.pin(handles)
	infos, err := sess.client.FolderTree(ctx, "", graph.MaxWorkspaceFolders)
	if err != nil {
		r := d.listRefusal(err, "your folders")
		return nil, nil, &r
	}
	fresh := buildTree(infos)
	ids, vanished, bad := folderSet(sel, cached, fresh)
	if bad {
		r := refusalJ2("folder")
		return nil, nil, &r
	}
	sess.setTree(fresh)

	var warns, infosOut []string
	for _, id := range vanished {
		warns = append(warns, fmt.Sprintf("The folder %s is no longer in the mailbox, so it was left out.", cached.byID[id].DisplayName))
	}
	signedIn := func() (string, bool) { return d.signedInNow() }
	src := app.NewM365Source(sess.client, sess.upn, sess.tenant, signedIn)

	type found struct {
		folder   string
		subs     int
		reported int
		msgs     int
		excluded int
	}
	var perFolder []found
	var msgs []app.CopyMessage
	listed := map[string]bool{}
	for i, id := range ids {
		f := fresh.byID[id]
		job.set(func() { job.progress = fmt.Sprintf("Reading %s (%d of %d folders)", f.DisplayName, i+1, len(ids)) })
		path := fresh.path(id)
		// The listing stops as soon as this folder cannot fit what is already kept: a
		// folder of millions is refused after the page that shows it, not after it has
		// been read whole (EW-AC11). Exclusions only remove messages from what is listed.
		src.MaxListed = max(d.planLimit()-len(msgs)+len(excl), 1)
		ms, reported, w, info, lerr := d.listFolder(ctx, sess, src, f, path)
		if lerr != nil {
			if errors.Is(lerr, app.ErrListTooLarge) {
				ref := refusalI(len(ids) == 1 && len(picks) == 0)
				return nil, nil, &ref
			}
			r := d.listRefusal(lerr, f.DisplayName)
			return nil, nil, &r
		}
		warns, infosOut = append(warns, w...), append(infosOut, info...)
		fc := found{folder: strings.Join(folderNames(path), "/"), reported: reported, subs: len(f.children)}
		for _, m := range ms {
			if listed[m.SourceID] {
				continue
			}
			listed[m.SourceID] = true
			if _, drop := excl[m.SourceID]; drop {
				fc.excluded++
				continue
			}
			msgs = append(msgs, m)
			fc.msgs++
		}
		perFolder = append(perFolder, fc)
		if len(msgs) > d.planLimit() {
			ref := refusalI(len(ids) == 1 && len(picks) == 0)
			return nil, nil, &ref
		}
	}
	// the individual picks outside the folder set
	picked := 0
	for _, e := range picks {
		if listed[e.id] {
			continue
		}
		listed[e.id] = true
		path := fresh.path(e.folder)
		if len(path) == 0 {
			path = []app.CopyFolderRef{{ID: e.folder, Name: "Unknown folder"}}
			warns = append(warns, "A message you picked is in a folder that is no longer in the mailbox; it is exported under \"Unknown folder\".")
		}
		msgs = append(msgs, app.CopyMessage{SourceID: e.id, InternetMessageID: e.imid, Folder: path, Subject: e.subject, From: e.from, FromName: e.fromName,
			Received: e.received, SizeHint: e.size, Mailbox: sess.upn, State: e.state})
		picked++
	}
	if picked > 0 {
		perFolder = append(perFolder, found{folder: "Individually picked messages", msgs: picked, reported: picked})
	}
	order := map[string]int{}
	for i, id := range fresh.order {
		order[id] = i
	}
	sort.SliceStable(msgs, func(i, j int) bool { // tree order, then newest first
		a, b := msgs[i], msgs[j]
		ai, bi := folderIndex(order, a), folderIndex(order, b)
		if ai != bi {
			return ai < bi
		}
		return a.Received.After(b.Received)
	})

	var planFolders []app.CopyPlanFolder
	var folderOut []foundOut
	reportedTotal := 0
	for _, f := range perFolder {
		planFolders = append(planFolders, app.CopyPlanFolder{Folder: f.folder, Subfolders: f.subs, Excluded: f.excluded, Messages: f.msgs, Reported: f.reported})
		folderOut = append(folderOut, foundOut{Folder: f.folder, Reported: f.reported, Found: f.msgs, Subs: f.subs, Excluded: f.excluded})
		reportedTotal += f.reported
	}
	dest := strings.TrimSpace(sel.Dest)
	in := app.CopyPlanInput{Source: src.Describe(), Folders: planFolders, Messages: msgs, Options: opts, Dest: dest, Archives: d.archives()}
	plan, perr := d.copyEnv().PlanCopyExport(in)
	if perr != nil {
		return nil, nil, copyRefusal(perr)
	}
	return plan, planResult(plan, job.id, folderOut, reportedTotal, len(msgs), warns, infosOut), nil
}

type foundOut struct {
	Folder   string `json:"folder"`
	Reported int    `json:"reported"`
	Found    int    `json:"found"`
	Subs     int    `json:"subfolders"`
	Excluded int    `json:"excluded"`
}

func folderIndex(order map[string]int, m app.CopyMessage) int {
	if len(m.Folder) == 0 {
		return 1 << 30
	}
	if i, ok := order[m.Folder[len(m.Folder)-1].ID]; ok {
		return i
	}
	return 1 << 29
}

func folderNames(path []app.CopyFolderRef) []string {
	out := make([]string, len(path))
	for i, p := range path {
		out[i] = util.StripControl(p.Name)
	}
	return out
}

// listFolder lists one folder's messages with the reconciliation of design §4.7 (XC16):
// FolderCount before and after the listing; before == after is the only stability test.
// A folder that changed is listed once more and the two listings are united (an arrival
// during a newest-first listing is a duplicate, de-duplicated; a removal ahead of the
// cursor is a skip, which the union restores), with a WARN only if the second listing's own
// reads differ too. A stable folder whose reported count exceeds what it lists gets an
// info line, never a re-list or a WARN.
func (d *dashboard) listFolder(ctx context.Context, sess *wsSession, src *app.M365Source, f *wsFolder, path []app.CopyFolderRef) (msgs []app.CopyMessage, reported int, warns, infos []string, err error) {
	before, err := sess.client.FolderCount(ctx, "", f.ID)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	msgs, err = src.ListFolder(ctx, path)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	after, err := sess.client.FolderCount(ctx, "", f.ID)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	reported = after
	if before != after {
		again, lerr := src.ListFolder(ctx, path)
		if lerr != nil {
			return nil, 0, nil, nil, lerr
		}
		after2, cerr := sess.client.FolderCount(ctx, "", f.ID)
		if cerr != nil {
			return nil, 0, nil, nil, cerr
		}
		msgs = unionMessages(msgs, again)
		reported = after2
		if after != after2 {
			warns = append(warns, fmt.Sprintf("%s changed while it was being read twice in a row (Microsoft 365 reported %s, then %s, then %s items). Preview again for an exact copy.",
				f.DisplayName, commaInt(before), commaInt(after), commaInt(after2)))
		}
	} else if after != len(msgs) {
		infos = append(infos, fmt.Sprintf("Microsoft 365 reports %s items in %s; %s are messages and will be copied.", commaInt(after), f.DisplayName, commaInt(len(msgs))))
	}
	return msgs, reported, warns, infos, nil
}

// unionMessages is the union of two listings of one folder by id, newest first.
func unionMessages(a, b []app.CopyMessage) []app.CopyMessage {
	seen := make(map[string]bool, len(a)+len(b))
	var out []app.CopyMessage
	for _, list := range [][]app.CopyMessage{b, a} {
		for _, m := range list {
			if !seen[m.SourceID] {
				seen[m.SourceID] = true
				out = append(out, m)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Received.After(out[j].Received) })
	return out
}

// planLimit is previewMessageCap (R-i) unless a test lowers it for this dashboard.
func (d *dashboard) planLimit() int {
	if d.planMax > 0 {
		return d.planMax
	}
	return previewMessageCap
}

// archives are the archive folders an export must stay outside of: the configured one.
func (d *dashboard) archives() []string {
	if out := d.cfg.effectiveOut(); out != "" {
		return []string{out}
	}
	return nil
}

// archivesWith is archives and, for an export from another archive folder, that folder too
// [ruling W1]: an export never lands in the archive it reads, whichever one it is.
func (d *dashboard) archivesWith(other string) []string {
	out := d.archives()
	if other = strings.TrimSpace(other); other != "" {
		out = append(out, other)
	}
	return out
}

// copyRefusal turns an engine refusal into the page's refusal.
func copyRefusal(err error) *refusal {
	var ref *app.CopyRefusal
	if errors.As(err, &ref) {
		r := refusal{ID: ref.ID, Text: ref.Message, Status: statusRefused}
		if ref.Root != "" {
			r.Extra = map[string]any{"emptyRoot": filepath.Base(ref.Root)}
		}
		return &r
	}
	return &refusal{ID: "internal", Text: "MailArchive Desktop couldn't prepare this export. Nothing was written; try again.", Status: statusRefused}
}

// planResult is the preview's answer (design §4.7 step 4).
func planResult(p *app.CopyPlan, id string, folders any, reported, found int, warns, infos []string) map[string]any {
	warnings := append(append([]string(nil), p.Warnings...), warns...)
	if warnings == nil {
		warnings = []string{}
	}
	if infos == nil {
		infos = []string{}
	}
	posture := map[string]any{"kind": p.Posture.Kind, "line": p.Posture.Line, "readers": p.Posture.Readers, "predicted": p.Posture.Predicted}
	out := map[string]any{
		"previewId": id, "count": found, "reported": reported, "folders": folders,
		"estimateBytes": p.EstimateBytes, "estimate": humanSize(p.EstimateBytes), "unknownSize": p.UnknownSize,
		"attachBytes": p.AttachBytes, "attachEstimate": humanSize(p.AttachBytes),
		"freeBytes": p.FreeBytes, "free": humanSize(int64(p.FreeBytes)), "reserve": p.Reserve, "reserveText": humanSize(p.Reserve), "recordBound": p.RecordBound,
		"exportName": p.ExportName, "exportDir": filepath.Join(p.Dest, p.ExportName), "dest": p.Dest,
		"samplePaths": p.SamplePaths, "longestPath": p.LongestPath, "over259": p.Over259,
		"posture": posture, "fsType": p.FSType, "warnings": warnings, "info": infos,
		"preserve": p.Options.PreserveFolders, "html": p.Options.ReadableHTML, "attachments": p.Options.Attachments, "attachmentsZip": p.Options.AttachmentsAsZip, "retryOf": p.Chain.RetryOf, "part": len(p.Chain.Parts) + 1,
	}
	if c := p.Archive; c != nil { // the archive source's own counts (design §4.7 step 4)
		out["archive"] = map[string]any{"withOriginal": c.WithOriginal, "withoutOriginal": c.WithoutOriginal, "gone": c.Gone,
			"quarantined": c.Quarantined, "unattested": c.Unattested, "skipped": c.Skipped, "blocked": c.Blocked, "other": p.Source.Other, "path": p.Source.Archive}
	}
	return out
}

// humanSize is a size as the preview shows it: "1.4 GB".
func humanSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
