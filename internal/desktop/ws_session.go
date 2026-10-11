package desktop

import (
	"container/list"
	"crypto/rand"
	"sync"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graph"
)

// The workspace's Graph session and its caches (extraction design §4.3, §4.4): the folder
// tree, the seen-set that gates the row handles the page sends back (XC17), and the list
// cursors. Everything here is memory only, bounded, and released after 30 idle minutes
// (XC18).

const (
	seenMaxEntries = 250000   // the seen-set's entry cap
	seenMaxBytes   = 96 << 20 // and its byte cap (an estimate of its strings)
	cursorMax      = 64       // list cursors
	cursorTTL      = 10 * time.Minute
	sessionIdle    = 30 * time.Minute
	listPageRows   = 100 // list pages never hold more than this
)

// seenEntry is one listed message's metadata, resolved from its row handle.
type seenEntry struct {
	handle   string
	id       string // Graph's id (the immutable id when honoured)
	folder   string // the message's folder, from Graph's parentFolderId (interned)
	subject  string
	from     string
	fromName string
	imid     string
	received time.Time
	size     int64 // -1 = unknown
	el       *list.Element
}

func (e *seenEntry) bytes() int64 {
	return int64(len(e.handle)+len(e.id)+len(e.subject)+len(e.from)+len(e.fromName)+len(e.imid)) + 160
}

// seenSet is the gate for the row handles the page sends back: a listed message gets a
// short opaque handle, the handle resolves to the message's metadata, and nothing else
// does (R-j2). It is an LRU with an entry cap and a byte cap; the handles of the live
// selection are pinned against eviction, and resolving a selection never adds to it.
type seenSet struct {
	mu       sync.Mutex
	byHandle map[string]*seenEntry
	byID     map[string]string
	lru      *list.List // front = most recently used
	pinned   map[string]bool
	intern   map[string]string
	maxN     int
	maxBytes int64
	bytes    int64
}

func newSeenSet() *seenSet {
	return &seenSet{byHandle: map[string]*seenEntry{}, byID: map[string]string{}, lru: list.New(), pinned: map[string]bool{},
		intern: map[string]string{}, maxN: seenMaxEntries, maxBytes: seenMaxBytes}
}

const handleAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// newHandle is a random 8-character handle no entry holds.
func (s *seenSet) newHandleLocked() string {
	for {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			panic("row handle: no randomness: " + err.Error())
		}
		for i := range b {
			b[i] = handleAlphabet[int(b[i])%len(handleAlphabet)]
		}
		if h := string(b); s.byHandle[h] == nil {
			return h
		}
	}
}

// add records a listed message and returns its handle: the handle it already has, when
// the same message is listed again, so a selection survives a reload of the list.
func (s *seenSet) add(e seenEntry) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.byID[e.id]; ok {
		if old := s.byHandle[h]; old != nil {
			s.lru.MoveToFront(old.el)
			return h
		}
	}
	e.handle = s.newHandleLocked()
	if v, ok := s.intern[e.folder]; ok {
		e.folder = v
	} else {
		s.intern[e.folder] = e.folder
	}
	ne := e
	ne.el = s.lru.PushFront(&ne)
	s.byHandle[ne.handle] = &ne
	s.byID[ne.id] = ne.handle
	s.bytes += ne.bytes()
	s.evictLocked()
	return ne.handle
}

// get resolves a handle, refreshing its place in the LRU.
func (s *seenSet) get(h string) (seenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byHandle[h]
	if e == nil {
		return seenEntry{}, false
	}
	s.lru.MoveToFront(e.el)
	return *e, true
}

// peek resolves a handle without touching the LRU: resolving a selection never feeds it.
func (s *seenSet) peek(h string) (seenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.byHandle[h]; e != nil {
		return *e, true
	}
	return seenEntry{}, false
}

// hasID reports whether a listed message with this Graph id is in the set.
func (s *seenSet) hasID(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.byID[id]
	return ok
}

// pin replaces the pinned handles: the live selection's, which eviction skips.
func (s *seenSet) pin(handles []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinned = make(map[string]bool, len(handles))
	for _, h := range handles {
		s.pinned[h] = true
	}
}

func (s *seenSet) len() (n int, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byHandle), s.bytes
}

// evictLocked drops the least recently used unpinned entries until both caps hold.
func (s *seenSet) evictLocked() {
	for len(s.byHandle) > s.maxN || s.bytes > s.maxBytes {
		var victim *list.Element
		for el := s.lru.Back(); el != nil; el = el.Prev() {
			if !s.pinned[el.Value.(*seenEntry).handle] {
				victim = el
				break
			}
		}
		if victim == nil {
			return // everything left is pinned
		}
		e := victim.Value.(*seenEntry)
		s.lru.Remove(victim)
		delete(s.byHandle, e.handle)
		if s.byID[e.id] == e.handle {
			delete(s.byID, e.id)
		}
		s.bytes -= e.bytes()
	}
}

// wsFolder is one folder of the cached tree.
type wsFolder struct {
	graph.FolderInfo
	children []string
}

// wsTree is the folder tree a "Load folders" cached: Microsoft 365 folder ids from the
// page are accepted only if they are in it (XC17).
type wsTree struct {
	order []string // tree order: a folder, then its children, depth first
	byID  map[string]*wsFolder
	roots []string
}

func buildTree(infos []graph.FolderInfo) *wsTree {
	t := &wsTree{byID: make(map[string]*wsFolder, len(infos))}
	for _, f := range infos {
		t.byID[f.ID] = &wsFolder{FolderInfo: f}
	}
	for _, f := range infos {
		if p := t.byID[f.ParentID]; p != nil && f.ParentID != f.ID {
			p.children = append(p.children, f.ID)
		} else {
			t.roots = append(t.roots, f.ID)
		}
	}
	var walk func(id string)
	seen := map[string]bool{}
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		t.order = append(t.order, id)
		for _, c := range t.byID[id].children {
			walk(c)
		}
	}
	for _, r := range t.roots {
		walk(r)
	}
	return t
}

// subtree is id and every descendant, in tree order.
func (t *wsTree) subtree(id string) []string {
	if t.byID[id] == nil {
		return nil
	}
	// Children are built from the parentFolderId Graph reports, so two folders may name
	// each other as parent: each folder is listed once (a visited set), and the walk is
	// an explicit stack, so neither a cycle nor a very deep tree can overflow the stack,
	// which recover cannot catch (EW-AC6).
	var out []string
	seen := map[string]bool{}
	stack := []string{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		f := t.byID[cur]
		if f == nil || seen[cur] {
			continue
		}
		seen[cur] = true
		out = append(out, cur)
		for i := len(f.children) - 1; i >= 0; i-- { // pushed in reverse: popped in tree order
			stack = append(stack, f.children[i])
		}
	}
	return out
}

// path is the folder's display path root to leaf, as the export names it.
func (t *wsTree) path(id string) []app.CopyFolderRef {
	var rev []app.CopyFolderRef
	for cur, n := t.byID[id], 0; cur != nil && n < 400; n++ {
		rev = append(rev, app.CopyFolderRef{ID: cur.ID, Name: cur.DisplayName})
		cur = t.byID[cur.ParentID]
		if cur != nil && cur.ID == id {
			break
		}
	}
	out := make([]app.CopyFolderRef, len(rev))
	for i := range rev {
		out[len(rev)-1-i] = rev[i]
	}
	return out
}

// wsCursor continues a folder's listing: the Graph nextLink, recorded server-side and
// never handed to the page, with the guard of the loop it belongs to.
type wsCursor struct {
	folder  string
	next    string
	afterH  string // set instead of next when the page was cut at 100 rows: the handle of its last row
	honored bool
	guard   *graph.PageGuard
	expires time.Time
	last    time.Time // the last loaded row's received time, for a keyset continuation
}

// wsSession is the Graph session of one dashboard process: the client, the signer, and
// the caches a "Load folders" and the list pages fill.
type wsSession struct {
	client *graph.Client
	upn    string
	tenant string

	mu       sync.Mutex
	tree     *wsTree
	seen     *seenSet
	cursors  map[string]*wsCursor
	lastUsed time.Time
}

func newWsSession(client *graph.Client, upn, tenant string, now time.Time) *wsSession {
	return &wsSession{client: client, upn: upn, tenant: tenant, seen: newSeenSet(), cursors: map[string]*wsCursor{}, lastUsed: now}
}

func (s *wsSession) treeNow() *wsTree {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tree
}

func (s *wsSession) setTree(t *wsTree) {
	s.mu.Lock()
	s.tree = t
	s.mu.Unlock()
}

func (s *wsSession) touch(now time.Time) {
	s.mu.Lock()
	s.lastUsed = now
	s.mu.Unlock()
}

// release drops the tree, the seen-set and the cursors (XC18); the client stays, so a
// later "Load folders" needs no new sign-in.
func (s *wsSession) release() {
	s.mu.Lock()
	s.tree = nil
	s.seen = newSeenSet()
	s.cursors = map[string]*wsCursor{}
	s.mu.Unlock()
}

func (s *wsSession) idleSince() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastUsed
}

// newCursor records a continuation and returns its opaque id; at the cap the least
// recently used cursor goes.
func (s *wsSession) newCursor(c *wsCursor, now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, old := range s.cursors { // expired ones first
		if now.After(old.expires) {
			delete(s.cursors, id)
		}
	}
	for len(s.cursors) >= cursorMax {
		var oldest string
		var at time.Time
		for id, old := range s.cursors {
			if oldest == "" || old.expires.Before(at) {
				oldest, at = id, old.expires
			}
		}
		delete(s.cursors, oldest)
	}
	b := make([]byte, 9)
	rand.Read(b)
	for i := range b {
		b[i] = handleAlphabet[int(b[i])%len(handleAlphabet)]
	}
	id := string(b)
	c.expires = now.Add(cursorTTL)
	s.cursors[id] = c
	return id
}

// takeCursor resolves a cursor id and refreshes its TTL; an expired or evicted one is gone.
func (s *wsSession) takeCursor(id string, now time.Time) (*wsCursor, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cursors[id]
	if c == nil || now.After(c.expires) {
		delete(s.cursors, id)
		return nil, false
	}
	c.expires = now.Add(cursorTTL)
	return c, true
}
